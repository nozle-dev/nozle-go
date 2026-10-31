package nozle

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const cnBase = "/core/api/v1/credit_notes"

var cnItems = []CreditNoteItem{{FeeID: "fee_1", AmountCents: 500}}

func estimateJSON(refundable, creditable int) string {
	return `{"estimated_credit_note":{"invoice_id":"inv_1","currency":"USD","max_creditable_amount_cents":` + itoa(creditable) +
		`,"max_refundable_amount_cents":` + itoa(refundable) + `}}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// creditNoteServer answers by "METHOD path", records bodies and counts calls per route.
func creditNoteServer(t *testing.T, routes map[string]func(w http.ResponseWriter, body map[string]any)) (*Client, map[string]*int32, *[]map[string]any) {
	t.Helper()
	calls := map[string]*int32{}
	for k := range routes {
		calls[k] = new(int32)
	}
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		route, ok := routes[key]
		if !ok {
			t.Errorf("unexpected %s", key)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		atomic.AddInt32(calls[key], 1)
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test" {
			t.Errorf("authorization %q", got)
		}
		if r.Header.Get("Idempotency-Key") != "" {
			t.Errorf("credit notes must not send Idempotency-Key")
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		route(w, body)
	}))
	t.Cleanup(server.Close)
	return New("sk_test", WithHost(server.URL), WithMaxRetries(2), WithRetryDelay(1)), calls, &bodies
}

func reply(status int, body string) func(http.ResponseWriter, map[string]any) {
	return func(w http.ResponseWriter, _ map[string]any) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

const noteJSON = `{"credit_note":{"nozle_id":"cn_1","number":"INV-1-CN1","refund_amount_cents":600,"refund_status":"pending"}}`

func TestPreviewCreditNoteSendsTheEstimateBodyAndRetries(t *testing.T) {
	var n int32
	c, calls, bodies := creditNoteServer(t, map[string]func(http.ResponseWriter, map[string]any){
		"POST " + cnBase + "/estimate": func(w http.ResponseWriter, _ map[string]any) {
			if atomic.AddInt32(&n, 1) < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(estimateJSON(500, 600)))
		},
	})
	p, err := c.PreviewCreditNote(context.Background(), PreviewCreditNoteParams{InvoiceID: "inv_1", Items: cnItems})
	if err != nil || p.MaxRefundableAmountCents != 500 || p.MaxCreditableAmountCents != 600 {
		t.Fatalf("%+v %v", p, err)
	}
	if *calls["POST "+cnBase+"/estimate"] != 3 {
		t.Fatalf("a preview creates nothing and is retried: %d calls", *calls["POST "+cnBase+"/estimate"])
	}
	want := `{"credit_note":{"invoice_id":"inv_1","items":[{"amount_cents":500,"fee_id":"fee_1"}]}}`
	got, _ := json.Marshal((*bodies)[0])
	if string(got) != want {
		t.Fatalf("body %s", got)
	}
}

func TestCreateCreditNoteSendsAmountsReasonDescriptionAndMetadata(t *testing.T) {
	c, _, bodies := creditNoteServer(t, map[string]func(http.ResponseWriter, map[string]any){"POST " + cnBase: reply(200, noteJSON)})
	note, err := c.CreateCreditNote(context.Background(), CreateCreditNoteParams{
		InvoiceID: "inv_1", Items: cnItems, RefundAmountCents: 400, CreditAmountCents: 100, OffsetAmountCents: 100,
		Reason: CreditNoteReasonOrderCancellation, Description: "Cancelled", Metadata: map[string]string{"ticket": "T-1"},
	})
	if err != nil || note.NozleID != "cn_1" || note.RefundStatus == nil || *note.RefundStatus != "pending" {
		t.Fatalf("%+v %v", note, err)
	}
	got, _ := json.Marshal((*bodies)[0])
	want := `{"credit_note":{"credit_amount_cents":100,"description":"Cancelled","invoice_id":"inv_1","items":[{"amount_cents":500,"fee_id":"fee_1"}],"metadata":{"ticket":"T-1"},"offset_amount_cents":100,"reason":"order_cancellation","refund_amount_cents":400}}`
	if string(got) != want {
		t.Fatalf("body %s", got)
	}
}

func TestCreateAndVoidAreNeverRetried(t *testing.T) {
	c, calls, _ := creditNoteServer(t, map[string]func(http.ResponseWriter, map[string]any){
		"POST " + cnBase:               reply(503, `{}`),
		"PUT " + cnBase + "/cn_1/void": reply(502, `{}`),
	})
	var api *APIError
	if _, err := c.CreateCreditNote(context.Background(), CreateCreditNoteParams{InvoiceID: "inv_1", Items: cnItems, RefundAmountCents: 500}); !errors.As(err, &api) || api.StatusCode != 503 {
		t.Fatalf("%v", err)
	}
	if _, err := c.VoidCreditNote(context.Background(), "cn_1"); !errors.As(err, &api) || api.StatusCode != 502 {
		t.Fatalf("%v", err)
	}
	if *calls["POST "+cnBase] != 1 || *calls["PUT "+cnBase+"/cn_1/void"] != 1 {
		t.Fatalf("retried: %d %d", *calls["POST "+cnBase], *calls["PUT "+cnBase+"/cn_1/void"])
	}
}

func TestRefundCreditNotePreviewsThenRefundsThePreviewedTotal(t *testing.T) {
	c, _, bodies := creditNoteServer(t, map[string]func(http.ResponseWriter, map[string]any){
		"POST " + cnBase + "/estimate": reply(200, estimateJSON(600, 600)),
		"POST " + cnBase:               reply(200, noteJSON),
	})
	if _, err := c.RefundCreditNote(context.Background(), RefundCreditNoteParams{InvoiceID: "inv_1", Items: cnItems, Reason: CreditNoteReasonDuplicatedCharge}); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal((*bodies)[1])
	want := `{"credit_note":{"invoice_id":"inv_1","items":[{"amount_cents":500,"fee_id":"fee_1"}],"reason":"duplicated_charge","refund_amount_cents":600}}`
	if string(got) != want {
		t.Fatalf("body %s", got)
	}
}

func TestRefundCreditNoteCreatesNothingWhenLessWasPaidUnlessCreditRemainder(t *testing.T) {
	c, calls, bodies := creditNoteServer(t, map[string]func(http.ResponseWriter, map[string]any){
		"POST " + cnBase + "/estimate": reply(200, estimateJSON(250, 600)),
		"POST " + cnBase:               reply(200, noteJSON),
	})
	_, err := c.RefundCreditNote(context.Background(), RefundCreditNoteParams{InvoiceID: "inv_1", Items: cnItems})
	var api *APIError
	if !errors.As(err, &api) || !api.HasCode(CodeRefundExceedsPaidAmount) || api.StatusCode != 422 {
		t.Fatalf("%v", err)
	}
	if *calls["POST "+cnBase] != 0 {
		t.Fatal("created a credit note")
	}
	if _, err := c.RefundCreditNote(context.Background(), RefundCreditNoteParams{InvoiceID: "inv_1", Items: cnItems, CreditRemainder: true}); err != nil {
		t.Fatal(err)
	}
	last := (*bodies)[len(*bodies)-1]["credit_note"].(map[string]any)
	if last["refund_amount_cents"] != float64(250) || last["credit_amount_cents"] != float64(350) {
		t.Fatalf("%v", last)
	}
}

func TestRefundCreditNoteOfAnUnpaidInvoiceFailsEvenWithCreditRemainder(t *testing.T) {
	c, _, _ := creditNoteServer(t, map[string]func(http.ResponseWriter, map[string]any){"POST " + cnBase + "/estimate": reply(200, estimateJSON(0, 600))})
	_, err := c.RefundCreditNote(context.Background(), RefundCreditNoteParams{InvoiceID: "inv_1", Items: cnItems, CreditRemainder: true})
	var api *APIError
	if !errors.As(err, &api) || api.Code() != CodeRefundExceedsPaidAmount {
		t.Fatalf("%v", err)
	}
}

func TestListGetAndVoidCreditNotes(t *testing.T) {
	c, _, _ := creditNoteServer(t, map[string]func(http.ResponseWriter, map[string]any){
		"GET " + cnBase:                reply(200, `{"credit_notes":[{"nozle_id":"cn_1"}],"meta":{"current_page":2,"total_pages":3}}`),
		"GET " + cnBase + "/cn 1":      reply(200, noteJSON),
		"PUT " + cnBase + "/cn_1/void": reply(200, `{"credit_note":{"nozle_id":"cn_1","credit_status":"voided"}}`),
	})
	page, err := c.ListCreditNotes(context.Background(), ListCreditNotesParams{CustomerID: "cus 1", RefundStatus: "failed", AmountFrom: 1, Page: 2, PerPage: 5})
	if err != nil || page.CreditNotes[0].NozleID != "cn_1" || page.Meta.TotalPages != 3 {
		t.Fatalf("%+v %v", page, err)
	}
	if note, err := c.GetCreditNote(context.Background(), "cn 1"); err != nil || note.Number != "INV-1-CN1" {
		t.Fatalf("%+v %v", note, err)
	}
	if note, err := c.VoidCreditNote(context.Background(), "cn_1"); err != nil || note.CreditStatus == nil || *note.CreditStatus != "voided" {
		t.Fatalf("%+v %v", note, err)
	}
}

func TestListCreditNotesQueryAndNoFilters(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Encode())
		_, _ = w.Write([]byte(`{"credit_notes":[],"meta":{}}`))
	}))
	defer server.Close()
	c := New("sk_test", WithHost(server.URL))
	_, _ = c.ListCreditNotes(context.Background(), ListCreditNotesParams{
		CustomerID: "cus 1", InvoiceNumber: "INV-1", Reason: "other", CreditStatus: "available", RefundStatus: "failed", Currency: "USD",
		IssuingDateFrom: "2026-01-01", IssuingDateTo: "2026-02-01", AmountFrom: 1, AmountTo: 9, Page: 2, PerPage: 5,
	})
	_, _ = c.ListCreditNotes(context.Background(), ListCreditNotesParams{})
	want := "amount_from=1&amount_to=9&credit_status=available&currency=USD&external_customer_id=cus+1&invoice_number=INV-1&issuing_date_from=2026-01-01&issuing_date_to=2026-02-01&page=2&per_page=5&reason=other&refund_status=failed"
	if queries[0] != want || queries[1] != "" {
		t.Fatalf("%q", queries)
	}
}

func TestCreditNoteErrorsCarryStatusAndCodes(t *testing.T) {
	c, _, _ := creditNoteServer(t, map[string]func(http.ResponseWriter, map[string]any){
		"POST " + cnBase:               reply(422, `{"status":422,"error":"Unprocessable Entity","code":"validation_errors","error_details":{"refund_amount_cents":["higher_than_remaining_invoice_amount"]}}`),
		"POST " + cnBase + "/estimate": reply(422, `{"status":422,"error":"Unprocessable Entity","code":"invalid_type_or_status"}`),
		"PUT " + cnBase + "/cn_1/void": reply(422, `{"status":422,"code":"no_voidable_amount"}`),
		"GET " + cnBase + "/cn_x":      reply(404, `{"status":404,"error":"Not Found","code":"credit_note_not_found"}`),
		"GET " + cnBase:                reply(401, `{"status":401,"error":"Unauthorized"}`),
	})
	ctx := context.Background()
	check := func(err error, status int, code string) {
		t.Helper()
		var api *APIError
		if !errors.As(err, &api) || api.StatusCode != status || (code != "" && !api.HasCode(code)) {
			t.Fatalf("want %d %s, got %v", status, code, err)
		}
	}
	_, err := c.CreateCreditNote(ctx, CreateCreditNoteParams{InvoiceID: "inv_1", Items: cnItems, RefundAmountCents: 9})
	check(err, 422, CodeHigherThanRemainingInvoiceAmount)
	_, err = c.PreviewCreditNote(ctx, PreviewCreditNoteParams{InvoiceID: "inv_1", Items: cnItems})
	check(err, 422, CodeInvalidTypeOrStatus)
	_, err = c.VoidCreditNote(ctx, "cn_1")
	check(err, 422, CodeNoVoidableAmount)
	_, err = c.GetCreditNote(ctx, "cn_x")
	check(err, 404, "credit_note_not_found")
	_, err = c.ListCreditNotes(ctx, ListCreditNotesParams{})
	check(err, 401, "")
}

func TestCreditNoteInputIsCheckedBeforeAnyRequest(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { atomic.AddInt32(&hits, 1) }))
	defer server.Close()
	c := New("sk_test", WithHost(server.URL))
	ctx := context.Background()
	cases := map[string]func() error{
		"invoice id": func() error {
			_, err := c.PreviewCreditNote(ctx, PreviewCreditNoteParams{InvoiceID: " ", Items: cnItems})
			return err
		},
		"items": func() error {
			_, err := c.PreviewCreditNote(ctx, PreviewCreditNoteParams{InvoiceID: "inv_1"})
			return err
		},
		"fee id": func() error {
			_, err := c.PreviewCreditNote(ctx, PreviewCreditNoteParams{InvoiceID: "inv_1", Items: []CreditNoteItem{{AmountCents: 1}}})
			return err
		},
		"negative item": func() error {
			_, err := c.PreviewCreditNote(ctx, PreviewCreditNoteParams{InvoiceID: "inv_1", Items: []CreditNoteItem{{FeeID: "f", AmountCents: -1}}})
			return err
		},
		"no amount": func() error {
			_, err := c.CreateCreditNote(ctx, CreateCreditNoteParams{InvoiceID: "inv_1", Items: cnItems})
			return err
		},
		"negative refund": func() error {
			_, err := c.CreateCreditNote(ctx, CreateCreditNoteParams{InvoiceID: "inv_1", Items: cnItems, RefundAmountCents: -1, CreditAmountCents: 9})
			return err
		},
		"reason": func() error {
			_, err := c.CreateCreditNote(ctx, CreateCreditNoteParams{InvoiceID: "inv_1", Items: cnItems, CreditAmountCents: 1, Reason: "bored"})
			return err
		},
		"get id":  func() error { _, err := c.GetCreditNote(ctx, ""); return err },
		"void id": func() error { _, err := c.VoidCreditNote(ctx, ""); return err },
	}
	for name, call := range cases {
		var v *ValidationError
		if err := call(); !errors.As(err, &v) {
			t.Errorf("%s: %v", name, err)
		}
	}
	pk := New("pk_test", WithHost(server.URL))
	var auth *AuthenticationError
	if _, err := pk.GetCreditNote(ctx, "cn_1"); !errors.As(err, &auth) {
		t.Errorf("publishable key: %v", err)
	}
	if _, err := pk.RefundCreditNote(ctx, RefundCreditNoteParams{InvoiceID: "inv_1", Items: cnItems}); !errors.As(err, &auth) {
		t.Errorf("publishable key refund: %v", err)
	}
	if hits != 0 {
		t.Errorf("%d requests were sent", hits)
	}
}
