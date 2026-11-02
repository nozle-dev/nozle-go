package nozle

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// TaxIDType is a customer tax ID type: Stripe's list. Core validates it, so a value a newer Core adds still
// decodes and can be sent; compare with the constants below.
type TaxIDType string

const (
	TaxIDTypeAdNrt    TaxIDType = "ad_nrt"
	TaxIDTypeAeTrn    TaxIDType = "ae_trn"
	TaxIDTypeAlTin    TaxIDType = "al_tin"
	TaxIDTypeAmTin    TaxIDType = "am_tin"
	TaxIDTypeAoTin    TaxIDType = "ao_tin"
	TaxIDTypeArCuit   TaxIDType = "ar_cuit"
	TaxIDTypeAuAbn    TaxIDType = "au_abn"
	TaxIDTypeAuArn    TaxIDType = "au_arn"
	TaxIDTypeBaTin    TaxIDType = "ba_tin"
	TaxIDTypeBbTin    TaxIDType = "bb_tin"
	TaxIDTypeBgUic    TaxIDType = "bg_uic"
	TaxIDTypeBhVat    TaxIDType = "bh_vat"
	TaxIDTypeBoTin    TaxIDType = "bo_tin"
	TaxIDTypeBrCnpj   TaxIDType = "br_cnpj"
	TaxIDTypeBrCpf    TaxIDType = "br_cpf"
	TaxIDTypeBsTin    TaxIDType = "bs_tin"
	TaxIDTypeByTin    TaxIDType = "by_tin"
	TaxIDTypeCaBn     TaxIDType = "ca_bn"
	TaxIDTypeCaGstHst TaxIDType = "ca_gst_hst"
	TaxIDTypeCaPstBc  TaxIDType = "ca_pst_bc"
	TaxIDTypeCaPstMb  TaxIDType = "ca_pst_mb"
	TaxIDTypeCaPstSk  TaxIDType = "ca_pst_sk"
	TaxIDTypeCaQst    TaxIDType = "ca_qst"
	TaxIDTypeCdNif    TaxIDType = "cd_nif"
	TaxIDTypeChUid    TaxIDType = "ch_uid"
	TaxIDTypeChVat    TaxIDType = "ch_vat"
	TaxIDTypeClTin    TaxIDType = "cl_tin"
	TaxIDTypeCnTin    TaxIDType = "cn_tin"
	TaxIDTypeCoNit    TaxIDType = "co_nit"
	TaxIDTypeCrTin    TaxIDType = "cr_tin"
	TaxIDTypeDeStn    TaxIDType = "de_stn"
	TaxIDTypeDoRcn    TaxIDType = "do_rcn"
	TaxIDTypeEcRuc    TaxIDType = "ec_ruc"
	TaxIDTypeEgTin    TaxIDType = "eg_tin"
	TaxIDTypeEsCif    TaxIDType = "es_cif"
	TaxIDTypeEuOssVat TaxIDType = "eu_oss_vat"
	TaxIDTypeEuVat    TaxIDType = "eu_vat"
	TaxIDTypeGbVat    TaxIDType = "gb_vat"
	TaxIDTypeGeVat    TaxIDType = "ge_vat"
	TaxIDTypeGnNif    TaxIDType = "gn_nif"
	TaxIDTypeHkBr     TaxIDType = "hk_br"
	TaxIDTypeHrOib    TaxIDType = "hr_oib"
	TaxIDTypeHuTin    TaxIDType = "hu_tin"
	TaxIDTypeIdNpwp   TaxIDType = "id_npwp"
	TaxIDTypeIlVat    TaxIDType = "il_vat"
	TaxIDTypeInGst    TaxIDType = "in_gst"
	TaxIDTypeIsVat    TaxIDType = "is_vat"
	TaxIDTypeJpCn     TaxIDType = "jp_cn"
	TaxIDTypeJpRn     TaxIDType = "jp_rn"
	TaxIDTypeJpTrn    TaxIDType = "jp_trn"
	TaxIDTypeKePin    TaxIDType = "ke_pin"
	TaxIDTypeKgTin    TaxIDType = "kg_tin"
	TaxIDTypeKhTin    TaxIDType = "kh_tin"
	TaxIDTypeKrBrn    TaxIDType = "kr_brn"
	TaxIDTypeKzBin    TaxIDType = "kz_bin"
	TaxIDTypeLiUid    TaxIDType = "li_uid"
	TaxIDTypeLiVat    TaxIDType = "li_vat"
	TaxIDTypeMaVat    TaxIDType = "ma_vat"
	TaxIDTypeMdVat    TaxIDType = "md_vat"
	TaxIDTypeMePib    TaxIDType = "me_pib"
	TaxIDTypeMkVat    TaxIDType = "mk_vat"
	TaxIDTypeMrNif    TaxIDType = "mr_nif"
	TaxIDTypeMxRfc    TaxIDType = "mx_rfc"
	TaxIDTypeMyFrp    TaxIDType = "my_frp"
	TaxIDTypeMyItn    TaxIDType = "my_itn"
	TaxIDTypeMySst    TaxIDType = "my_sst"
	TaxIDTypeNgTin    TaxIDType = "ng_tin"
	TaxIDTypeNoVat    TaxIDType = "no_vat"
	TaxIDTypeNoVoec   TaxIDType = "no_voec"
	TaxIDTypeNpPan    TaxIDType = "np_pan"
	TaxIDTypeNzGst    TaxIDType = "nz_gst"
	TaxIDTypeOmVat    TaxIDType = "om_vat"
	TaxIDTypePeRuc    TaxIDType = "pe_ruc"
	TaxIDTypePhTin    TaxIDType = "ph_tin"
	TaxIDTypeRoTin    TaxIDType = "ro_tin"
	TaxIDTypeRsPib    TaxIDType = "rs_pib"
	TaxIDTypeRuInn    TaxIDType = "ru_inn"
	TaxIDTypeRuKpp    TaxIDType = "ru_kpp"
	TaxIDTypeSaVat    TaxIDType = "sa_vat"
	TaxIDTypeSgGst    TaxIDType = "sg_gst"
	TaxIDTypeSgUen    TaxIDType = "sg_uen"
	TaxIDTypeSiTin    TaxIDType = "si_tin"
	TaxIDTypeSnNinea  TaxIDType = "sn_ninea"
	TaxIDTypeSrFin    TaxIDType = "sr_fin"
	TaxIDTypeSvNit    TaxIDType = "sv_nit"
	TaxIDTypeThVat    TaxIDType = "th_vat"
	TaxIDTypeTjTin    TaxIDType = "tj_tin"
	TaxIDTypeTrTin    TaxIDType = "tr_tin"
	TaxIDTypeTwVat    TaxIDType = "tw_vat"
	TaxIDTypeTzVat    TaxIDType = "tz_vat"
	TaxIDTypeUaVat    TaxIDType = "ua_vat"
	TaxIDTypeUgTin    TaxIDType = "ug_tin"
	TaxIDTypeUsEin    TaxIDType = "us_ein"
	TaxIDTypeUyRuc    TaxIDType = "uy_ruc"
	TaxIDTypeUzTin    TaxIDType = "uz_tin"
	TaxIDTypeUzVat    TaxIDType = "uz_vat"
	TaxIDTypeVeRif    TaxIDType = "ve_rif"
	TaxIDTypeVnTin    TaxIDType = "vn_tin"
	TaxIDTypeZaVat    TaxIDType = "za_vat"
	TaxIDTypeZmTin    TaxIDType = "zm_tin"
	TaxIDTypeZwTin    TaxIDType = "zw_tin"
)

// TaxIDStatus is the result of checking a customer's tax ID.
type TaxIDStatus string

const (
	TaxIDStatusUnverified TaxIDStatus = "unverified" // stored, never checked or not checkable
	TaxIDStatusPending    TaxIDStatus = "pending"    // a VIES check is running or retrying
	TaxIDStatusValid      TaxIDStatus = "valid"
	TaxIDStatusInvalid    TaxIDStatus = "invalid"
)

// TaxTreatment says how tax applies to a customer or an invoice.
type TaxTreatment string

const (
	TaxTreatmentStandard      TaxTreatment = "standard"
	TaxTreatmentReverseCharge TaxTreatment = "reverse_charge"
	TaxTreatmentExempt        TaxTreatment = "exempt"
	TaxTreatmentProvider      TaxTreatment = "provider" // an external tax provider decides
	TaxTreatmentPending       TaxTreatment = "pending"  // held for a VIES check
	TaxTreatmentNone          TaxTreatment = "none"     // no EU tax management
)

// Codes for customer tax data. The first three arrive in the error_details of a 422 UpsertCustomer
// (tax_identification_type or tax_identification_number); CodeCustomerTaxLocationRequired (422, error_details
// "customer": country_missing or address_missing) and CodeCustomerTaxCheckPending (409, see
// APIError.RetryAfterSeconds) refuse a checkout call. Test them with APIError.HasCode.
const (
	CodeValueIsInvalid              = "value_is_invalid"
	CodeValueIsMandatory            = "value_is_mandatory"
	CodeInvalidFormat               = "invalid_format"
	CodeCustomerTaxLocationRequired = "customer_tax_location_required"
	CodeCustomerTaxCheckPending     = "customer_tax_check_pending"

	// Invoice tax failures (TaxStatus "failed").
	CodeCustomerTaxLocationInvalid = "customer_tax_location_invalid"
	CodeTaxIDInvalid               = "tax_id_invalid"
	CodeStripeTaxNotAuthorized     = "stripe_tax_not_authorized"
	CodeStripeAccountRequired      = "stripe_account_required"
	CodeTaxProviderUnavailable     = "tax_provider_unavailable"
)

// ErrTaxCheckPending is returned by WaitForTaxCheck when the timeout passes with the check still pending. The check
// keeps running in Core.
var ErrTaxCheckPending = errors.New("nozle: customers.wait_for_tax_check: tax check still pending")

// CustomerAddress is a postal address. On the wire Core uses address_line1, address_line2, city, state, zipcode and
// country.
type CustomerAddress struct {
	Line1, Line2, City, State, PostalCode string
	Country                               string // ISO 3166-1 alpha-2, for example "DE"
}

type addressWire struct {
	Line1      string `json:"address_line1,omitempty"`
	Line2      string `json:"address_line2,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"zipcode,omitempty"`
	Country    string `json:"country,omitempty"`
}

func (a addressWire) empty() bool { return a == addressWire{} }

func (a CustomerAddress) MarshalJSON() ([]byte, error) { return json.Marshal(addressWire(a)) }

func (a *CustomerAddress) UnmarshalJSON(data []byte) error {
	var w addressWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*a = CustomerAddress(w)
	return nil
}

// CustomerTaxID is a customer's tax ID. Core validates the format.
type CustomerTaxID struct {
	Type  TaxIDType
	Value string
}

// customerWire is Customer's JSON form: Core's flat keys. TaxNumber is a RawMessage so an explicit null (clearing
// the tax ID) is sent while an unset one is omitted.
type customerWire struct {
	ExternalID   string `json:"external_id"`
	Name         string `json:"name,omitempty"`
	Email        string `json:"email,omitempty"`
	CustomerType string `json:"customer_type,omitempty"`
	LegalName    string `json:"legal_name,omitempty"`
	TaxExempt    *bool  `json:"tax_exempt,omitempty"`
	addressWire
	ShippingAddress      *addressWire                  `json:"shipping_address,omitempty"`
	TaxNumber            json.RawMessage               `json:"tax_identification_number,omitempty"`
	TaxType              TaxIDType                     `json:"tax_identification_type,omitempty"`
	BillingConfiguration *CustomerBillingConfiguration `json:"billing_configuration,omitempty"`
}

// customerReadWire holds what only Core's answer carries.
type customerReadWire struct {
	customerWire
	Status   TaxIDStatus  `json:"tax_identification_status,omitempty"`
	Treat    TaxTreatment `json:"tax_treatment,omitempty"`
	Warnings []string     `json:"tax_identification_warnings,omitempty"`
}

// MarshalJSON writes Core's flat request keys. The read-only fields (TaxIdentificationStatus, TaxTreatment,
// TaxIdentificationWarnings) are never sent, so a decoded Customer can be sent back unchanged.
func (c Customer) MarshalJSON() ([]byte, error) {
	w := customerWire{ExternalID: c.ExternalID, Name: c.Name, Email: c.Email, CustomerType: c.CustomerType,
		LegalName: c.LegalName, TaxExempt: c.TaxExempt, BillingConfiguration: c.BillingConfiguration}
	if c.Address != nil {
		w.addressWire = addressWire(*c.Address)
	}
	if c.ShippingAddress != nil {
		s := addressWire(*c.ShippingAddress)
		w.ShippingAddress = &s
	}
	switch {
	case c.TaxID != nil:
		number, err := json.Marshal(c.TaxID.Value)
		if err != nil {
			return nil, err
		}
		w.TaxNumber, w.TaxType = number, c.TaxID.Type
	case c.ClearTaxID:
		w.TaxNumber = json.RawMessage("null")
	}
	return json.Marshal(w)
}

// UnmarshalJSON drops a billing_configuration without a payment provider, which
// Core returns for customers that have none, so a decoded Customer can be sent
// back to UpsertCustomer unchanged.
func (c *Customer) UnmarshalJSON(data []byte) error {
	var w customerReadWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	out := Customer{ExternalID: w.ExternalID, Name: w.Name, Email: w.Email, CustomerType: w.CustomerType,
		LegalName: w.LegalName, TaxExempt: w.TaxExempt, BillingConfiguration: w.BillingConfiguration,
		TaxIdentificationStatus: w.Status, TaxTreatment: w.Treat, TaxIdentificationWarnings: w.Warnings}
	if out.BillingConfiguration != nil && out.BillingConfiguration.PaymentProvider == "" {
		out.BillingConfiguration = nil
	}
	if !w.addressWire.empty() {
		a := CustomerAddress(w.addressWire)
		out.Address = &a
	}
	if w.ShippingAddress != nil && !w.ShippingAddress.empty() {
		a := CustomerAddress(*w.ShippingAddress)
		out.ShippingAddress = &a
	}
	var number string
	if len(w.TaxNumber) > 0 && string(w.TaxNumber) != "null" {
		if err := json.Unmarshal(w.TaxNumber, &number); err != nil {
			return err
		}
	}
	if number != "" || w.TaxType != "" {
		out.TaxID = &CustomerTaxID{Type: w.TaxType, Value: number}
	}
	*c = out
	return nil
}

// GetCustomer returns one customer with its tax fields (GET /customers/:external_id, Core). Secret key.
func (c *Client) GetCustomer(ctx context.Context, externalID string) (Customer, error) {
	var out struct {
		Customer Customer `json:"customer"`
	}
	if err := validateSecret(c, "customers.get"); err != nil {
		return out.Customer, err
	}
	if err := required("customers.get", "external_id", externalID); err != nil {
		return out.Customer, err
	}
	err := c.request(ctx, "customers.get", "GET", true, customerPath(externalID), nil, &out, "")
	return out.Customer, err
}

// WaitForTaxCheckOptions tunes WaitForTaxCheck. Zero values mean a 10 s timeout and a 500 ms poll interval.
type WaitForTaxCheckOptions struct{ Timeout, Interval time.Duration }

// WaitForTaxCheck polls GetCustomer until TaxIdentificationStatus is not "pending" (a VIES check is running) and
// returns the customer. Call it before a checkout so the price already reflects reverse charge. It returns
// ErrTaxCheckPending after the timeout, and ctx's error if ctx ends first.
func (c *Client) WaitForTaxCheck(ctx context.Context, externalID string, opts WaitForTaxCheckOptions) (Customer, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.Interval <= 0 {
		opts.Interval = 500 * time.Millisecond
	}
	deadline := time.Now().Add(opts.Timeout)
	for {
		customer, err := c.GetCustomer(ctx, externalID)
		if err != nil || customer.TaxIdentificationStatus != TaxIDStatusPending {
			return customer, err
		}
		if time.Now().Add(opts.Interval).After(deadline) {
			return customer, ErrTaxCheckPending
		}
		select {
		case <-ctx.Done():
			return customer, ctx.Err()
		case <-time.After(opts.Interval):
		}
	}
}

// InvoiceTaxDetails is the tax record Core writes at finalization; Invoice.TaxDetails is nil on older invoices.
type InvoiceTaxDetails struct {
	Treatment                           TaxTreatment `json:"treatment"`
	TaxIdentificationNumber             *string      `json:"tax_identification_number"`
	TaxIdentificationType               *TaxIDType   `json:"tax_identification_type"`
	TaxIdentificationStatus             TaxIDStatus  `json:"tax_identification_status"`
	TaxIdentificationConsultationNumber *string      `json:"tax_identification_consultation_number"`
	CustomerCountry                     *string      `json:"customer_country"`
	SellerTaxIdentificationNumber       *string      `json:"seller_tax_identification_number"`
	// Set when Stripe Tax computed the tax.
	Provider          *string  `json:"provider"`
	CalculationID     *string  `json:"calculation_id"`
	TransactionID     *string  `json:"transaction_id"`
	TaxabilityReasons []string `json:"taxability_reasons"`
}
