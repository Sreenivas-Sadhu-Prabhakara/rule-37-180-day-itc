package rule37

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

func TestCSVViews(t *testing.T) {
	set := DefaultSettings()
	asOf := mustDay(t, "2027-03-01")
	invoices := testPortfolio(t)
	// Invoice A paid in full a month after its deadline, so there is something
	// in every list.
	invoices[0].Payments = []Payment{{Date: mustDay(t, "2027-02-10"), Amount: 11_800_000}}
	lines := AssessAll(invoices, asOf, set)
	plan := PayFirst(lines, asOf, set, 10_000_000)

	cases := []struct {
		view     View
		wantRows int // including the heading row
		wantHead string
	}{
		{view: ViewWatchlist, wantRows: 3, wantHead: "ITC at risk total"},
		{view: ViewReversals, wantRows: 3, wantHead: "Reverse in return"},
		{view: ViewReclaims, wantRows: 2, wantHead: "Claim in return"},
		// Only one invoice is both still inside its window and inside the
		// default 60-day planning horizon as at 1 March 2027.
		{view: ViewPlan, wantRows: 2, wantHead: "Paise protected per rupee"},
		{view: ViewAll, wantRows: 7, wantHead: "Position"},
	}
	for _, c := range cases {
		t.Run(string(c.view), func(t *testing.T) {
			raw, err := CSV(c.view, lines, plan, asOf, set)
			if err != nil {
				t.Fatalf("CSV(%q): %v", c.view, err)
			}
			rows, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
			if err != nil {
				t.Fatalf("the output is not valid CSV: %v", err)
			}
			if len(rows) != c.wantRows {
				t.Errorf("%q has %d rows, want %d", c.view, len(rows), c.wantRows)
			}
			if !strings.Contains(strings.Join(rows[0], ","), c.wantHead) {
				t.Errorf("%q headings do not mention %q: %v", c.view, c.wantHead, rows[0])
			}
			// Every row must have the same number of cells as the heading, or a
			// spreadsheet will misalign the columns.
			for i, r := range rows {
				if len(r) != len(rows[0]) {
					t.Errorf("row %d has %d cells, want %d", i, len(r), len(rows[0]))
				}
			}
			if c.view.Title() == "" {
				t.Errorf("%q has no title", c.view)
			}
			if !c.view.Valid() {
				t.Errorf("%q should be a valid view", c.view)
			}
		})
	}

	if _, err := CSV("invented", lines, plan, asOf, set); err == nil {
		t.Error("an unknown view should be an error")
	}
	if View("invented").Valid() {
		t.Error("an unknown view should not be valid")
	}
}

func TestCSVAmountsAreMachineReadable(t *testing.T) {
	set := DefaultSettings()
	asOf := mustDay(t, "2027-02-01")
	lines := AssessAll(testPortfolio(t), asOf, set)

	raw, err := CSV(ViewWatchlist, lines, Plan{}, asOf, set)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	// No currency marks and no digit grouping, so the figures read back as
	// numbers in a spreadsheet.
	if strings.Contains(text, "₹") {
		t.Error("the CSV should not carry a currency mark")
	}
	rows, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	// The watch-list is in deadline order, soonest first, which is the order to
	// act in.
	var last string
	for _, r := range rows[1:] {
		payBy := r[7]
		if payBy == "" {
			t.Fatalf("a watch-list row has no deadline: %v", r)
		}
		if last != "" && indianDateKey(payBy) < indianDateKey(last) {
			t.Errorf("the watch-list is out of order: %s came after %s", payBy, last)
		}
		last = payBy
	}
	// Amounts parse back exactly.
	first := rows[1]
	if got, err := ParseAmount(first[4]); err != nil || got != 12_800_000 {
		t.Errorf("the invoice total read back as %d (%v), want 12800000", got, err)
	}
}

func TestCSVOnAnEmptyRegister(t *testing.T) {
	// Every view must still produce its headings, so a download is never an
	// empty file a spreadsheet refuses to open.
	for _, v := range Views {
		raw, err := CSV(v, nil, Plan{}, mustDay(t, "2027-02-01"), DefaultSettings())
		if err != nil {
			t.Fatalf("CSV(%q) on nothing: %v", v, err)
		}
		rows, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
		if err != nil {
			t.Fatalf("CSV(%q) is not valid CSV: %v", v, err)
		}
		if len(rows) != 1 {
			t.Errorf("CSV(%q) on nothing has %d rows, want just the headings", v, len(rows))
		}
	}
}

func TestWorkingPaper(t *testing.T) {
	set := DefaultSettings()
	asOf := mustDay(t, "2027-03-01")
	invoices := testPortfolio(t)
	// Invoice A: ₹1,18,000 paid on 10 February 2027, a month after its 11
	// January deadline. The reversal belongs in the February 2027 return and
	// the re-claim can be taken in the same one.
	invoices[0].Payments = []Payment{{Date: mustDay(t, "2027-02-10"), Amount: 11_800_000}}
	lines := AssessAll(invoices, asOf, set)

	biz := Business{Name: "Nagarjuna Fabricators", GSTIN: "29AAGCB7383J1ZS"}
	feb := TaxPeriod{Year: 2027, Month: 2, Months: 1}
	paper := WorkingPaper(biz, feb, lines, asOf, set)

	for _, want := range []string{
		"RULE 37",
		"Nagarjuna Fabricators",
		"29AAGCB7383J1ZS",
		"February 2027",
		"20 March 2027",  // the return due date
		"180 days",       // the window, from the named constant
		"1 October 2022", // which version of the rule this implements
		"TABLE " + TableReversal,
		"TABLE " + TableReclaim,
		TableReclaimDisclosure,
		ReversalLedgerName,
		"SM/001", // the invoice behind the figures
		"Sundar Metals",
		"18,000.00",    // the reversal
		"not a return", // the honesty line
	} {
		if !strings.Contains(paper, want) {
			t.Errorf("the working paper does not mention %q", want)
		}
	}

	// The reversal and the re-claim fall in the same return, so it adds
	// nothing to the output tax liability.
	if !strings.Contains(paper, "Net added to output tax liability") {
		t.Error("the working paper should show the net effect")
	}

	// Invoice F's reversal belongs in the March return, not this one, so it
	// must not appear on the February paper.
	if strings.Contains(paper, "KP/101") {
		t.Error("the February paper should not carry an invoice reversing in March")
	}
	// Nor should anything settled or out of scope.
	for _, no := range []string{"KP/100", "GT/900"} {
		if strings.Contains(paper, no) {
			t.Errorf("the working paper should not carry %s", no)
		}
	}

	// The March paper carries invoice F's reversal and nothing to re-claim.
	mar := WorkingPaper(biz, feb.Next(), lines, asOf, set)
	if !strings.Contains(mar, "KP/101") {
		t.Error("the March paper should carry invoice KP/101")
	}
	if !strings.Contains(mar, "Nothing to re-claim") {
		t.Error("the March paper should say there is nothing to re-claim")
	}

	// A period with nothing in it says so rather than printing an empty table.
	quiet := WorkingPaper(biz, TaxPeriod{Year: 2030, Month: 1, Months: 1}, lines, asOf, set)
	if !strings.Contains(quiet, "Nothing to reverse") {
		t.Error("an empty period should say there is nothing to reverse")
	}
}

func TestWorkingPaperShowsInterestOnlyWhenLate(t *testing.T) {
	set := DefaultSettings()
	biz := Business{Name: "Nagarjuna Fabricators"}
	feb := TaxPeriod{Year: 2027, Month: 2, Months: 1}

	// Before the February return falls due on 20 March 2027, there is no
	// interest to show.
	early := WorkingPaper(biz, feb, AssessAll(testPortfolio(t), mustDay(t, "2027-03-01"), set), mustDay(t, "2027-03-01"), set)
	if strings.Contains(early, "INTEREST") {
		t.Error("no interest should be shown before the return falls due")
	}

	// Three months later it is late, and the figure is labelled indicative
	// with the position stated.
	asOf := mustDay(t, "2027-06-20")
	late := WorkingPaper(biz, feb, AssessAll(testPortfolio(t), asOf, set), asOf, set)
	for _, want := range []string{"INDICATIVE ONLY", "816.66", "s.50(3)", "not a", "demand"} {
		if !strings.Contains(late, want) {
			t.Errorf("the late working paper does not mention %q", want)
		}
	}
}

func TestWorkingPaperForAQuarterlyFiler(t *testing.T) {
	set := DefaultSettings()
	set.ReturnPeriodMonths = QuarterlyPeriodMonths
	set.ReturnDueDay = QuarterlyDueDayLate
	asOf := mustDay(t, "2027-05-01")
	lines := AssessAll(testPortfolio(t), asOf, set)

	q := TaxPeriod{Year: 2027, Month: 4, Months: 3}
	paper := WorkingPaper(Business{Name: "Nagarjuna Fabricators"}, q, lines, asOf, set)
	for _, want := range []string{"April–June 2027", "24 July 2027", "quarterly, under the QRMP scheme"} {
		if !strings.Contains(paper, want) {
			t.Errorf("the quarterly working paper does not mention %q", want)
		}
	}
}

func TestWorkingPaperWithNoBusinessDetails(t *testing.T) {
	// A paper can be printed before the business's own details are filled in,
	// and must not carry empty labels.
	paper := WorkingPaper(Business{}, TaxPeriod{Year: 2027, Month: 2, Months: 1}, nil, mustDay(t, "2027-03-01"), DefaultSettings())
	if strings.Contains(paper, "Registered person   :") {
		t.Error("the paper should leave out a label it has no value for")
	}
	if !strings.Contains(paper, "February 2027") {
		t.Error("the paper should still name its period")
	}
}

func TestTruncAndSupplierOf(t *testing.T) {
	if got, want := trunc("short", 10), "short"; got != want {
		t.Errorf("trunc = %q, want %q", got, want)
	}
	if got, want := trunc("a much longer string", 6), "a muc…"; got != want {
		t.Errorf("trunc = %q, want %q", got, want)
	}
	if got := trunc("abc", 0); got != "" {
		t.Errorf("trunc to nothing = %q, want an empty string", got)
	}
	// Multi-byte characters must not be cut in half.
	if got := trunc("कागज़ात और बिल", 4); len([]rune(got)) != 4 {
		t.Errorf("trunc cut %q to %d runes, want 4", got, len([]rune(got)))
	}

	cases := []struct {
		inv  Invoice
		want string
	}{
		{inv: Invoice{SupplierName: "A", SupplierGSTIN: "G"}, want: "A · G"},
		{inv: Invoice{SupplierName: "A"}, want: "A"},
		{inv: Invoice{SupplierGSTIN: "G"}, want: "G"},
		{inv: Invoice{}, want: "(supplier not recorded)"},
	}
	for _, c := range cases {
		if got := supplierOf(c.inv); got != c.want {
			t.Errorf("supplierOf(%+v) = %q, want %q", c.inv, got, c.want)
		}
	}
	if got, want := (Invoice{InvoiceNo: "X", SupplierName: "Y"}).Describe(), "X — Y"; got != want {
		t.Errorf("Describe() = %q, want %q", got, want)
	}
	if got, want := (Invoice{InvoiceNo: "X"}).Describe(), "X"; got != want {
		t.Errorf("Describe() = %q, want %q", got, want)
	}
}

// indianDateKey turns dd-mm-yyyy into a sortable yyyymmdd for the order check.
func indianDateKey(s string) string {
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return s
	}
	return parts[2] + parts[1] + parts[0]
}
