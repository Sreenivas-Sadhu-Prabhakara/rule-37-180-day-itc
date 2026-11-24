package rule37

import "testing"

// testPortfolio is the worked set the money view is checked against, assessed
// as at 1 February 2027 by a monthly filer.
//
//	A  Sundar Metals    15-07-2026  ₹1,18,000  18% IGST   nothing paid  day 180 was 11-01-2027 — REVERSE in the February 2027 return
//	B  Nandi Hardware   24-11-2026  ₹1,18,000  18% CGST+SGST  nothing paid  day 180 is 23-05-2027 — 111 days left
//	C  Sundar Metals    04-10-2026  ₹1,05,000   5% IGST   nothing paid  day 180 is 02-04-2027 —  60 days left
//	D  Ganga Transport  15-07-2026  reverse charge — out of scope
//	E  Kaveri Plastics  15-07-2026  paid in full on 01-08-2026 — settled
//	F  Kaveri Plastics  19-08-2026  ₹1,28,000  28% IGST  nothing paid  day 180 is 15-02-2027 —  14 days left
func testPortfolio(t *testing.T) []Invoice {
	t.Helper()
	return []Invoice{
		{
			ID: "A", SupplierName: "Sundar Metals", SupplierGSTIN: "27AAPFU0939F1ZV",
			InvoiceNo: "SM/001", InvoiceDate: mustDay(t, "2026-07-15"),
			TaxableValue: 10_000_000, ITC: Heads{IGST: 1_800_000},
		},
		{
			ID: "B", SupplierName: "Nandi Hardware", SupplierGSTIN: "29AABCU9603R1ZM",
			InvoiceNo: "NH/045", InvoiceDate: mustDay(t, "2026-11-24"),
			TaxableValue: 10_000_000, ITC: Heads{CGST: 900_000, SGST: 900_000},
		},
		{
			ID: "C", SupplierName: "Sundar Metals", SupplierGSTIN: "27AAPFU0939F1ZV",
			InvoiceNo: "SM/002", InvoiceDate: mustDay(t, "2026-10-04"),
			TaxableValue: 10_000_000, ITC: Heads{IGST: 500_000},
		},
		{
			ID: "D", SupplierName: "Ganga Transport", SupplierGSTIN: "29AAGCB7383J1ZS",
			InvoiceNo: "GT/900", InvoiceDate: mustDay(t, "2026-07-15"),
			TaxableValue: 5_000_000, ITC: Heads{IGST: 250_000},
			Exclusion: ExclusionReverseCharge,
		},
		{
			ID: "E", SupplierName: "Kaveri Plastics", SupplierGSTIN: "29AAGCB7383J1ZS",
			InvoiceNo: "KP/100", InvoiceDate: mustDay(t, "2026-07-15"),
			TaxableValue: 10_000_000, ITC: Heads{IGST: 1_800_000},
			Payments: []Payment{{Date: mustDay(t, "2026-08-01"), Amount: 11_800_000}},
		},
		{
			ID: "F", SupplierName: "Kaveri Plastics", SupplierGSTIN: "29AAGCB7383J1ZS",
			InvoiceNo: "KP/101", InvoiceDate: mustDay(t, "2026-08-19"),
			TaxableValue: 10_000_000, ITC: Heads{IGST: 2_800_000},
		},
	}
}

func TestTotalsHeadlineFigures(t *testing.T) {
	set := DefaultSettings()
	asOf := mustDay(t, "2027-02-01")
	s := Totals(AssessAll(testPortfolio(t), asOf, set), asOf, set)

	if got, want := s.Invoices, 6; got != want {
		t.Errorf("invoices = %d, want %d", got, want)
	}
	if got, want := s.Excluded, 1; got != want {
		t.Errorf("out of scope = %d, want %d", got, want)
	}
	if got, want := s.Unusable, 0; got != want {
		t.Errorf("unusable = %d, want %d", got, want)
	}
	if got, want := s.Counted[StageSettled], 1; got != want {
		t.Errorf("settled = %d, want %d", got, want)
	}

	// The headline: credit on everything still unpaid whose window is open.
	// B ₹18,000 + C ₹5,000 + F ₹28,000.
	if got, want := s.AtRisk.Total(), int64(5_100_000); got != want {
		t.Errorf("credit at risk = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.AtRiskInvoices, 3; got != want {
		t.Errorf("invoices at risk = %d, want %d", got, want)
	}
	if got, want := s.CashToProtectAll, int64(35_100_000); got != want {
		t.Errorf("cash to protect it all = %s, want %s", Rupees(got), Rupees(want))
	}
	// Head by head, because that is how the return is filled in.
	if got, want := s.AtRisk, (Heads{IGST: 3_300_000, CGST: 900_000, SGST: 900_000}); got != want {
		t.Errorf("credit at risk per head = %+v, want %+v", got, want)
	}

	// Only invoice F has its deadline inside the February 2027 return period,
	// so it is the one that tips over this period if it is not paid.
	if got, want := s.ThisPeriod.String(), "February 2027"; got != want {
		t.Errorf("this period = %q, want %q", got, want)
	}
	if got, want := s.NextPeriod.String(), "March 2027"; got != want {
		t.Errorf("next period = %q, want %q", got, want)
	}
	if got, want := s.RiskThisPeriod.Total(), int64(2_800_000); got != want {
		t.Errorf("credit at risk this period = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.RiskThisPeriodInvoices, 1; got != want {
		t.Errorf("invoices at risk this period = %d, want %d", got, want)
	}
	if got, want := s.CashThisPeriod, int64(12_800_000); got != want {
		t.Errorf("cash needed this period = %s, want %s", Rupees(got), Rupees(want))
	}

	// Invoice A is past its deadline, so its credit is no longer at risk: it
	// must be reversed.
	if got, want := s.ReversalDue.Total(), int64(1_800_000); got != want {
		t.Errorf("reversal due = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.ReversalDueInvoices, 1; got != want {
		t.Errorf("invoices to reverse = %d, want %d", got, want)
	}
	// Its return is not due until 20 March 2027, so nothing is overdue yet.
	if got := s.ReversalOverdue.Total(); got != 0 {
		t.Errorf("overdue reversal = %s, want nil", Rupees(got))
	}
	if got := s.ReclaimAvailable.Total(); got != 0 {
		t.Errorf("re-claim available = %s, want nil: nothing has been paid late", Rupees(got))
	}
	if got, want := s.Exposure(), int64(6_900_000); got != want {
		t.Errorf("total exposure = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.NetThisReturn(), int64(1_800_000); got != want {
		t.Errorf("net addition = %s, want %s", Rupees(got), Rupees(want))
	}
	if got := s.Interest; got != 0 {
		t.Errorf("interest = %s, want nil: no reversal is late yet", Rupees(got))
	}
}

func TestTotalsPeriodSchedule(t *testing.T) {
	set := DefaultSettings()
	asOf := mustDay(t, "2027-02-01")
	s := Totals(AssessAll(testPortfolio(t), asOf, set), asOf, set)

	want := []struct {
		period    string
		due       string
		reversal  int64
		projected int64
	}{
		{period: "February 2027", due: "2027-03-20", reversal: 1_800_000}, // invoice A
		{period: "March 2027", due: "2027-04-20", projected: 2_800_000},   // invoice F, if unpaid
		{period: "May 2027", due: "2027-06-20", projected: 500_000},       // invoice C, if unpaid
		{period: "June 2027", due: "2027-07-20", projected: 1_800_000},    // invoice B, if unpaid
	}
	if len(s.Periods) != len(want) {
		t.Fatalf("schedule has %d periods, want %d: %+v", len(s.Periods), len(want), s.Periods)
	}
	for i, w := range want {
		got := s.Periods[i]
		if got.Period.String() != w.period {
			t.Errorf("period %d = %q, want %q", i, got.Period.String(), w.period)
		}
		if got.DueDate.String() != w.due {
			t.Errorf("%s return due %s, want %s", w.period, got.DueDate, w.due)
		}
		if got.Reversal.Total() != w.reversal {
			t.Errorf("%s reversal = %s, want %s", w.period, Rupees(got.Reversal.Total()), Rupees(w.reversal))
		}
		if got.Projected.Total() != w.projected {
			t.Errorf("%s projected = %s, want %s", w.period, Rupees(got.Projected.Total()), Rupees(w.projected))
		}
		if got.Overdue {
			t.Errorf("%s should not be overdue as at 1 February 2027", w.period)
		}
	}

	// The projection carries the cash that would stop it happening.
	if got, want := s.Periods[1].CashToAvoid, int64(12_800_000); got != want {
		t.Errorf("cash to avoid the March reversal = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.Periods[0].NetAddition(), int64(1_800_000); got != want {
		t.Errorf("February net addition = %s, want %s", Rupees(got), Rupees(want))
	}

	feb, ok := s.PositionFor(TaxPeriod{Year: 2027, Month: 2, Months: 1})
	if !ok {
		t.Fatal("February 2027 should be in the schedule")
	}
	if feb.Reversal.Total() != 1_800_000 {
		t.Errorf("PositionFor February = %s", Rupees(feb.Reversal.Total()))
	}
	if _, ok := s.PositionFor(TaxPeriod{Year: 2030, Month: 1, Months: 1}); ok {
		t.Error("a period with nothing in it should not be in the schedule")
	}
}

func TestTotalsMarksAnOverdueReturn(t *testing.T) {
	set := DefaultSettings()
	// Long after the February 2027 return fell due on 20 March 2027.
	asOf := mustDay(t, "2027-06-20")
	s := Totals(AssessAll(testPortfolio(t), asOf, set), asOf, set)

	// Two returns have now fallen due with a reversal in them: invoice A's,
	// in the February 2027 return due 20 March, and invoice F's, in the March
	// 2027 return due 20 April. Invoice C's reversal belongs in the May return,
	// which falls due on this very day and so is not yet late.
	if got, want := s.ReversalOverdue.Total(), int64(4_600_000); got != want {
		t.Errorf("overdue reversal = %s, want %s", Rupees(got), Rupees(want))
	}
	// ₹816.66 on invoice A for 92 days, ₹842.30 on invoice F for 61 days.
	if got, want := s.Interest, int64(165_896); got != want {
		t.Errorf("indicative interest = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.InterestInvoices, 2; got != want {
		t.Errorf("invoices bearing interest = %d, want %d", got, want)
	}
	feb, ok := s.PositionFor(TaxPeriod{Year: 2027, Month: 2, Months: 1})
	if !ok {
		t.Fatal("February 2027 should still be in the schedule")
	}
	if !feb.Overdue {
		t.Error("the February 2027 return should be marked overdue")
	}
	// By June every deadline has passed, so nothing is protectable any more.
	if got := s.AtRisk.Total(); got != 0 {
		t.Errorf("credit at risk = %s, want nil: every window has closed", Rupees(got))
	}
	if got, want := s.ReversalDue.Total(), int64(6_900_000); got != want {
		t.Errorf("reversal due = %s, want %s", Rupees(got), Rupees(want))
	}
}

func TestTotalsSupplierRollup(t *testing.T) {
	set := DefaultSettings()
	asOf := mustDay(t, "2027-02-01")
	s := Totals(AssessAll(testPortfolio(t), asOf, set), asOf, set)

	// Suppliers are ranked by the credit involved, largest first, because that
	// is the order a payment run should be decided in.
	want := []struct {
		name     string
		invoices int
		exposure int64
	}{
		{name: "Kaveri Plastics", invoices: 1, exposure: 2_800_000},
		{name: "Sundar Metals", invoices: 2, exposure: 2_300_000},
		{name: "Nandi Hardware", invoices: 1, exposure: 1_800_000},
	}
	if len(s.Suppliers) != len(want) {
		t.Fatalf("got %d suppliers, want %d: %+v", len(s.Suppliers), len(want), s.Suppliers)
	}
	for i, w := range want {
		got := s.Suppliers[i]
		if got.Name != w.name {
			t.Errorf("supplier %d = %q, want %q", i, got.Name, w.name)
		}
		if got.Invoices != w.invoices {
			t.Errorf("%s has %d invoices, want %d", w.name, got.Invoices, w.invoices)
		}
		if got.Exposure() != w.exposure {
			t.Errorf("%s exposure = %s, want %s", w.name, Rupees(got.Exposure()), Rupees(w.exposure))
		}
	}

	// A 28 per cent purchase protects more credit per rupee than an 18 per
	// cent one, and far more than a 5 per cent one. That ratio is what decides
	// who to pay first when the cash will not stretch.
	kaveri, sundar, nandi := s.Suppliers[0], s.Suppliers[1], s.Suppliers[2]
	if got, want := kaveri.ProtectedPerRupee, int64(22); got != want {
		t.Errorf("Kaveri protects %d paise per rupee, want %d", got, want)
	}
	if nandi.ProtectedPerRupee <= sundar.ProtectedPerRupee {
		t.Errorf("Nandi (18%%, all protectable) %d should beat Sundar (5%% protectable) %d",
			nandi.ProtectedPerRupee, sundar.ProtectedPerRupee)
	}

	// The settled invoice and the out-of-scope one bring no supplier into the
	// list at all, so a supplier with nothing outstanding does not appear.
	for _, sup := range s.Suppliers {
		if sup.Name == "Ganga Transport" {
			t.Error("an out-of-scope invoice should not put its supplier on the list")
		}
	}

	if got, want := kaveri.NextDeadline.String(), "2027-02-15"; got != want {
		t.Errorf("Kaveri's next deadline = %s, want %s", got, want)
	}
	if got, want := kaveri.DaysToNext, 14; got != want {
		t.Errorf("Kaveri's next deadline is in %d days, want %d", got, want)
	}
	// Sundar's earliest OPEN deadline is invoice C's, not the one already gone.
	if got, want := sundar.NextDeadline.String(), "2027-04-02"; got != want {
		t.Errorf("Sundar's next deadline = %s, want %s", got, want)
	}
}

func TestTotalsGathersFlagsWithCounts(t *testing.T) {
	set := DefaultSettings()
	invoices := testPortfolio(t)
	// Two invoices with a paid amount and no date, one with an odd GSTIN.
	invoices[1].Payments = []Payment{{Amount: 1_000_000}}
	invoices[2].Payments = []Payment{{Amount: 1_000_000}}
	invoices[5].SupplierGSTIN = "29AAGCB7383J1Z"

	asOf := mustDay(t, "2027-02-01")
	s := Totals(AssessAll(invoices, asOf, set), asOf, set)

	if len(s.Flags) == 0 {
		t.Fatal("the flags should be reported")
	}
	var sawCount, sawGSTIN bool
	for _, f := range s.Flags {
		if len(f) > 2 && f[:2] == "2 " {
			sawCount = true
		}
		if containsAll(f, "15-character") {
			sawGSTIN = true
		}
	}
	if !sawCount {
		t.Errorf("two invoices share a flag, so it should be counted: %v", s.Flags)
	}
	if !sawGSTIN {
		t.Errorf("the odd GSTIN should be flagged: %v", s.Flags)
	}
}

func TestTotalsOnNothing(t *testing.T) {
	set := DefaultSettings()
	asOf := mustDay(t, "2027-02-01")
	s := Totals(nil, asOf, set)

	if s.Invoices != 0 || s.AtRisk.Total() != 0 || s.ReversalDue.Total() != 0 {
		t.Errorf("an empty register should total to nothing: %+v", s)
	}
	if len(s.Periods) != 0 {
		t.Errorf("an empty register has no return schedule, got %+v", s.Periods)
	}
	if len(s.Suppliers) != 0 {
		t.Errorf("an empty register has no suppliers, got %+v", s.Suppliers)
	}
	// The ageing keeps its shape even with nothing in it.
	if len(s.Buckets) != len(DefaultBucketEdges)+1 {
		t.Errorf("the ageing has %d bands, want %d", len(s.Buckets), len(DefaultBucketEdges)+1)
	}
	if s.ThisPeriod.String() != "February 2027" {
		t.Errorf("this period = %q, want February 2027", s.ThisPeriod.String())
	}
}

func TestTotalsQuarterlyFiler(t *testing.T) {
	set := DefaultSettings()
	set.ReturnPeriodMonths = QuarterlyPeriodMonths
	set.ReturnDueDay = QuarterlyDueDayEarly
	asOf := mustDay(t, "2027-02-01")
	s := Totals(AssessAll(testPortfolio(t), asOf, set), asOf, set)

	if got, want := s.ThisPeriod.String(), "January–March 2027"; got != want {
		t.Errorf("this period = %q, want %q", got, want)
	}
	// For a quarterly filer, invoices A and F both have their deadline inside
	// the January to March quarter — A's has passed, F's has not — so F is the
	// one still protectable this period.
	if got, want := s.RiskThisPeriod.Total(), int64(2_800_000); got != want {
		t.Errorf("credit at risk this quarter = %s, want %s", Rupees(got), Rupees(want))
	}
	// Both A and F land in the April to June return, due 22 July 2027 — but A's
	// deadline has gone, so its credit must be reversed, while F's has not, so
	// its credit is only projected and paying still saves it.
	apr, ok := s.PositionFor(TaxPeriod{Year: 2027, Month: 4, Months: 3})
	if !ok {
		t.Fatalf("the April to June quarter should be in the schedule: %+v", s.Periods)
	}
	if got, want := apr.Reversal.Total(), int64(1_800_000); got != want {
		t.Errorf("April to June reversal = %s, want %s (invoice A)", Rupees(got), Rupees(want))
	}
	if got, want := apr.Projected.Total(), int64(2_800_000); got != want {
		t.Errorf("April to June projected = %s, want %s (invoice F, if unpaid)", Rupees(got), Rupees(want))
	}
	if got, want := apr.DueDate.String(), "2027-07-22"; got != want {
		t.Errorf("April to June return due %s, want %s", got, want)
	}
	// Filing quarterly collapses four monthly returns into two: one carrying
	// A and F, one carrying B and C.
	if len(s.Periods) != 2 {
		t.Errorf("a quarterly filer has %d return periods in the schedule, want 2: %+v", len(s.Periods), s.Periods)
	}
}

func TestTotalsCountsAReclaimAgainstItsReturn(t *testing.T) {
	set := DefaultSettings()
	invoices := testPortfolio(t)
	// Invoice A is paid in full on 10 February 2027, a month after its
	// deadline: the reversal still happens, and the credit comes back in the
	// February 2027 return.
	invoices[0].Payments = []Payment{{Date: mustDay(t, "2027-02-10"), Amount: 11_800_000}}

	asOf := mustDay(t, "2027-03-01")
	s := Totals(AssessAll(invoices, asOf, set), asOf, set)

	// By 1 March, invoice F's deadline has gone too, so ₹46,000 must be
	// reversed in all — of which ₹18,000 comes straight back.
	if got, want := s.ReversalDue.Total(), int64(4_600_000); got != want {
		t.Errorf("reversal due = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.ReclaimAvailable.Total(), int64(1_800_000); got != want {
		t.Errorf("re-claim available = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := s.ReclaimAvailableInvoices, 1; got != want {
		t.Errorf("invoices with a re-claim = %d, want %d", got, want)
	}
	if got, want := s.NetThisReturn(), int64(2_800_000); got != want {
		t.Errorf("net addition = %s, want %s: only invoice F's reversal has nothing coming back", Rupees(got), Rupees(want))
	}
	feb, ok := s.PositionFor(TaxPeriod{Year: 2027, Month: 2, Months: 1})
	if !ok {
		t.Fatal("February 2027 should be in the schedule")
	}
	if feb.Reversal.Total() != 1_800_000 || feb.Reclaim.Total() != 1_800_000 {
		t.Errorf("February carries a reversal of %s and a re-claim of %s, want ₹18,000 each",
			Rupees(feb.Reversal.Total()), Rupees(feb.Reclaim.Total()))
	}
	if feb.NetAddition() != 0 {
		t.Errorf("February net addition = %s, want nil", Rupees(feb.NetAddition()))
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
