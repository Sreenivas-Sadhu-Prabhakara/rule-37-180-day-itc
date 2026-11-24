package rule37

import (
	"io"
	"os"
	"strings"
	"testing"
)

const goodRegister = `Purchase register — Nagarjuna Fabricators
For the period 1 April 2026 to 31 March 2027

Supplier GSTIN,Supplier name,Invoice no,Invoice date,Taxable value,IGST,CGST,SGST,Cess,Invoice value,Amount paid,Payment date,Out-of-scope reason,Remarks
27AAPFU0939F1ZV,Sundar Metals,INV/001/26-27,15-07-2026,"1,00,000.00","18,000.00",0,0,0,"1,18,000.00",0,,,
29AABCU9603R1ZM,Nandi Hardware,NH-0045,24-11-2026,100000,0,9000,9000,0,118000,"50,000.00",01-12-2026,,part paid
29AAGCB7383J1ZS,Ganga Transport,GT/900,15-07-2026,50000,2500,-,-,-,52500,-,-,RCM,freight
27AAPFU0939F1ZV,Sundar Metals,INV/002/26-27,04-10-2026,₹1,00,000.00,5000,0,0,0,105000,,,,
TOTAL,,,,350000,25500,9000,9000,0,393500,50000,,,
`

func TestParseRegisterCSV(t *testing.T) {
	got, err := ParseRegisterCSV(strings.NewReader(goodRegister))
	if err != nil {
		t.Fatalf("ParseRegisterCSV: %v", err)
	}
	if len(got.Invoices) != 4 {
		t.Fatalf("read %d invoices, want 4: %+v", len(got.Invoices), got.Invoices)
	}
	if len(got.Problems) != 0 {
		t.Errorf("unexpected problems: %+v", got.Problems)
	}
	if got.Skipped != 1 {
		t.Errorf("skipped %d rows, want 1 (the total line)", got.Skipped)
	}

	first := got.Invoices[0]
	if first.SupplierGSTIN != "27AAPFU0939F1ZV" || first.SupplierName != "Sundar Metals" {
		t.Errorf("first supplier = %q / %q", first.SupplierGSTIN, first.SupplierName)
	}
	if first.InvoiceNo != "INV/001/26-27" {
		t.Errorf("first invoice number = %q", first.InvoiceNo)
	}
	if got, want := first.InvoiceDate.String(), "2026-07-15"; got != want {
		t.Errorf("first invoice date = %s, want %s", got, want)
	}
	if first.TaxableValue != 10_000_000 || first.ITC.IGST != 1_800_000 || first.InvoiceValue != 11_800_000 {
		t.Errorf("first invoice amounts = %d / %+v / %d", first.TaxableValue, first.ITC, first.InvoiceValue)
	}
	if len(first.Payments) != 0 {
		t.Errorf("the first invoice has %d payments, want none", len(first.Payments))
	}
	if first.Line != 5 {
		t.Errorf("the first invoice came from line %d, want 5 (two title rows, a blank, then the headings)", first.Line)
	}

	// Central and State tax, a part payment with its date, and a remark.
	second := got.Invoices[1]
	if second.ITC != (Heads{CGST: 900_000, SGST: 900_000}) {
		t.Errorf("second invoice credit = %+v", second.ITC)
	}
	if len(second.Payments) != 1 {
		t.Fatalf("second invoice has %d payments, want 1", len(second.Payments))
	}
	if second.Payments[0].Amount != 5_000_000 {
		t.Errorf("second invoice payment = %s", Rupees(second.Payments[0].Amount))
	}
	if got, want := second.Payments[0].Date.String(), "2026-12-01"; got != want {
		t.Errorf("second invoice payment date = %s, want %s", got, want)
	}
	if second.Note != "part paid" {
		t.Errorf("second invoice remark = %q", second.Note)
	}

	// Reverse charge out of scope, with dashes read as nil.
	third := got.Invoices[2]
	if third.Exclusion != ExclusionReverseCharge {
		t.Errorf("third invoice exclusion = %q, want %q", third.Exclusion, ExclusionReverseCharge)
	}
	if third.ITC.CGST != 0 || third.ITC.SGST != 0 || third.ITC.Cess != 0 {
		t.Errorf("dashes should read as nil: %+v", third.ITC)
	}
}

func TestParseRegisterCSVColumnAliases(t *testing.T) {
	// The headings an accounting package actually writes, with units and
	// currency marks in them, and in a different order.
	in := `Invoice Number,Date of Invoice,GSTIN of Supplier,Party Name,Assessable Value,IGST Amount (₹),CGST Amount,SGST/UTGST Amount,Cess Amt,Gross Total,Paid to Supplier,Date Paid
INV-9,15/07/2026,27AAPFU0939F1ZV,Sundar Metals,100000.00,18000.00,0.00,0.00,0.00,118000.00,0.00,
`
	got, err := ParseRegisterCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseRegisterCSV: %v", err)
	}
	if len(got.Invoices) != 1 {
		t.Fatalf("read %d invoices, want 1", len(got.Invoices))
	}
	inv := got.Invoices[0]
	if inv.SupplierGSTIN != "27AAPFU0939F1ZV" || inv.SupplierName != "Sundar Metals" {
		t.Errorf("supplier not found: %q / %q", inv.SupplierGSTIN, inv.SupplierName)
	}
	if inv.TaxableValue != 10_000_000 || inv.ITC.IGST != 1_800_000 || inv.InvoiceValue != 11_800_000 {
		t.Errorf("amounts = %d / %+v / %d", inv.TaxableValue, inv.ITC, inv.InvoiceValue)
	}
	if got, want := inv.InvoiceDate.String(), "2026-07-15"; got != want {
		t.Errorf("date = %s, want %s", got, want)
	}
}

func TestParseRegisterCSVIgnoresARateColumn(t *testing.T) {
	// A "GST rate" column must not be mistaken for a tax amount.
	in := `Invoice no,Invoice date,Taxable value,GST Rate,IGST,Invoice value
INV-1,15-07-2026,100000,18,18000,118000
`
	got, err := ParseRegisterCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseRegisterCSV: %v", err)
	}
	if got.Invoices[0].ITC.IGST != 1_800_000 {
		t.Errorf("IGST = %s, want ₹18,000 — the rate column should be ignored", Rupees(got.Invoices[0].ITC.IGST))
	}
}

func TestParseRegisterCSVDelimiters(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "semicolons", in: "Invoice no;Invoice date;Taxable value;IGST;Invoice value\nINV-1;15-07-2026;100000;18000;118000\n"},
		{name: "tabs", in: "Invoice no\tInvoice date\tTaxable value\tIGST\tInvoice value\nINV-1\t15-07-2026\t100000\t18000\t118000\n"},
		{name: "pipes", in: "Invoice no|Invoice date|Taxable value|IGST|Invoice value\nINV-1|15-07-2026|100000|18000|118000\n"},
		{name: "a byte-order mark", in: "\ufeffInvoice no,Invoice date,Taxable value,IGST,Invoice value\nINV-1,15-07-2026,100000,18000,118000\n"},
		{name: "carriage returns", in: "Invoice no,Invoice date,Taxable value,IGST,Invoice value\r\nINV-1,15-07-2026,100000,18000,118000\r\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseRegisterCSV(strings.NewReader(c.in))
			if err != nil {
				t.Fatalf("ParseRegisterCSV: %v", err)
			}
			if len(got.Invoices) != 1 {
				t.Fatalf("read %d invoices, want 1", len(got.Invoices))
			}
			if got.Invoices[0].ITC.IGST != 1_800_000 {
				t.Errorf("IGST = %s, want ₹18,000", Rupees(got.Invoices[0].ITC.IGST))
			}
		})
	}
}

// TestParseRegisterCSVReportsBadRowsByLine is the defensiveness requirement: a
// bad row is reported with the line number and the field, and every other row
// still imports.
func TestParseRegisterCSVReportsBadRowsByLine(t *testing.T) {
	in := `Supplier GSTIN,Supplier name,Invoice no,Invoice date,Taxable value,IGST,Invoice value,Amount paid,Payment date,Out-of-scope reason
27AAPFU0939F1ZV,Good One,INV-1,15-07-2026,100000,18000,118000,,,
27AAPFU0939F1ZV,No Number,,15-07-2026,100000,18000,118000,,,
27AAPFU0939F1ZV,No Date,INV-3,,100000,18000,118000,,,
27AAPFU0939F1ZV,Bad Date,INV-4,32-13-2026,100000,18000,118000,,,
27AAPFU0939F1ZV,Impossible Date,INV-5,29-02-2027,100000,18000,118000,,,
27AAPFU0939F1ZV,Bad Amount,INV-6,15-07-2026,one lakh,18000,118000,,,
27AAPFU0939F1ZV,Bad Tax,INV-7,15-07-2026,100000,eighteen,118000,,,
27AAPFU0939F1ZV,Bad Reason,INV-8,15-07-2026,100000,18000,118000,,,because I said so
27AAPFU0939F1ZV,Bad Pay Date,INV-9,15-07-2026,100000,18000,118000,5000,yesterday,
27AAPFU0939F1ZV,Negative Pay,INV-10,15-07-2026,100000,18000,118000,-5000,01-08-2026,
27AAPFU0939F1ZV,Good One,INV-1,15-07-2026,100000,18000,118000,,,
27AAPFU0939F1ZV,Another Good,INV-11,15-07-2026,100000,18000,118000,,,
`
	got, err := ParseRegisterCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseRegisterCSV should import the good rows: %v", err)
	}
	if len(got.Invoices) != 2 {
		t.Errorf("imported %d invoices, want 2 (INV-1 and INV-11): %+v", len(got.Invoices), got.Invoices)
	}

	wantProblemLines := []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	if len(got.Problems) != len(wantProblemLines) {
		t.Fatalf("reported %d problems, want %d: %+v", len(got.Problems), len(wantProblemLines), got.Problems)
	}
	for i, want := range wantProblemLines {
		if got.Problems[i].Line != want {
			t.Errorf("problem %d is on line %d, want line %d (%s)", i, got.Problems[i].Line, want, got.Problems[i].Reason)
		}
		if got.Problems[i].Reason == "" {
			t.Errorf("problem on line %d has no reason", got.Problems[i].Line)
		}
		if got.Problems[i].String() == "" {
			t.Errorf("problem on line %d does not describe itself", got.Problems[i].Line)
		}
	}

	// The duplicate on line 12 names the line the first copy came from, since
	// importing an invoice twice would reverse its credit twice.
	dup := problemOnLine(t, got.Problems, 12)
	if !strings.Contains(dup.Reason, "line 2") {
		t.Errorf("the duplicate should point at the first copy: %q", dup.Reason)
	}
	// A negative payment is refused with a message about how to record a refund.
	neg := problemOnLine(t, got.Problems, 11)
	if !strings.Contains(neg.Reason, "negative") {
		t.Errorf("the negative payment was reported as %q", neg.Reason)
	}
	// A rejected row is reported with the field that is wrong.
	if got.Problems[0].Field == "" {
		t.Error("a problem should name the column it is in")
	}
	// And the whole file carries a note saying rows were left out.
	if len(got.Notes) == 0 {
		t.Error("the import should note that rows were left out")
	}
}

func TestParseRegisterCSVErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "blank lines only", in: "\n\n   \n"},
		{name: "no heading row", in: "a,b,c\n1,2,3\n"},
		{name: "headings but no rows", in: "Invoice no,Invoice date,IGST\n"},
		{name: "no tax columns at all", in: "Invoice no,Invoice date,Taxable value\nINV-1,15-07-2026,100000\n"},
		{name: "every row unusable", in: "Invoice no,Invoice date,IGST\n,15-07-2026,100\n,16-07-2026,100\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseRegisterCSV(strings.NewReader(c.in)); err == nil {
				t.Error("want an error")
			}
		})
	}
	if _, err := ParseRegisterCSV(nil); err == nil {
		t.Error("a nil reader should be an error, not a panic")
	}
}

func TestParseRegisterCSVNotesWhatIsMissing(t *testing.T) {
	// No payment columns at all: every invoice reads as wholly unpaid, and the
	// user is told so rather than left to assume the figures are complete.
	in := `Invoice no,Invoice date,Taxable value,IGST
INV-1,15-07-2026,100000,18000
`
	got, err := ParseRegisterCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseRegisterCSV: %v", err)
	}
	joined := strings.Join(got.Notes, " | ")
	for _, want := range []string{"Amount paid", "CGST", "GSTIN"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the notes should mention %q: %s", want, joined)
		}
	}

	// A paid column with no date column is worth saying out loud, because it
	// changes every reversal.
	in2 := `Invoice no,Invoice date,Taxable value,IGST,Amount paid
INV-1,15-07-2026,100000,18000,50000
`
	got2, err := ParseRegisterCSV(strings.NewReader(in2))
	if err != nil {
		t.Fatalf("ParseRegisterCSV: %v", err)
	}
	if !strings.Contains(strings.Join(got2.Notes, " | "), "Payment date") {
		t.Errorf("the missing payment date should be noted: %v", got2.Notes)
	}
	if len(got2.Invoices[0].Payments) != 1 || !got2.Invoices[0].Payments[0].Date.IsZero() {
		t.Errorf("the undated payment should be kept without a date: %+v", got2.Invoices[0].Payments)
	}
}

func TestParseRegisterCSVSurvivesRaggedRows(t *testing.T) {
	// Short rows, a row of nothing but separators, and trailing blank lines are
	// all things a spreadsheet export produces. None of them may panic.
	in := `Supplier GSTIN,Supplier name,Invoice no,Invoice date,Taxable value,IGST,Invoice value
27AAPFU0939F1ZV,Sundar Metals,INV-1,15-07-2026,100000,18000,118000
27AAPFU0939F1ZV,Short Row,INV-2,15-07-2026
,,,,,,
27AAPFU0939F1ZV,Trailing,INV-3,15-07-2026,100000,18000,118000,extra,cells,here

`
	got, err := ParseRegisterCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseRegisterCSV: %v", err)
	}
	// The short row has an invoice number and a date, so it imports with nil
	// amounts and is excluded as carrying no credit; the extra cells on the
	// last row are simply ignored.
	if len(got.Invoices) != 3 {
		t.Errorf("imported %d invoices, want 3: %+v", len(got.Invoices), got.Invoices)
	}
	lines := AssessAll(got.Invoices, mustDay(t, "2027-02-01"), DefaultSettings())
	if lines[1].Assessment.Stage != StageExcluded {
		t.Errorf("the short row assessed as %q, want %q", lines[1].Assessment.Stage, StageExcluded)
	}
}

const goodPayments = `Payments to suppliers
Supplier GSTIN,Invoice no,Payment date,Amount,UTR no
27AAPFU0939F1ZV,INV/001/26-27,05-01-2027,"1,18,000.00",UTR123456
29AABCU9603R1ZM,NH-0045,01-02-2027,"68,000.00",UTR123457
27AAPFU0939F1ZV,INV/002/26-27,10-04-2027,"52,500.00",
`

func TestParsePaymentsCSVAndAttach(t *testing.T) {
	reg, err := ParseRegisterCSV(strings.NewReader(goodRegister))
	if err != nil {
		t.Fatal(err)
	}
	pays, err := ParsePaymentsCSV(strings.NewReader(goodPayments))
	if err != nil {
		t.Fatalf("ParsePaymentsCSV: %v", err)
	}
	if len(pays.Rows) != 3 {
		t.Fatalf("read %d payments, want 3: %+v", len(pays.Rows), pays.Rows)
	}
	if got, want := pays.Rows[0].Payment.Amount, int64(11_800_000); got != want {
		t.Errorf("first payment = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := pays.Rows[0].Payment.Date.String(), "2027-01-05"; got != want {
		t.Errorf("first payment date = %s, want %s", got, want)
	}
	if got, want := pays.Rows[0].Payment.Ref, "UTR123456"; got != want {
		t.Errorf("first payment reference = %q, want %q", got, want)
	}

	invoices, problems := AttachPayments(reg.Invoices, pays.Rows)
	if len(problems) != 0 {
		t.Errorf("unexpected problems attaching payments: %+v", problems)
	}
	if len(invoices[0].Payments) != 1 || invoices[0].Payments[0].Amount != 11_800_000 {
		t.Errorf("INV/001 payments = %+v", invoices[0].Payments)
	}
	// The second invoice already had a part payment in the register; the
	// payments file adds the balance, in date order.
	if len(invoices[1].Payments) != 2 {
		t.Fatalf("NH-0045 has %d payments, want 2", len(invoices[1].Payments))
	}
	if invoices[1].Payments[0].Amount != 5_000_000 || invoices[1].Payments[1].Amount != 6_800_000 {
		t.Errorf("NH-0045 payments out of order: %+v", invoices[1].Payments)
	}

	// The register the payments were attached to is left untouched, so an
	// import can be re-run without doubling anything.
	if len(reg.Invoices[0].Payments) != 0 {
		t.Error("AttachPayments modified the invoices it was given")
	}

	// Paid in full on 5 January, the day before its 11 January deadline, the
	// first invoice is settled with nothing to reverse.
	lines := AssessAll(invoices, mustDay(t, "2027-05-01"), DefaultSettings())
	if lines[0].Assessment.Stage != StageSettled {
		t.Errorf("INV/001 is %q (%s), want %q", lines[0].Assessment.Stage, lines[0].Assessment.Reason, StageSettled)
	}
	// NH-0045 was paid in full on 1 February, well inside its 23 May window.
	if lines[1].Assessment.Stage != StageSettled {
		t.Errorf("NH-0045 is %q (%s), want %q", lines[1].Assessment.Stage, lines[1].Assessment.Reason, StageSettled)
	}
	// INV/002 was paid on 10 April, eight days after its 2 April deadline, so
	// the reversal stands and the whole of it can be taken again.
	third := lines[3].Assessment
	if third.Stage != StageReclaim {
		t.Errorf("INV/002 is %q (%s), want %q", third.Stage, third.Reason, StageReclaim)
	}
	if third.Reversal.Total() != 500_000 || third.Reclaim.Total() != 500_000 {
		t.Errorf("INV/002 reversal %s and re-claim %s, want ₹5,000 each",
			Rupees(third.Reversal.Total()), Rupees(third.Reclaim.Total()))
	}
}

func TestAttachPaymentsReportsWhatItCannotMatch(t *testing.T) {
	invoices := []Invoice{
		{InvoiceNo: "INV-1", SupplierGSTIN: "27AAPFU0939F1ZV", SupplierName: "A", InvoiceDate: mustDay(t, "2026-07-15"), TaxableValue: 100, Line: 2},
		// The same invoice number from a different supplier, which is why a
		// payment with no GSTIN cannot be placed.
		{InvoiceNo: "INV-1", SupplierGSTIN: "29AABCU9603R1ZM", SupplierName: "B", InvoiceDate: mustDay(t, "2026-07-16"), TaxableValue: 100, Line: 3},
		{InvoiceNo: "INV-2", SupplierGSTIN: "27AAPFU0939F1ZV", SupplierName: "A", InvoiceDate: mustDay(t, "2026-07-17"), TaxableValue: 100, Line: 4},
	}
	rows := []PaymentRow{
		// Matched on GSTIN and number together.
		{GSTIN: "27AAPFU0939F1ZV", InvoiceNo: "inv 1", Payment: Payment{Amount: 10, Line: 2}},
		// No GSTIN and an ambiguous number.
		{InvoiceNo: "INV-1", Payment: Payment{Amount: 20, Line: 3}},
		// No such invoice.
		{GSTIN: "27AAPFU0939F1ZV", InvoiceNo: "INV-999", Payment: Payment{Amount: 30, Line: 4}},
		// No GSTIN, but the number is unique, so it still lands.
		{InvoiceNo: "INV/0002", Payment: Payment{Amount: 40, Line: 5}},
	}
	got, problems := AttachPayments(invoices, rows)

	if len(got[0].Payments) != 1 || got[0].Payments[0].Amount != 10 {
		t.Errorf("the matched payment went astray: %+v", got[0].Payments)
	}
	if len(got[2].Payments) != 1 || got[2].Payments[0].Amount != 40 {
		t.Errorf("a unique invoice number should match without a GSTIN: %+v", got[2].Payments)
	}
	if len(problems) != 2 {
		t.Fatalf("reported %d problems, want 2: %+v", len(problems), problems)
	}
	if problems[0].Line != 3 || !strings.Contains(problems[0].Reason, "more than one invoice") {
		t.Errorf("the ambiguous payment was reported as %+v", problems[0])
	}
	if !strings.Contains(problems[0].Reason, "line 2") || !strings.Contains(problems[0].Reason, "line 3") {
		t.Errorf("the ambiguity should name the register lines involved: %q", problems[0].Reason)
	}
	if problems[1].Line != 4 || !strings.Contains(problems[1].Reason, "no invoice with that number") {
		t.Errorf("the unmatched payment was reported as %+v", problems[1])
	}
	// An ambiguous payment is not applied to either candidate.
	if len(got[1].Payments) != 0 {
		t.Errorf("an ambiguous payment was applied anyway: %+v", got[1].Payments)
	}
}

func TestParsePaymentsCSVProblemsAndErrors(t *testing.T) {
	in := `Invoice no,Payment date,Amount
INV-1,05-01-2027,118000
,05-01-2027,5000
INV-3,nonsense,5000
INV-4,05-01-2027,five thousand
INV-5,05-01-2027,-5000
INV-6,05-01-2027,0
TOTAL,,128000
`
	got, err := ParsePaymentsCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParsePaymentsCSV: %v", err)
	}
	if len(got.Rows) != 1 {
		t.Errorf("read %d payments, want 1: %+v", len(got.Rows), got.Rows)
	}
	if len(got.Problems) != 4 {
		t.Errorf("reported %d problems, want 4: %+v", len(got.Problems), got.Problems)
	}
	// A nil payment and the total line are skipped quietly, not complained of.
	if got.Skipped != 2 {
		t.Errorf("skipped %d rows, want 2 (a nil amount and the total line)", got.Skipped)
	}

	bad := []struct{ name, in string }{
		{name: "empty", in: ""},
		{name: "no heading row", in: "a,b\n1,2\n"},
		{name: "no amount column", in: "Invoice no,Payment date\nINV-1,05-01-2027\n"},
		{name: "headings but no rows", in: "Invoice no,Payment date,Amount\n"},
		{name: "every row unusable", in: "Invoice no,Payment date,Amount\n,05-01-2027,100\n,06-01-2027,100\n"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParsePaymentsCSV(strings.NewReader(c.in)); err == nil {
				t.Error("want an error")
			}
		})
	}
	if _, err := ParsePaymentsCSV(nil); err == nil {
		t.Error("a nil reader should be an error, not a panic")
	}

	// Without a payment date column, the fact is noted and the payments are
	// kept undated rather than guessed at.
	undated, err := ParsePaymentsCSV(strings.NewReader("Invoice no,Amount\nINV-1,5000\n"))
	if err != nil {
		t.Fatalf("ParsePaymentsCSV: %v", err)
	}
	if !undated.Rows[0].Payment.Date.IsZero() {
		t.Error("a payment with no date column should stay undated")
	}
	if !strings.Contains(strings.Join(undated.Notes, " | "), "payment date") {
		t.Errorf("the missing dates should be noted: %v", undated.Notes)
	}
}

func TestParseCSVRefusesAFileTooLargeToBeARegister(t *testing.T) {
	// A single very long line stands in for a file that is not a register at
	// all. It must be refused rather than read into memory without limit.
	huge := strings.Repeat("x", maxFileBytes+1)
	if _, err := ParseRegisterCSV(strings.NewReader(huge)); err == nil {
		t.Error("want an error for an oversized file")
	} else if !strings.Contains(err.Error(), "MB") {
		t.Errorf("the error should say how large is too large: %v", err)
	}
}

func problemOnLine(t *testing.T, problems []RowProblem, line int) RowProblem {
	t.Helper()
	for _, p := range problems {
		if p.Line == line {
			return p
		}
	}
	t.Fatalf("no problem reported on line %d: %+v", line, problems)
	return RowProblem{}
}

func TestRowProblemString(t *testing.T) {
	cases := []struct {
		in   RowProblem
		want string
	}{
		{in: RowProblem{Line: 7, Field: "Invoice date", Value: "32-13-2026", Reason: "not a date"},
			want: `line 7, Invoice date "32-13-2026": not a date`},
		{in: RowProblem{Line: 7, Field: "Invoice no", Reason: "missing"}, want: "line 7, Invoice no: missing"},
		{in: RowProblem{Line: 7, Reason: "unreadable"}, want: "line 7: unreadable"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}

// TestParseSampleFiles reads the sample register and payments file shipped with
// the tool, so the example in the documentation is known to work and to produce
// the figures the documentation quotes.
func TestParseSampleFiles(t *testing.T) {
	reg := openSample(t, "testdata/purchase-register-sample.csv", func(r io.Reader) (any, error) {
		return ParseRegisterCSV(r)
	}).(Import)
	if len(reg.Invoices) != 6 {
		t.Fatalf("the sample register has %d invoices, want 6", len(reg.Invoices))
	}
	if len(reg.Problems) != 0 {
		t.Errorf("the sample register should import cleanly: %+v", reg.Problems)
	}
	if reg.Skipped != 1 {
		t.Errorf("the sample register's total line should be left out; skipped %d", reg.Skipped)
	}

	pays := openSample(t, "testdata/supplier-payments-sample.csv", func(r io.Reader) (any, error) {
		return ParsePaymentsCSV(r)
	}).(PaymentImport)
	if len(pays.Rows) != 2 {
		t.Fatalf("the sample payments file has %d rows, want 2", len(pays.Rows))
	}

	invoices, problems := AttachPayments(reg.Invoices, pays.Rows)
	if len(problems) != 0 {
		t.Errorf("every sample payment should match an invoice: %+v", problems)
	}

	set := DefaultSettings()
	asOf := mustDay(t, "2027-03-01")
	lines := AssessAll(invoices, asOf, set)
	s := Totals(lines, asOf, set)

	// The figures the documentation quotes for this register. Only SM/002 is
	// still inside its window with something unpaid: NH-0045's balance arrives
	// in the payments file, inside its 180 days, which settles it.
	if got, want := s.AtRisk.Total(), int64(500_000); got != want {
		t.Errorf("sample credit at risk = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.AtRiskInvoices, 1; got != want {
		t.Errorf("sample invoices at risk = %d, want %d", got, want)
	}
	if got, want := s.Counted[StageSettled], 2; got != want {
		t.Errorf("sample invoices paid in time = %d, want %d", got, want)
	}
	if got, want := s.ReversalDue.Total(), int64(4_600_000); got != want {
		t.Errorf("sample reversal due = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.ReclaimAvailable.Total(), int64(1_800_000); got != want {
		t.Errorf("sample re-claim available = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.Excluded, 1; got != want {
		t.Errorf("sample out-of-scope invoices = %d, want %d", got, want)
	}
}

func openSample(t *testing.T, path string, parse func(io.Reader) (any, error)) any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	got, err := parse(f)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return got
}
