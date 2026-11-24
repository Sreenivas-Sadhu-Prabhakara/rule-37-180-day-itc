package rule37

import (
	"strings"
	"testing"
)

// A standard invoice used throughout: ₹1,00,000 of taxable value with ₹18,000
// of integrated tax, so ₹1,18,000 must reach the supplier and ₹18,000 of credit
// rides on it. Issued 15 July 2026, its 180 days run out on 11 January 2027,
// which falls in the January 2027 return period — so the reversal belongs in
// the FEBRUARY 2027 return, due 20 March 2027.
const (
	stdTaxable = 10_000_000 // ₹1,00,000
	stdIGST    = 1_800_000  // ₹18,000
	stdPayable = 11_800_000 // ₹1,18,000
)

func stdInvoice(t *testing.T, payments ...Payment) Invoice {
	t.Helper()
	return Invoice{
		ID:            "i1",
		SupplierName:  "Sundar Metals",
		SupplierGSTIN: "27AAPFU0939F1ZV",
		InvoiceNo:     "INV/0042/26-27",
		InvoiceDate:   mustDay(t, "2026-07-15"),
		TaxableValue:  stdTaxable,
		ITC:           Heads{IGST: stdIGST},
		Payments:      payments,
	}
}

func pay(t *testing.T, date string, amount int64) Payment {
	t.Helper()
	if date == "" {
		return Payment{Amount: amount}
	}
	return Payment{Date: mustDay(t, date), Amount: amount}
}

func TestAssessDeadlineAndReversalReturn(t *testing.T) {
	set := DefaultSettings()
	a := Assess(stdInvoice(t), mustDay(t, "2026-12-01"), set)

	if got, want := a.Deadline.String(), "2027-01-11"; got != want {
		t.Errorf("deadline = %s, want %s", got, want)
	}
	if got, want := a.WindowDays, PaymentWindowDays; got != want {
		t.Errorf("window = %d days, want %d", got, want)
	}
	if got, want := a.DaysElapsed, 139; got != want {
		t.Errorf("days elapsed = %d, want %d", got, want)
	}
	if got, want := a.DaysLeft, 41; got != want {
		t.Errorf("days left = %d, want %d", got, want)
	}
	if got, want := a.ReversalPeriod.String(), "February 2027"; got != want {
		t.Errorf("reversal period = %q, want %q", got, want)
	}
	if got, want := a.ReversalDue.String(), "2027-03-20"; got != want {
		t.Errorf("reversal due = %s, want %s", got, want)
	}
	if got, want := a.Payable, int64(stdPayable); got != want {
		t.Errorf("payable = %d, want %d (the value of the supply with the tax on it)", got, want)
	}
}

func TestAssessStages(t *testing.T) {
	set := DefaultSettings()
	cases := []struct {
		name      string
		invoice   Invoice
		asOf      string
		wantStage Stage
		// The figures that drive the money view.
		wantAtRisk   int64
		wantReversal int64
		wantReclaim  int64
		wantUnpaid   int64
	}{
		{
			name:       "nothing paid, well inside the window",
			invoice:    stdInvoice(t),
			asOf:       "2026-12-01",
			wantStage:  StageWatch,
			wantAtRisk: stdIGST,
			wantUnpaid: stdPayable,
		},
		{
			name:       "paid in full in time",
			invoice:    stdInvoice(t, pay(t, "2026-08-01", stdPayable)),
			asOf:       "2026-12-01",
			wantStage:  StageSettled,
			wantUnpaid: 0,
		},
		{
			name:       "part paid, still inside the window",
			invoice:    stdInvoice(t, pay(t, "2026-08-01", 5_000_000)),
			asOf:       "2026-12-01",
			wantStage:  StageWatch,
			wantAtRisk: 1_037_288, // ₹18,000 x ₹68,000 / ₹1,18,000
			wantUnpaid: 6_800_000,
		},
		{
			name:      "paid on the very last day",
			invoice:   stdInvoice(t, pay(t, "2027-01-11", stdPayable)),
			asOf:      "2027-02-01",
			wantStage: StageSettled,
		},
		{
			name:         "paid one day late",
			invoice:      stdInvoice(t, pay(t, "2027-01-12", stdPayable)),
			asOf:         "2027-02-01",
			wantStage:    StageReclaim,
			wantReversal: stdIGST,
			wantReclaim:  stdIGST,
		},
		{
			name:         "nothing paid, the window has closed",
			invoice:      stdInvoice(t),
			asOf:         "2027-02-01",
			wantStage:    StageReverse,
			wantReversal: stdIGST,
			wantUnpaid:   stdPayable,
		},
		{
			name:         "part paid in time, the rest still owing",
			invoice:      stdInvoice(t, pay(t, "2026-08-01", 5_000_000)),
			asOf:         "2027-02-01",
			wantStage:    StageReverse,
			wantReversal: 1_037_288,
			wantUnpaid:   6_800_000,
		},
		{
			name: "part paid in time, the rest paid late",
			invoice: stdInvoice(t,
				pay(t, "2026-08-01", 5_000_000),
				pay(t, "2027-02-10", 6_800_000)),
			asOf:         "2027-03-01",
			wantStage:    StageReclaim,
			wantReversal: 1_037_288,
			wantReclaim:  1_037_288,
		},
		{
			name: "part paid in time, part paid late, part still owing",
			invoice: stdInvoice(t,
				pay(t, "2026-08-01", 5_000_000),
				pay(t, "2027-02-10", 3_000_000)),
			asOf:         "2027-03-01",
			wantStage:    StageReverse,
			wantReversal: 1_037_288,
			wantReclaim:  457_627, // ₹18,000 x ₹30,000 / ₹1,18,000
			wantUnpaid:   3_800_000,
		},
		// An out-of-scope invoice still shows what is owed on it — the money is
		// owed, it is simply not a rule 37 problem — but nothing is at risk and
		// nothing reverses.
		{
			name:       "reverse charge is out of scope",
			invoice:    withExclusion(stdInvoice(t), ExclusionReverseCharge),
			asOf:       "2027-02-01",
			wantStage:  StageExcluded,
			wantUnpaid: stdPayable,
		},
		{
			name:       "a Schedule I supply is deemed paid",
			invoice:    withExclusion(stdInvoice(t), ExclusionScheduleI),
			asOf:       "2027-02-01",
			wantStage:  StageExcluded,
			wantUnpaid: stdPayable,
		},
		{
			name:       "no credit taken means nothing to reverse",
			invoice:    withNoCredit(stdInvoice(t)),
			asOf:       "2027-02-01",
			wantStage:  StageExcluded,
			wantUnpaid: stdTaxable,
		},
		{
			name:      "no invoice date cannot be assessed",
			invoice:   Invoice{InvoiceNo: "X", ITC: Heads{IGST: 100}},
			asOf:      "2027-02-01",
			wantStage: StageUnusable,
		},
		{
			name:      "overpaid is settled, not negative",
			invoice:   stdInvoice(t, pay(t, "2026-08-01", 15_000_000)),
			asOf:      "2027-02-01",
			wantStage: StageSettled,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := Assess(c.invoice, mustDay(t, c.asOf), set)
			if a.Stage != c.wantStage {
				t.Errorf("stage = %q (%s), want %q", a.Stage, a.Reason, c.wantStage)
			}
			if got := a.AtRisk.Total(); got != c.wantAtRisk {
				t.Errorf("credit at risk = %s, want %s", Rupees(got), Rupees(c.wantAtRisk))
			}
			if got := a.Reversal.Total(); got != c.wantReversal {
				t.Errorf("reversal = %s, want %s", Rupees(got), Rupees(c.wantReversal))
			}
			if got := a.Reclaim.Total(); got != c.wantReclaim {
				t.Errorf("re-claim = %s, want %s", Rupees(got), Rupees(c.wantReclaim))
			}
			if got := a.UnpaidToDate; got != c.wantUnpaid {
				t.Errorf("unpaid to date = %s, want %s", Rupees(got), Rupees(c.wantUnpaid))
			}
			if a.Reason == "" {
				t.Error("every assessment should say why it reached its stage")
			}
			// Credit at risk and a reversal are mutually exclusive: the first
			// is what paying still protects, the second what paying no longer
			// can.
			if !a.AtRisk.IsZero() && !a.Reversal.IsZero() {
				t.Error("an invoice cannot be both at risk and already reversing")
			}
			// A re-claim can never exceed the reversal it undoes.
			if a.Reclaim.Total() > a.Reversal.Total() {
				t.Errorf("re-claim %s exceeds the reversal %s", Rupees(a.Reclaim.Total()), Rupees(a.Reversal.Total()))
			}
		})
	}
}

// TestAssessPartialPaymentProportions walks the payment up from nothing to the
// whole invoice, checking the proportion at every step and that the two ends are
// exact.
func TestAssessPartialPaymentProportions(t *testing.T) {
	set := DefaultSettings()
	cases := []struct {
		paid         int64
		wantReversal int64
	}{
		{paid: 0, wantReversal: stdIGST},
		// A single paisa off ₹1,18,000 moves the credit by 0.15 paise, which
		// rounds back to the whole: the proportion is honest about its scale.
		{paid: 1, wantReversal: stdIGST},
		{paid: 2_950_000, wantReversal: 1_350_000}, // a quarter paid
		{paid: 5_900_000, wantReversal: 900_000},   // half paid
		{paid: 6_800_000, wantReversal: 762_712},   // ₹18,000 x ₹50,000 / ₹1,18,000
		{paid: 8_850_000, wantReversal: 450_000},   // three quarters paid
		{paid: 11_799_999, wantReversal: 0},        // one paisa short rounds to nothing
		{paid: stdPayable, wantReversal: 0},        // paid in full
	}
	for _, c := range cases {
		t.Run(Rupees(c.paid)+" paid", func(t *testing.T) {
			inv := stdInvoice(t, pay(t, "2026-08-01", c.paid))
			a := Assess(inv, mustDay(t, "2027-02-01"), set)
			if got := a.Reversal.Total(); got != c.wantReversal {
				t.Errorf("reversal on %s paid = %s, want %s", Rupees(c.paid), Rupees(got), Rupees(c.wantReversal))
			}
			// The reversal must never exceed the credit availed, and never be
			// negative, whatever has been paid.
			if a.Reversal.Total() > stdIGST || a.Reversal.Total() < 0 {
				t.Errorf("reversal %d is outside 0..%d", a.Reversal.Total(), stdIGST)
			}
		})
	}

	// One paisa short of the whole invoice reverses one paisa short of nothing,
	// which rounds to nothing; the stage is still "reverse", because there is
	// something unpaid and the deadline has gone.
	short := Assess(stdInvoice(t, pay(t, "2026-08-01", stdPayable-1)), mustDay(t, "2027-02-01"), DefaultSettings())
	if short.Stage != StageReverse {
		t.Errorf("a paisa short gives stage %q, want %q", short.Stage, StageReverse)
	}
	if short.UnpaidAtDeadline != 1 {
		t.Errorf("unpaid at the deadline = %d paise, want 1", short.UnpaidAtDeadline)
	}
}

// TestAssessRoundingOfTheStagedEntry checks that the figure put in the return is
// in whole rupees while the running total stays exact in paise.
func TestAssessRoundingOfTheStagedEntry(t *testing.T) {
	a := Assess(stdInvoice(t, pay(t, "2026-08-01", 5_000_000)), mustDay(t, "2027-02-01"), DefaultSettings())
	if got, want := a.Reversal.IGST, int64(1_037_288); got != want {
		t.Errorf("exact reversal = %d paise, want %d", got, want)
	}
	if got, want := a.ReversalRupees.IGST, int64(1_037_300); got != want {
		t.Errorf("reversal for the return = %d paise (₹%s), want %d", got, Rupees(got), want)
	}
	if a.ReversalRupees.IGST%RupeePaise != 0 {
		t.Error("the figure entered in the return must be a whole number of rupees")
	}

	// Central and State tax are rounded head by head, because the return has a
	// column for each.
	split := stdInvoice(t, pay(t, "2026-08-01", 5_000_000))
	split.ITC = Heads{CGST: 900_000, SGST: 900_000}
	b := Assess(split, mustDay(t, "2027-02-01"), DefaultSettings())
	if b.Reversal.CGST != 518_644 || b.Reversal.SGST != 518_644 {
		t.Errorf("split reversal = %+v, want 518644 per head", b.Reversal)
	}
	if b.ReversalRupees.CGST != 518_600 || b.ReversalRupees.SGST != 518_600 {
		t.Errorf("split reversal in rupees = %+v, want 518600 per head", b.ReversalRupees)
	}
}

func TestAssessUndatedPayment(t *testing.T) {
	set := DefaultSettings()

	// A register that records an amount paid but not when says only "this much
	// has reached the supplier by now". Assessed before the deadline, that is
	// in time.
	early := Assess(stdInvoice(t, pay(t, "", stdPayable)), mustDay(t, "2026-12-01"), set)
	if early.Stage != StageSettled {
		t.Errorf("an undated payment assessed inside the window gives %q, want %q", early.Stage, StageSettled)
	}

	// Assessed after the deadline, the same figure cannot show the payment was
	// in time, so the reversal stands and the re-claim follows.
	late := Assess(stdInvoice(t, pay(t, "", stdPayable)), mustDay(t, "2027-03-01"), set)
	if late.Stage != StageReclaim {
		t.Errorf("an undated payment assessed after the deadline gives %q, want %q", late.Stage, StageReclaim)
	}
	if late.Reversal.Total() != stdIGST || late.Reclaim.Total() != stdIGST {
		t.Errorf("reversal %s and re-claim %s, want %s each",
			Rupees(late.Reversal.Total()), Rupees(late.Reclaim.Total()), Rupees(stdIGST))
	}
	if late.NetAddition() != 0 {
		t.Errorf("net addition = %s, want nil: the reversal and the re-claim cancel", Rupees(late.NetAddition()))
	}
	if !hasFlagAbout(late.Flags, "not the date") {
		t.Errorf("an undated payment should be flagged; flags were %v", late.Flags)
	}
}

func TestAssessReclaimPeriods(t *testing.T) {
	set := DefaultSettings()
	// Two payments after the deadline, in different return periods. The
	// re-claim is split between the returns the payments fall in, and the parts
	// add up to the whole.
	inv := stdInvoice(t,
		pay(t, "2027-02-10", 4_000_000),
		pay(t, "2027-04-05", 7_800_000),
	)
	a := Assess(inv, mustDay(t, "2027-05-01"), set)

	if a.Stage != StageReclaim {
		t.Fatalf("stage = %q, want %q (%s)", a.Stage, StageReclaim, a.Reason)
	}
	if len(a.ReclaimPeriods) != 2 {
		t.Fatalf("re-claim split over %d periods, want 2: %+v", len(a.ReclaimPeriods), a.ReclaimPeriods)
	}
	if got, want := a.ReclaimPeriods[0].Period.String(), "February 2027"; got != want {
		t.Errorf("first re-claim period = %q, want %q", got, want)
	}
	if got, want := a.ReclaimPeriods[1].Period.String(), "April 2027"; got != want {
		t.Errorf("second re-claim period = %q, want %q", got, want)
	}
	if got, want := a.ReclaimPeriods[0].DueDate.String(), "2027-03-20"; got != want {
		t.Errorf("first re-claim return due %s, want %s", got, want)
	}
	// ₹18,000 x ₹40,000 / ₹1,18,000 = ₹6,101.69
	if got, want := a.ReclaimPeriods[0].Heads.Total(), int64(610_169); got != want {
		t.Errorf("February re-claim = %s, want %s", Rupees(got), Rupees(want))
	}
	var sum int64
	for _, rp := range a.ReclaimPeriods {
		sum += rp.Heads.Total()
	}
	if sum != a.Reclaim.Total() {
		t.Errorf("the periods come to %s but the re-claim is %s — not a paisa may be created or lost",
			Rupees(sum), Rupees(a.Reclaim.Total()))
	}
	if a.Reclaim.Total() != stdIGST {
		t.Errorf("a wholly paid invoice should restore the whole credit: got %s, want %s",
			Rupees(a.Reclaim.Total()), Rupees(stdIGST))
	}
}

func TestAssessReclaimIgnoresPaymentsAfterTheAssessmentDate(t *testing.T) {
	// A payment dated after the date the figures are drawn up has not happened
	// yet as far as this return is concerned.
	inv := stdInvoice(t, pay(t, "2027-06-01", stdPayable))
	a := Assess(inv, mustDay(t, "2027-03-01"), DefaultSettings())
	if a.Stage != StageReverse {
		t.Errorf("stage = %q, want %q", a.Stage, StageReverse)
	}
	if !a.Reclaim.IsZero() {
		t.Errorf("re-claim = %s, want nil: the payment is in the future", Rupees(a.Reclaim.Total()))
	}
	if a.PaidToDate != 0 {
		t.Errorf("paid to date = %s, want nil", Rupees(a.PaidToDate))
	}
}

func TestAssessInterestOnlyWhenTheReversalIsLate(t *testing.T) {
	set := DefaultSettings()

	// The reversal belongs in the February 2027 return, due 20 March 2027.
	// Before that date nothing is late and no interest is shown.
	onTime := Assess(stdInvoice(t), mustDay(t, "2027-03-20"), set)
	if onTime.LateDays != 0 || onTime.Interest != 0 {
		t.Errorf("on the due date: %d late days and %s of interest, want none", onTime.LateDays, Rupees(onTime.Interest))
	}

	// Three months after the return fell due, interest runs from the due date
	// at the section 50(3) rate.
	late := Assess(stdInvoice(t), mustDay(t, "2027-06-20"), set)
	if got, want := late.LateDays, 92; got != want {
		t.Errorf("late by %d days, want %d", got, want)
	}
	if got, want := late.Interest, int64(81_666); got != want {
		t.Errorf("interest = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := late.InterestFromDay.String(), "2027-03-20"; got != want {
		t.Errorf("interest runs from %s, want %s", got, want)
	}

	// On the wider reading, it runs from the due date of the return in which
	// the credit was taken — July 2026, due 20 August 2026.
	wider := set
	wider.InterestBasis = InterestFromAvailment
	w := Assess(stdInvoice(t), mustDay(t, "2027-06-20"), wider)
	if got, want := w.InterestFromDay.String(), "2026-08-20"; got != want {
		t.Errorf("on the wider basis interest runs from %s, want %s", got, want)
	}
	if got, want := w.LateDays, 304; got != want {
		t.Errorf("wider basis late by %d days, want %d", got, want)
	}
	if got, want := w.Interest, int64(269_852); got != want {
		t.Errorf("wider basis interest = %s, want %s", Rupees(got), Rupees(want))
	}
	if w.Interest <= late.Interest {
		t.Error("the wider basis should give the larger exposure")
	}

	// Setting the rate to nil is how a user takes the view that no interest
	// arises at all.
	none := set
	none.InterestRatePct = 0
	n := Assess(stdInvoice(t), mustDay(t, "2027-06-20"), none)
	if n.Interest != 0 {
		t.Errorf("at a nil rate interest = %s, want none", Rupees(n.Interest))
	}
	if n.LateDays != 92 {
		t.Error("a nil rate should not hide that the reversal is late")
	}
}

func TestInterestOn(t *testing.T) {
	cases := []struct {
		name      string
		principal int64
		rate      float64
		days      int
		want      int64
	}{
		{name: "a full year at eighteen per cent", principal: 1_000_000, rate: 18, days: DaysInYearForInterest, want: 180_000},
		{name: "thirty days", principal: 1_000_000, rate: 18, days: 30, want: 14_795},
		{name: "ninety-two days on the standard credit", principal: 1_800_000, rate: 18, days: 92, want: 81_666},
		{name: "a fractional rate rounds half away from zero", principal: 100, rate: 18.5, days: DaysInYearForInterest, want: 19},
		{name: "no days, no interest", principal: 1_000_000, rate: 18, days: 0, want: 0},
		{name: "negative days, no interest", principal: 1_000_000, rate: 18, days: -5, want: 0},
		{name: "no principal, no interest", principal: 0, rate: 18, days: 90, want: 0},
		{name: "a nil rate gives nothing", principal: 1_000_000, rate: 0, days: 90, want: 0},
		{name: "a negative rate gives nothing", principal: 1_000_000, rate: -18, days: 90, want: 0},
		{name: "a leap year is still 365 days for this purpose", principal: 1_000_000, rate: 18, days: 366, want: 180_493},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := InterestOn(c.principal, c.rate, c.days); got != c.want {
				t.Errorf("InterestOn(%d, %.2f, %d) = %d, want %d", c.principal, c.rate, c.days, got, c.want)
			}
		})
	}
}

func TestAssessFlags(t *testing.T) {
	set := DefaultSettings()
	cases := []struct {
		name  string
		setup func(Invoice) Invoice
		asOf  string
		about string
	}{
		{
			name:  "paid more than the invoice",
			setup: func(inv Invoice) Invoice { inv.Payments = []Payment{{Amount: 15_000_000}}; return inv },
			asOf:  "2026-12-01",
			about: "More has been recorded as paid",
		},
		{
			name:  "invoice total below the value plus tax",
			setup: func(inv Invoice) Invoice { inv.InvoiceValue = 10_000_000; return inv },
			asOf:  "2026-12-01",
			about: "less than the taxable value",
		},
		{
			name:  "a GSTIN that is the wrong shape",
			setup: func(inv Invoice) Invoice { inv.SupplierGSTIN = "27AAPFU0939F1Z"; return inv },
			asOf:  "2026-12-01",
			about: "not in the standard 15-character form",
		},
		{
			name: "a payment dated before the invoice",
			setup: func(inv Invoice) Invoice {
				inv.Payments = []Payment{{Date: mustDay(t, "2026-07-01"), Amount: 100}}
				return inv
			},
			asOf:  "2026-12-01",
			about: "before the invoice itself",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := Assess(c.setup(stdInvoice(t)), mustDay(t, c.asOf), set)
			if !hasFlagAbout(a.Flags, c.about) {
				t.Errorf("expected a flag about %q; flags were %v", c.about, a.Flags)
			}
		})
	}

	// An invoice dated after the assessment date has not started its clock.
	future := stdInvoice(t)
	future.InvoiceDate = mustDay(t, "2027-06-01")
	a := Assess(future, mustDay(t, "2026-11-24"), set)
	if !hasFlagAbout(a.Flags, "dated in the future") {
		t.Errorf("expected a flag about a future invoice; flags were %v", a.Flags)
	}
	if a.DaysElapsed >= 0 {
		t.Errorf("days elapsed = %d, want a negative count", a.DaysElapsed)
	}
	if a.Bucket.Index != 0 {
		t.Errorf("a future invoice falls in band %d, want the first", a.Bucket.Index)
	}
	if a.Stage != StageWatch {
		t.Errorf("stage = %q, want %q", a.Stage, StageWatch)
	}
}

func TestAssessCreditWithNoValueToPay(t *testing.T) {
	// Credit with no value behind it makes the proportion meaningless, because
	// it would be apportioned against the tax alone. Such a row is refused at
	// the door, and flagged loudly if it reaches the assessment from an older
	// data file.
	inv := Invoice{
		InvoiceNo:    "ODD-1",
		SupplierName: "Nowhere Traders",
		InvoiceDate:  mustDay(t, "2026-07-15"),
		ITC:          Heads{IGST: 1_800_000},
	}
	if err := inv.Validate(); err == nil {
		t.Error("an invoice with credit but no value should not validate")
	}
	a := Assess(inv, mustDay(t, "2027-02-01"), DefaultSettings())
	if !hasFlagAbout(a.Flags, "no taxable value") {
		t.Errorf("expected a flag about the missing value; flags were %v", a.Flags)
	}

	// An invoice with neither credit nor value is simply out of scope, and must
	// not divide by zero on the way there.
	empty := Invoice{InvoiceNo: "ODD-2", SupplierName: "Nowhere Traders", InvoiceDate: mustDay(t, "2026-07-15")}
	b := Assess(empty, mustDay(t, "2027-02-01"), DefaultSettings())
	if b.Stage != StageExcluded {
		t.Errorf("stage = %q, want %q", b.Stage, StageExcluded)
	}
	if !b.Reversal.IsZero() || !b.AtRisk.IsZero() {
		t.Error("nothing should be at stake on an invoice with no credit and no value")
	}
}

func TestAssessWithQuarterlyFiling(t *testing.T) {
	// The same invoice, for a filer under the Quarterly Return Monthly Payment
	// scheme: day 180 falls in the January to March quarter, so the reversal
	// goes in the April to June return, due on the 22nd of July.
	set := DefaultSettings()
	set.ReturnPeriodMonths = QuarterlyPeriodMonths
	set.ReturnDueDay = QuarterlyDueDayEarly
	a := Assess(stdInvoice(t), mustDay(t, "2027-02-01"), set)

	if got, want := a.Deadline.String(), "2027-01-11"; got != want {
		t.Errorf("deadline = %s, want %s — the window does not depend on how often you file", got, want)
	}
	if got, want := a.ReversalPeriod.String(), "April–June 2027"; got != want {
		t.Errorf("reversal period = %q, want %q", got, want)
	}
	if got, want := a.ReversalDue.String(), "2027-07-22"; got != want {
		t.Errorf("reversal due = %s, want %s", got, want)
	}
}

func TestAssessIsNeverSurprisedByABlankSettings(t *testing.T) {
	// A settings object from an older data file, or a sparse API request, must
	// compute the statutory position rather than dividing by zero or landing on
	// the zero date.
	a := Assess(stdInvoice(t), mustDay(t, "2027-02-01"), Settings{})
	if got, want := a.Deadline.String(), "2027-01-11"; got != want {
		t.Errorf("deadline from empty settings = %s, want %s", got, want)
	}
	if got, want := a.ReversalDue.String(), "2027-03-20"; got != want {
		t.Errorf("reversal due from empty settings = %s, want %s", got, want)
	}
	if a.Bucket.Label == "" {
		t.Error("empty settings should still give an ageing band")
	}
}

func TestAssessWithNoAsOfDateUsesTheInvoiceDate(t *testing.T) {
	a := Assess(stdInvoice(t), Day{}, DefaultSettings())
	if a.Stage != StageWatch {
		t.Errorf("stage = %q, want %q", a.Stage, StageWatch)
	}
	if a.DaysElapsed != 0 {
		t.Errorf("days elapsed = %d, want 0", a.DaysElapsed)
	}
	if a.DaysLeft != PaymentWindowDays {
		t.Errorf("days left = %d, want %d", a.DaysLeft, PaymentWindowDays)
	}
}

func TestStageHelpers(t *testing.T) {
	for _, s := range []Stage{StageWatch, StageReverse, StageReclaim} {
		if !(Assessment{Stage: s}).NeedsAction() {
			t.Errorf("%q should need action", s)
		}
	}
	for _, s := range []Stage{StageSettled, StageExcluded, StageUnusable} {
		if (Assessment{Stage: s}).NeedsAction() {
			t.Errorf("%q should not need action", s)
		}
	}
	for _, s := range []Stage{StageWatch, StageReverse, StageReclaim, StageSettled, StageExcluded, StageUnusable} {
		if s.Label() == "" {
			t.Errorf("%q has no label", s)
		}
	}
}

func TestExclusionLabelsAndParsing(t *testing.T) {
	cases := []struct {
		in   string
		want Exclusion
		bad  bool
	}{
		{in: "", want: ExclusionNone},
		{in: "  ", want: ExclusionNone},
		{in: "none", want: ExclusionNone},
		{in: "-", want: ExclusionNone},
		{in: "reverse-charge", want: ExclusionReverseCharge},
		{in: "RCM", want: ExclusionReverseCharge},
		{in: "Reverse Charge", want: ExclusionReverseCharge},
		{in: "schedule-i", want: ExclusionScheduleI},
		{in: "Schedule I", want: ExclusionScheduleI},
		{in: "section-15-2-b", want: ExclusionSection15},
		{in: "s.15(2)(b)", want: ExclusionSection15},
		{in: "no-credit", want: ExclusionNoCredit},
		{in: "ITC not availed", want: ExclusionNoCredit},
		{in: "because I felt like it", bad: true},
	}
	for _, c := range cases {
		got, err := ParseExclusion(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("ParseExclusion(%q) = %q, want an error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseExclusion(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseExclusion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, e := range []Exclusion{ExclusionReverseCharge, ExclusionScheduleI, ExclusionSection15, ExclusionNoCredit} {
		if !e.Valid() {
			t.Errorf("%q should be valid", e)
		}
		if e.Label() == "" {
			t.Errorf("%q should cite what puts it out of scope", e)
		}
	}
	if Exclusion("made up").Valid() {
		t.Error("an unknown exclusion should not be valid")
	}
	if ExclusionNone.Label() != "" {
		t.Error("an invoice in scope has no exclusion label")
	}
}

func TestInvoiceValidate(t *testing.T) {
	ok := stdInvoice(t)
	if err := ok.Validate(); err != nil {
		t.Fatalf("the standard invoice should be valid: %v", err)
	}

	cases := []struct {
		name   string
		mangle func(Invoice) Invoice
	}{
		{name: "no invoice number", mangle: func(i Invoice) Invoice { i.InvoiceNo = " "; return i }},
		{name: "no invoice date", mangle: func(i Invoice) Invoice { i.InvoiceDate = Day{}; return i }},
		{name: "negative taxable value", mangle: func(i Invoice) Invoice { i.TaxableValue = -1; return i }},
		{name: "negative credit", mangle: func(i Invoice) Invoice { i.ITC.IGST = -1; return i }},
		{name: "negative invoice value", mangle: func(i Invoice) Invoice { i.InvoiceValue = -1; return i }},
		{name: "unknown exclusion", mangle: func(i Invoice) Invoice { i.Exclusion = "whatever"; return i }},
		{name: "negative payment", mangle: func(i Invoice) Invoice { i.Payments = []Payment{{Amount: -1}}; return i }},
		{name: "credit with nothing to pay", mangle: func(i Invoice) Invoice { i.TaxableValue = 0; i.InvoiceValue = 0; return i }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.mangle(stdInvoice(t)).Validate(); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestInvoicePayableAndPayments(t *testing.T) {
	inv := stdInvoice(t, pay(t, "2026-08-01", 1_000_000), pay(t, "2026-09-01", 2_000_000))
	if got, want := inv.Payable(), int64(stdPayable); got != want {
		t.Errorf("Payable() = %d, want %d (derived from the value and the tax)", got, want)
	}
	inv.InvoiceValue = 12_000_000
	if got, want := inv.Payable(), int64(12_000_000); got != want {
		t.Errorf("Payable() = %d, want the stated invoice total %d", got, want)
	}
	if got, want := inv.PaidTotal(), int64(3_000_000); got != want {
		t.Errorf("PaidTotal() = %d, want %d", got, want)
	}
	if got, want := inv.PaidBy(mustDay(t, "2026-08-15"), Day{}), int64(1_000_000); got != want {
		t.Errorf("PaidBy mid-August = %d, want %d", got, want)
	}
	if got, want := inv.PaidBy(mustDay(t, "2026-07-01"), Day{}), int64(0); got != want {
		t.Errorf("PaidBy before any payment = %d, want %d", got, want)
	}
	if inv.HasUndatedPayment() {
		t.Error("all these payments are dated")
	}
	inv.Payments = append(inv.Payments, Payment{Amount: 500})
	if !inv.HasUndatedPayment() {
		t.Error("an undated payment should be reported")
	}

	// Sorting puts the payments in date order with undated ones last, the way
	// a ledger reads.
	inv.Payments = []Payment{
		{Amount: 1}, // undated
		pay(t, "2026-09-01", 2),
		pay(t, "2026-08-01", 3),
	}
	inv.SortPayments()
	if inv.Payments[0].Amount != 3 || inv.Payments[1].Amount != 2 || inv.Payments[2].Amount != 1 {
		t.Errorf("payments sorted to %+v", inv.Payments)
	}
}

func TestNormaliseInvoiceNo(t *testing.T) {
	cases := []struct{ in, want string }{
		{"INV-001/26", "INV126"},
		{"inv 001 / 26", "INV126"},
		{"INV1/26", "INV126"},
		{"INV-10/026", "INV1026"},
		{"INV-100/26", "INV10026"},
		{"000", "0"},
		{"", ""},
		{"a.b\\c", "ABC"},
	}
	for _, c := range cases {
		if got := NormaliseInvoiceNo(c.in); got != c.want {
			t.Errorf("NormaliseInvoiceNo(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Two genuinely different numbers must not collapse into one.
	if NormaliseInvoiceNo("INV-10/026") == NormaliseInvoiceNo("INV-100/26") {
		t.Error("two different invoice numbers collapsed into the same key")
	}
}

func TestValidGSTINFormat(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"27AAPFU0939F1ZV", true},
		{"29AAGCB7383J1ZS", true},
		{"29 AAGCB 7383 J1ZS", true}, // spaces are removed
		{"29aagcb7383j1zs", true},    // case is raised
		{"27AAPFU0939F1Z", false},    // too short
		{"27AAPFU0939F1ZVX", false},  // too long
		{"2AAAPFU0939F1ZV", false},   // State code must be two digits
		{"27AAPFU0939F1YV", false},   // the fourteenth character must be Z
		{"27AAPF10939F1ZV", false},   // the PAN's first five must be letters
		{"", false},
	}
	for _, c := range cases {
		if got := ValidGSTINFormat(c.in); got != c.want {
			t.Errorf("ValidGSTINFormat(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	if got, want := NormaliseGSTIN(" 27 aapfu 0939 f1zv "), "27AAPFU0939F1ZV"; got != want {
		t.Errorf("NormaliseGSTIN = %q, want %q", got, want)
	}
}

func withExclusion(inv Invoice, e Exclusion) Invoice {
	inv.Exclusion = e
	return inv
}

func withNoCredit(inv Invoice) Invoice {
	inv.ITC = Heads{}
	return inv
}

func hasFlagAbout(flags []string, about string) bool {
	for _, f := range flags {
		if strings.Contains(f, about) {
			return true
		}
	}
	return false
}

// TestAssessReclaimNeverPrecedesItsReversal covers the quarterly filer's trap:
// a payment made soon after day 180 can fall in the same quarter the 180 days
// expired in, while the reversal belongs in the quarter after. A re-claim
// cannot be taken before the reversal it undoes has been reported, so it is
// carried forward to the reversal's own return.
func TestAssessReclaimNeverPrecedesItsReversal(t *testing.T) {
	set := DefaultSettings()
	set.ReturnPeriodMonths = QuarterlyPeriodMonths
	set.ReturnDueDay = QuarterlyDueDayEarly

	// Day 180 is 11 January 2027, in the January to March quarter, so the
	// reversal belongs in the April to June return. The payment on 10 February
	// 2027 falls in the January to March quarter.
	inv := stdInvoice(t, pay(t, "2027-02-10", stdPayable))
	a := Assess(inv, mustDay(t, "2027-03-01"), set)

	if got, want := a.ReversalPeriod.String(), "April–June 2027"; got != want {
		t.Fatalf("reversal period = %q, want %q", got, want)
	}
	if len(a.ReclaimPeriods) != 1 {
		t.Fatalf("re-claim split over %d periods, want 1: %+v", len(a.ReclaimPeriods), a.ReclaimPeriods)
	}
	if got, want := a.ReclaimPeriods[0].Period.String(), "April–June 2027"; got != want {
		t.Errorf("re-claim period = %q, want %q — it cannot precede the reversal", got, want)
	}
	if a.ReclaimPeriods[0].Period.Compare(a.ReversalPeriod) < 0 {
		t.Error("the re-claim was scheduled before its own reversal")
	}
	if got := a.ReclaimPeriods[0].Heads.Total(); got != a.Reclaim.Total() {
		t.Errorf("the carried-forward re-claim is %s, want the whole %s", Rupees(got), Rupees(a.Reclaim.Total()))
	}

	// A monthly filer in the same position re-claims in the month of payment,
	// which is after the reversal's return in any event.
	monthly := Assess(stdInvoice(t, pay(t, "2027-02-10", stdPayable)), mustDay(t, "2027-03-01"), DefaultSettings())
	if got, want := monthly.ReclaimPeriods[0].Period.String(), "February 2027"; got != want {
		t.Errorf("monthly re-claim period = %q, want %q", got, want)
	}
}
