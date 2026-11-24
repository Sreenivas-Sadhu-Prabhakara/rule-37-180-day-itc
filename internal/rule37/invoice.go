package rule37

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Exclusion records why the 180-day condition does not bite on an invoice. The
// four reasons are the ones the law gives; anything else is in scope.
type Exclusion string

const (
	// ExclusionNone means the invoice is in scope.
	ExclusionNone Exclusion = ""

	// ExclusionReverseCharge is a supply on which the recipient itself pays the
	// tax. The second proviso to section 16(2) applies to supplies "other than
	// the supplies on which tax is payable on reverse charge basis", so the
	// 180-day condition never applies to them.
	ExclusionReverseCharge Exclusion = "reverse-charge"

	// ExclusionScheduleI is a supply made without consideration that is still
	// treated as a supply under Schedule I of the Act. The first proviso to
	// rule 37(1) deems its value to have been paid, so there is nothing to
	// reverse — there was never anything to pay.
	ExclusionScheduleI Exclusion = "schedule-i"

	// ExclusionSection15 is an amount added to the value of a supply under
	// clause (b) of section 15(2) — something the supplier was liable to pay
	// which the recipient incurred instead. The second proviso to rule 37(1)
	// deems it paid.
	ExclusionSection15 Exclusion = "section-15-2-b"

	// ExclusionNoCredit is a purchase on which no input tax credit was taken.
	// Rule 37 reverses credit availed; where none was availed there is nothing
	// for it to reach.
	ExclusionNoCredit Exclusion = "no-credit"
)

// Valid reports whether e is a reason this package knows.
func (e Exclusion) Valid() bool {
	switch e {
	case ExclusionNone, ExclusionReverseCharge, ExclusionScheduleI, ExclusionSection15, ExclusionNoCredit:
		return true
	}
	return false
}

// Label describes the exclusion and cites what puts it out of scope.
func (e Exclusion) Label() string {
	switch e {
	case ExclusionReverseCharge:
		return "Reverse charge — the second proviso to s.16(2) does not apply to supplies on which you pay the tax yourself"
	case ExclusionScheduleI:
		return "Schedule I supply — deemed paid by the first proviso to rule 37(1)"
	case ExclusionSection15:
		return "Value added under s.15(2)(b) — deemed paid by the second proviso to rule 37(1)"
	case ExclusionNoCredit:
		return "No input tax credit taken on this invoice, so there is nothing to reverse"
	default:
		return ""
	}
}

// ParseExclusion reads the exclusion written in a register cell. Blank, "no",
// "none" and "-" all mean the invoice is in scope.
func ParseExclusion(s string) (Exclusion, error) {
	raw := strings.TrimSpace(s)
	// A cell holding nothing but dashes or dots is a spreadsheet's way of
	// saying "not applicable", which here means the invoice is in scope.
	if raw == "" || strings.Trim(raw, "-–—.") == "" {
		return ExclusionNone, nil
	}
	t := strings.ToLower(raw)
	t = strings.NewReplacer(" ", "", "-", "", "_", "", ".", "", "(", "", ")", "", "/", "").Replace(t)
	switch t {
	case "", "none", "no", "n", "inscope", "nil", "na":
		return ExclusionNone, nil
	case "reversecharge", "rcm", "revchg", "reverse", "rc", "yesrcm":
		return ExclusionReverseCharge, nil
	case "schedulei", "schedule1", "schi", "sch1", "withoutconsideration", "deemedsupply":
		return ExclusionScheduleI, nil
	case "section152b", "s152b", "sec152b", "152b", "s15", "section15":
		return ExclusionSection15, nil
	case "nocredit", "noitc", "itcnotavailed", "notavailed", "ineligible", "blocked":
		return ExclusionNoCredit, nil
	}
	return ExclusionNone, fmt.Errorf("%q is not a reason this tool knows — use one of: reverse-charge, schedule-i, section-15-2-b, no-credit, or leave it blank", strings.TrimSpace(s))
}

// Payment is one amount that has actually reached the supplier against an
// invoice.
//
// A zero Date means the register recorded an amount paid but not when. The
// assessment treats such a payment as paid by the date it is run on — which is
// all the figure states — and flags the invoice so the date can be supplied.
type Payment struct {
	Date   Day    `json:"date"`
	Amount int64  `json:"amount"` // paise
	Ref    string `json:"ref,omitempty"`
	Line   int    `json:"line,omitempty"` // the line it was read from, for messages
}

// Invoice is one purchase invoice on which input tax credit has been taken, and
// the payments made against it. All money is whole paise.
type Invoice struct {
	ID            string `json:"id"`
	SupplierName  string `json:"supplierName"`
	SupplierGSTIN string `json:"supplierGstin"`
	InvoiceNo     string `json:"invoiceNo"`
	InvoiceDate   Day    `json:"invoiceDate"`

	// TaxableValue is the value of the supply before tax.
	TaxableValue int64 `json:"taxableValue"`

	// The four heads hold the input tax credit AVAILED on this invoice, not
	// necessarily the tax the supplier charged: rule 37(1) reverses "the input
	// tax credit availed in respect of such supply", so where part of the tax
	// was never taken as credit only what was taken is at stake.
	ITC Heads `json:"itc"`

	// InvoiceValue is the whole amount that must reach the supplier — the value
	// of the supply together with the tax on it — because that is what the
	// second proviso to section 16(2) requires to be paid. Left at zero it is
	// taken as the taxable value plus the credit heads.
	InvoiceValue int64 `json:"invoiceValue,omitempty"`

	Payments  []Payment `json:"payments,omitempty"`
	Exclusion Exclusion `json:"exclusion,omitempty"`
	Note      string    `json:"note,omitempty"`

	// Line is the line of the register this invoice was read from. It is kept
	// so a problem can be reported against the row the user can see.
	Line int `json:"line,omitempty"`
}

// Payable is the amount that must reach the supplier for the condition in the
// second proviso to section 16(2) to be satisfied: the value of the supply
// along with the tax payable on it.
func (inv Invoice) Payable() int64 {
	if inv.InvoiceValue > 0 {
		return inv.InvoiceValue
	}
	return inv.TaxableValue + inv.ITC.Total()
}

// PaidBy is the total that had reached the supplier on or before d. A payment
// with no date counts only from the date the assessment is run on, so it is
// included here only when d is that date or later — the caller passes its own
// as-of date for that purpose.
func (inv Invoice) PaidBy(d Day, undatedFrom Day) int64 {
	var sum int64
	for _, p := range inv.Payments {
		eff := p.Date
		if eff.IsZero() {
			eff = undatedFrom
		}
		if eff.IsZero() || eff.OnOrBefore(d) {
			sum += p.Amount
		}
	}
	return sum
}

// PaidTotal is everything recorded as paid against the invoice, whenever.
func (inv Invoice) PaidTotal() int64 {
	var sum int64
	for _, p := range inv.Payments {
		sum += p.Amount
	}
	return sum
}

// HasUndatedPayment reports whether any payment was recorded without a date.
func (inv Invoice) HasUndatedPayment() bool {
	for _, p := range inv.Payments {
		if p.Date.IsZero() && p.Amount != 0 {
			return true
		}
	}
	return false
}

// SortPayments puts the payments in date order, undated ones last, so a working
// paper reads the way a ledger does.
func (inv *Invoice) SortPayments() {
	sort.SliceStable(inv.Payments, func(i, j int) bool {
		a, b := inv.Payments[i], inv.Payments[j]
		switch {
		case a.Date.IsZero() && b.Date.IsZero():
			return false
		case a.Date.IsZero():
			return false
		case b.Date.IsZero():
			return true
		}
		return a.Date.Before(b.Date)
	})
}

// Describe names the invoice for a message or a table: "INV/0042 — Sundar Metals".
func (inv Invoice) Describe() string {
	who := strings.TrimSpace(inv.SupplierName)
	if who == "" {
		who = inv.SupplierGSTIN
	}
	if who == "" {
		return inv.InvoiceNo
	}
	return inv.InvoiceNo + " — " + who
}

// Validate reports anything that would make the invoice impossible to assess.
//
// It is deliberately narrow: a doubtful but usable row is flagged by the
// assessment rather than refused here, because refusing a row loses it. A
// register with no supplier column at all is still worth ageing, so the
// supplier is not required — its absence is flagged instead.
func (inv Invoice) Validate() error {
	switch {
	case strings.TrimSpace(inv.InvoiceNo) == "":
		return fmt.Errorf("the invoice number is needed")
	case inv.InvoiceDate.IsZero():
		return fmt.Errorf("the invoice date is needed — the 180 days are counted from it")
	case inv.TaxableValue < 0:
		return fmt.Errorf("the taxable value cannot be negative")
	case inv.ITC.IGST < 0 || inv.ITC.CGST < 0 || inv.ITC.SGST < 0 || inv.ITC.Cess < 0:
		return fmt.Errorf("input tax credit cannot be negative — a credit note reduces the credit on its own invoice, it is not a negative invoice")
	case inv.InvoiceValue < 0:
		return fmt.Errorf("the invoice value cannot be negative")
	case !inv.Exclusion.Valid():
		return fmt.Errorf("%q is not a reason this tool knows for the 180-day condition not to apply", inv.Exclusion)
	}
	for i, p := range inv.Payments {
		if p.Amount < 0 {
			return fmt.Errorf("payment %d is negative — record a refund by reducing the amount paid, not as a negative payment", i+1)
		}
	}
	// Credit with no value behind it makes the proportion in rule 37(1)
	// meaningless: the credit would be apportioned against the tax alone, so a
	// part payment of the tax would read as the whole invoice settled.
	if inv.TaxableValue == 0 && inv.InvoiceValue == 0 && inv.ITC.Total() > 0 {
		return fmt.Errorf("this invoice carries input tax credit but no value to pay — enter the taxable value, or the invoice total including tax")
	}
	return nil
}

// NormaliseGSTIN trims and upper-cases a GSTIN and removes inner spaces, so a
// register that writes "29 AAGCB 7383 J1ZS" still matches a payment file that
// writes "29AAGCB7383J1ZS".
func NormaliseGSTIN(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// NormaliseInvoiceNo reduces an invoice number to the form used for matching a
// payment to the invoice it is against: upper-cased, with spaces and the
// separators / - \ . removed, and with the leading zeros of every run of digits
// dropped.
//
// Nobody types an invoice number twice the same way, so "inv-001/26", "INV1/26"
// and "Inv 001 / 26" all reduce to "INV126". Leading zeros are dropped run by
// run, before the separators go, so two genuinely different numbers do not
// collapse into one: "INV-10/026" gives "INV1026" while "INV-100/26" gives
// "INV10026".
func NormaliseInvoiceNo(s string) string {
	var out, digits strings.Builder
	flush := func() {
		if digits.Len() == 0 {
			return
		}
		d := strings.TrimLeft(digits.String(), "0")
		if d == "" {
			d = "0" // a run of zeros is the number zero, not nothing
		}
		out.WriteString(d)
		digits.Reset()
	}
	for _, r := range strings.ToUpper(s) {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case unicode.IsSpace(r), r == '/', r == '-', r == '\\', r == '.':
			flush()
		default:
			flush()
			out.WriteRune(r)
		}
	}
	flush()
	return out.String()
}

// gstinLength is the fixed length of a GSTIN: a 2-digit State code, the
// 10-character PAN, an entity character, the letter Z and a check character.
const gstinLength = 15

// ValidGSTINFormat reports whether s has the shape of a Goods and Services Tax
// Identification Number. Only the shape is checked, not the check character: a
// mis-typed GSTIN is worth flagging for the user to look at, never worth
// dropping the row for.
func ValidGSTINFormat(s string) bool {
	g := NormaliseGSTIN(s)
	if len(g) != gstinLength {
		return false
	}
	isDigit := func(b byte) bool { return b >= '0' && b <= '9' }
	isAlpha := func(b byte) bool { return b >= 'A' && b <= 'Z' }
	isAlnum := func(b byte) bool { return isDigit(b) || isAlpha(b) }
	for i := 0; i < gstinLength; i++ {
		var ok bool
		switch {
		case i < 2:
			ok = isDigit(g[i]) // State code
		case i < 7:
			ok = isAlpha(g[i]) // PAN: first five letters
		case i < 11:
			ok = isDigit(g[i]) // PAN: four digits
		case i == 11:
			ok = isAlpha(g[i]) // PAN: last letter
		case i == 12:
			ok = isAlnum(g[i]) // entity number
		case i == 13:
			ok = g[i] == 'Z'
		default:
			ok = isAlnum(g[i]) // check character
		}
		if !ok {
			return false
		}
	}
	return true
}
