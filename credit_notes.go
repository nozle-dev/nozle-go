package nozle

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Codes Core puts in APIError.Code or error_details; test them with APIError.HasCode.
// CodeRefundExceedsPaidAmount is returned by RefundCreditNote itself, the rest by Core.
const (
	CodeInvalidTypeOrStatus              = "invalid_type_or_status"
	CodeCannotRefundUnpaidInvoice        = "cannot_refund_unpaid_invoice"
	CodeHigherThanRemainingInvoiceAmount = "higher_than_remaining_invoice_amount"
	CodeHigherThanRemainingFeeAmount     = "higher_than_remaining_fee_amount"
	CodeDoesNotMatchItemAmounts          = "does_not_match_item_amounts"
	CodeTotalAmountMustBePositive        = "total_amount_must_be_positive"
	CodeNoVoidableAmount                 = "no_voidable_amount"
	CodeRefundExceedsPaidAmount          = "refund_exceeds_paid_amount"
)

// CreditNoteReason says why a credit note is issued. Core defaults to CreditNoteReasonOther.
type CreditNoteReason string

const (
	CreditNoteReasonDuplicatedCharge      CreditNoteReason = "duplicated_charge"
	CreditNoteReasonProductUnsatisfactory CreditNoteReason = "product_unsatisfactory"
	CreditNoteReasonOrderChange           CreditNoteReason = "order_change"
	CreditNoteReasonOrderCancellation     CreditNoteReason = "order_cancellation"
	CreditNoteReasonFraudulentCharge      CreditNoteReason = "fraudulent_charge"
	CreditNoteReasonOther                 CreditNoteReason = "other"
)

var creditNoteReasons = map[CreditNoteReason]bool{
	CreditNoteReasonDuplicatedCharge: true, CreditNoteReasonProductUnsatisfactory: true, CreditNoteReasonOrderChange: true,
	CreditNoteReasonOrderCancellation: true, CreditNoteReasonFraudulentCharge: true, CreditNoteReasonOther: true,
}

// CreditNoteItem is one invoice fee (fees[].nozle_id) and how much of it, in cents before tax, to credit.
type CreditNoteItem struct {
	FeeID       string `json:"fee_id"`
	AmountCents int64  `json:"amount_cents"`
}

// CreditNote is a credit note. RefundStatus is pending, succeeded or failed (nil without a refund);
// CreditStatus is available, consumed or voided. The API reference lists every field.
type CreditNote struct {
	NozleID            string  `json:"nozle_id"`
	Number             string  `json:"number"`
	InvoiceID          string  `json:"invoice_id"`
	InvoiceNumber      string  `json:"invoice_number"`
	IssuingDate        string  `json:"issuing_date"`
	Reason             string  `json:"reason"`
	Description        *string `json:"description"`
	Currency           string  `json:"currency"`
	CreditStatus       *string `json:"credit_status"`
	RefundStatus       *string `json:"refund_status"`
	TotalAmountCents   int64   `json:"total_amount_cents"`
	TaxesAmountCents   int64   `json:"taxes_amount_cents"`
	BalanceAmountCents int64   `json:"balance_amount_cents"`
	CreditAmountCents  int64   `json:"credit_amount_cents"`
	RefundAmountCents  int64   `json:"refund_amount_cents"`
	OffsetAmountCents  int64   `json:"offset_amount_cents"`
	FileURL            *string `json:"file_url"`
}

// CreditNotePreview is what a credit note would be worth. Nothing is created.
// MaxRefundableAmountCents is what was paid and not refunded yet, capped at the total.
type CreditNotePreview struct {
	InvoiceID                         string           `json:"invoice_id"`
	InvoiceNumber                     string           `json:"invoice_number"`
	Currency                          string           `json:"currency"`
	TaxesAmountCents                  int64            `json:"taxes_amount_cents"`
	TaxesRate                         float64          `json:"taxes_rate"`
	SubTotalExcludingTaxesAmountCents int64            `json:"sub_total_excluding_taxes_amount_cents"`
	MaxCreditableAmountCents          int64            `json:"max_creditable_amount_cents"`
	MaxRefundableAmountCents          int64            `json:"max_refundable_amount_cents"`
	CouponsAdjustmentAmountCents      int64            `json:"coupons_adjustment_amount_cents"`
	Items                             []CreditNoteItem `json:"items"`
}

// PreviewCreditNoteParams names the invoice and the items a credit note would cover.
type PreviewCreditNoteParams struct {
	InvoiceID string
	Items     []CreditNoteItem
}

// CreateCreditNoteParams issues a credit note. The three amounts must add up to the total of Items with
// taxes (take it from PreviewCreditNote) and at least one must be above zero.
//   - CreditAmountCents goes to the customer's balance, applied to their next invoice.
//   - RefundAmountCents goes back to the original payment method, only up to what was paid and not
//     refunded yet; follow CreditNote.RefundStatus with GetCreditNote.
//   - OffsetAmountCents comes off what the customer still owes on the invoice.
type CreateCreditNoteParams struct {
	InvoiceID                                               string
	Items                                                   []CreditNoteItem
	CreditAmountCents, RefundAmountCents, OffsetAmountCents int64
	Reason                                                  CreditNoteReason
	// Description is printed on the credit note PDF.
	Description string
	// Metadata is your own key-value pairs, for example a ticket number.
	Metadata map[string]string
}

// RefundCreditNoteParams refunds Items to the original payment method.
type RefundCreditNoteParams struct {
	InvoiceID   string
	Items       []CreditNoteItem
	Reason      CreditNoteReason
	Description string
	Metadata    map[string]string
	// CreditRemainder refunds what was paid and credits the rest to the customer's balance when less was
	// paid than Items are worth. Without it RefundCreditNote returns CodeRefundExceedsPaidAmount.
	CreditRemainder bool
}

// ListCreditNotesParams filters ListCreditNotes. Empty fields are not sent; dates are inclusive YYYY-MM-DD.
// CreditStatus is available, consumed or voided; RefundStatus is pending, succeeded or failed.
type ListCreditNotesParams struct {
	CustomerID, InvoiceNumber, Reason, CreditStatus, RefundStatus, Currency, IssuingDateFrom, IssuingDateTo string
	AmountFrom, AmountTo                                                                                    int64
	Page, PerPage                                                                                           int
}

// CreditNotePage is one page of credit notes.
type CreditNotePage struct {
	CreditNotes []CreditNote `json:"credit_notes"`
	Meta        struct {
		CurrentPage int  `json:"current_page"`
		NextPage    *int `json:"next_page"`
		PrevPage    *int `json:"prev_page"`
		TotalPages  int  `json:"total_pages"`
		TotalCount  int  `json:"total_count"`
	} `json:"meta"`
}

type creditNoteBody struct {
	InvoiceID         string            `json:"invoice_id"`
	Items             []CreditNoteItem  `json:"items"`
	CreditAmountCents int64             `json:"credit_amount_cents,omitempty"`
	RefundAmountCents int64             `json:"refund_amount_cents,omitempty"`
	OffsetAmountCents int64             `json:"offset_amount_cents,omitempty"`
	Reason            CreditNoteReason  `json:"reason,omitempty"`
	Description       string            `json:"description,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// PreviewCreditNote returns what a credit note for these items would be worth, with taxes and coupons
// applied. It creates nothing, so it is retried like a read.
func (c *Client) PreviewCreditNote(ctx context.Context, p PreviewCreditNoteParams) (CreditNotePreview, error) {
	const op = "preview_credit_note"
	var out struct {
		Estimated CreditNotePreview `json:"estimated_credit_note"`
	}
	if err := c.creditNoteCheck(op, "invoice_id", p.InvoiceID); err != nil {
		return out.Estimated, err
	}
	items, err := creditNoteItems(op, p.Items)
	if err != nil {
		return out.Estimated, err
	}
	body := map[string]any{"credit_note": map[string]any{"invoice_id": p.InvoiceID, "items": items}}
	err = c.request(ctx, op, http.MethodPost, true, "/api/v1/credit_notes/estimate", body, &out, "")
	return out.Estimated, err
}

// CreateCreditNote issues a credit note. It sends no Idempotency-Key yet, so it is never retried: after a transport error or a 5xx, call
// ListCreditNotes with the CustomerID and InvoiceNumber before trying again.
func (c *Client) CreateCreditNote(ctx context.Context, p CreateCreditNoteParams) (CreditNote, error) {
	const op = "create_credit_note"
	var out struct {
		CreditNote CreditNote `json:"credit_note"`
	}
	if err := c.creditNoteCheck(op, "invoice_id", p.InvoiceID); err != nil {
		return out.CreditNote, err
	}
	items, err := creditNoteItems(op, p.Items)
	if err != nil {
		return out.CreditNote, err
	}
	for name, v := range map[string]int64{"credit_amount_cents": p.CreditAmountCents, "refund_amount_cents": p.RefundAmountCents, "offset_amount_cents": p.OffsetAmountCents} {
		if v < 0 {
			return out.CreditNote, &ValidationError{op, name + " must be 0 or more"}
		}
	}
	if p.CreditAmountCents+p.RefundAmountCents+p.OffsetAmountCents <= 0 {
		return out.CreditNote, &ValidationError{op, "one of credit_amount_cents, refund_amount_cents or offset_amount_cents must be above zero"}
	}
	if p.Reason != "" && !creditNoteReasons[p.Reason] {
		return out.CreditNote, &ValidationError{op, "reason " + strconv.Quote(string(p.Reason)) + " is not one of the CreditNoteReason values"}
	}
	body := map[string]any{"credit_note": creditNoteBody{
		InvoiceID: p.InvoiceID, Items: items, CreditAmountCents: p.CreditAmountCents, RefundAmountCents: p.RefundAmountCents,
		OffsetAmountCents: p.OffsetAmountCents, Reason: p.Reason, Description: p.Description, Metadata: p.Metadata,
	}}
	err = c.request(ctx, op, http.MethodPost, true, "/api/v1/credit_notes", body, &out, "")
	return out.CreditNote, err
}

// RefundCreditNote refunds the items to the original payment method: PreviewCreditNote for the exact
// total, then CreateCreditNote with that total as the refund. Two calls; the second is not retried.
// When less was paid than the items are worth it returns an *APIError with CodeRefundExceedsPaidAmount
// and creates nothing, unless CreditRemainder is set.
func (c *Client) RefundCreditNote(ctx context.Context, p RefundCreditNoteParams) (CreditNote, error) {
	const op = "refund_credit_note"
	preview, err := c.PreviewCreditNote(ctx, PreviewCreditNoteParams{InvoiceID: p.InvoiceID, Items: p.Items})
	if err != nil {
		return CreditNote{}, err
	}
	total := preview.MaxCreditableAmountCents
	refundable := min(preview.MaxRefundableAmountCents, total)
	remainder := total - refundable
	if refundable <= 0 || (remainder > 0 && !p.CreditRemainder) {
		hint := ""
		if refundable > 0 {
			hint = "; set CreditRemainder to credit the rest"
		}
		message := strconv.FormatInt(refundable, 10) + " of " + strconv.FormatInt(total, 10) + " cents can be refunded" + hint
		details := map[string]any{"code": CodeRefundExceedsPaidAmount, "error": message}
		return CreditNote{}, &APIError{
			StatusCode: http.StatusUnprocessableEntity, Method: http.MethodPost, Path: "/api/v1/credit_notes", Operation: op,
			Body: `{"code":"` + CodeRefundExceedsPaidAmount + `","error":` + strconv.Quote(message) + `}`, Details: details,
		}
	}
	return c.CreateCreditNote(ctx, CreateCreditNoteParams{
		InvoiceID: p.InvoiceID, Items: p.Items, RefundAmountCents: refundable, CreditAmountCents: remainder,
		Reason: p.Reason, Description: p.Description, Metadata: p.Metadata,
	})
}

// ListCreditNotes returns credit notes, newest first: all of them, or one customer's.
func (c *Client) ListCreditNotes(ctx context.Context, p ListCreditNotesParams) (CreditNotePage, error) {
	var out CreditNotePage
	if err := validateKey(c.apiKey, "list_credit_notes"); err != nil {
		return out, err
	}
	q := url.Values{}
	for name, value := range map[string]string{
		"external_customer_id": p.CustomerID, "invoice_number": p.InvoiceNumber, "reason": p.Reason, "credit_status": p.CreditStatus,
		"refund_status": p.RefundStatus, "currency": p.Currency, "issuing_date_from": p.IssuingDateFrom, "issuing_date_to": p.IssuingDateTo,
	} {
		if value != "" {
			q.Set(name, value)
		}
	}
	for name, value := range map[string]int64{"amount_from": p.AmountFrom, "amount_to": p.AmountTo, "page": int64(p.Page), "per_page": int64(p.PerPage)} {
		if value > 0 {
			q.Set(name, strconv.FormatInt(value, 10))
		}
	}
	err := c.request(ctx, "list_credit_notes", http.MethodGet, true, query("/api/v1/credit_notes", q), nil, &out, "")
	return out, err
}

// GetCreditNote returns one credit note.
func (c *Client) GetCreditNote(ctx context.Context, creditNoteID string) (CreditNote, error) {
	var out struct {
		CreditNote CreditNote `json:"credit_note"`
	}
	if err := c.creditNoteCheck("get_credit_note", "credit_note_id", creditNoteID); err != nil {
		return out.CreditNote, err
	}
	err := c.request(ctx, "get_credit_note", http.MethodGet, true, "/api/v1/credit_notes/"+segment(creditNoteID), nil, &out, "")
	return out.CreditNote, err
}

// VoidCreditNote voids what is left of the credit note's credit balance. It does not undo a refund that
// was paid out or an offset already applied. Core answers 422 CodeNoVoidableAmount when nothing is left.
// It is never retried.
func (c *Client) VoidCreditNote(ctx context.Context, creditNoteID string) (CreditNote, error) {
	var out struct {
		CreditNote CreditNote `json:"credit_note"`
	}
	if err := c.creditNoteCheck("void_credit_note", "credit_note_id", creditNoteID); err != nil {
		return out.CreditNote, err
	}
	err := c.request(ctx, "void_credit_note", http.MethodPut, true, "/api/v1/credit_notes/"+segment(creditNoteID)+"/void", nil, &out, "")
	return out.CreditNote, err
}

func (c *Client) creditNoteCheck(op, field, value string) error {
	if err := validateKey(c.apiKey, op); err != nil {
		return err
	}
	return required(op, field, value)
}

func creditNoteItems(op string, items []CreditNoteItem) ([]CreditNoteItem, error) {
	if len(items) == 0 {
		return nil, &ValidationError{op, "at least one item is required"}
	}
	for _, item := range items {
		if strings.TrimSpace(item.FeeID) == "" {
			return nil, &ValidationError{op, "items[].fee_id is required"}
		}
		if item.AmountCents < 0 {
			return nil, &ValidationError{op, "items[].amount_cents must be 0 or more"}
		}
	}
	return items, nil
}
