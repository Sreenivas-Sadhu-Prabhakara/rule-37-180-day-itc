package rule37

import "testing"

func planSettings(horizon int) Settings {
	s := DefaultSettings()
	s.PlanHorizonDays = horizon
	return s
}

// TestPayFirstOrdersByCreditPerRupee is the heart of the payment plan: with
// limited cash, pay the invoice that protects the most credit per rupee. Since
// a part payment protects its own proportion of the credit, ranking by that
// ratio is the exact answer and not a guess.
func TestPayFirstOrdersByCreditPerRupee(t *testing.T) {
	set := planSettings(0) // no horizon: every open invoice is considered
	asOf := mustDay(t, "2027-02-01")
	lines := AssessAll(testPortfolio(t), asOf, set)
	plan := PayFirst(lines, asOf, set, NoBudgetLimit)

	wantOrder := []struct {
		invoice string
		rate    int64 // paise of credit per rupee paid
	}{
		{invoice: "KP/101", rate: 22}, // 28 per cent goods
		{invoice: "NH/045", rate: 15}, // 18 per cent goods
		{invoice: "SM/002", rate: 5},  // 5 per cent goods
	}
	if len(plan.Actions) != len(wantOrder) {
		t.Fatalf("plan has %d actions, want %d: %+v", len(plan.Actions), len(wantOrder), plan.Actions)
	}
	for i, w := range wantOrder {
		got := plan.Actions[i]
		if got.InvoiceNo != w.invoice {
			t.Errorf("position %d = %s, want %s", i+1, got.InvoiceNo, w.invoice)
		}
		if got.ProtectedPerRupee != w.rate {
			t.Errorf("%s protects %d paise per rupee, want %d", w.invoice, got.ProtectedPerRupee, w.rate)
		}
	}

	// An unlimited budget pays everything in full.
	if plan.Limited {
		t.Error("a negative budget means no limit")
	}
	if got, want := plan.CashNeeded, int64(35_100_000); got != want {
		t.Errorf("cash needed = %s, want %s", Rupees(got), Rupees(want))
	}
	if plan.CashUsed != plan.CashNeeded {
		t.Errorf("cash used %s should equal cash needed %s with no budget limit", Rupees(plan.CashUsed), Rupees(plan.CashNeeded))
	}
	if got, want := plan.Protected.Total(), int64(5_100_000); got != want {
		t.Errorf("credit protected = %s, want %s", Rupees(got), Rupees(want))
	}
	if got := plan.StillAtRisk.Total(); got != 0 {
		t.Errorf("credit still at risk = %s, want nil", Rupees(got))
	}
	if plan.Shortfall() != 0 {
		t.Errorf("shortfall = %s, want nil", Rupees(plan.Shortfall()))
	}
	for _, act := range plan.Actions {
		if act.Partial {
			t.Errorf("%s should be paid in full with no budget limit", act.InvoiceNo)
		}
		if act.Protected != act.AtStake {
			t.Errorf("%s protects %s of %s at stake", act.InvoiceNo, Rupees(act.Protected.Total()), Rupees(act.AtStake.Total()))
		}
	}
}

func TestPayFirstWithABudget(t *testing.T) {
	set := planSettings(0)
	asOf := mustDay(t, "2027-02-01")
	lines := AssessAll(testPortfolio(t), asOf, set)

	// Enough for the first invoice and half of the second.
	plan := PayFirst(lines, asOf, set, 18_700_000)
	if !plan.Limited {
		t.Error("a budget of nil or more is a limit")
	}
	if got, want := plan.CashUsed, int64(18_700_000); got != want {
		t.Errorf("cash used = %s, want the whole budget %s", Rupees(got), Rupees(want))
	}
	if got, want := plan.PaidInvoices, 2; got != want {
		t.Errorf("%d invoices paid, want %d", got, want)
	}

	first, second, third := plan.Actions[0], plan.Actions[1], plan.Actions[2]
	if first.Pay != 12_800_000 || first.Partial {
		t.Errorf("the first invoice should be paid in full: pay %s, partial %v", Rupees(first.Pay), first.Partial)
	}
	if got, want := second.Pay, int64(5_900_000); got != want {
		t.Errorf("the second invoice should take what is left: pay %s, want %s", Rupees(got), Rupees(want))
	}
	if !second.Partial {
		t.Error("the second invoice is only part paid and should say so")
	}
	// Half of ₹1,18,000 protects half of ₹18,000.
	if got, want := second.Protected.Total(), int64(900_000); got != want {
		t.Errorf("the part payment protects %s, want %s", Rupees(got), Rupees(want))
	}
	if third.Pay != 0 || third.Protected.Total() != 0 {
		t.Errorf("the third invoice gets nothing: pay %s, protected %s", Rupees(third.Pay), Rupees(third.Protected.Total()))
	}

	if got, want := plan.Protected.Total(), int64(3_700_000); got != want {
		t.Errorf("credit protected = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := plan.StillAtRisk.Total(), int64(1_400_000); got != want {
		t.Errorf("credit still at risk = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := plan.Shortfall(), int64(16_400_000); got != want {
		t.Errorf("shortfall = %s, want %s", Rupees(got), Rupees(want))
	}
	// Nothing is protected twice, and nothing goes missing.
	if plan.Protected.Total()+plan.StillAtRisk.Total() != plan.AtStake.Total() {
		t.Errorf("protected %s plus still at risk %s should come to the %s at stake",
			Rupees(plan.Protected.Total()), Rupees(plan.StillAtRisk.Total()), Rupees(plan.AtStake.Total()))
	}
}

func TestPayFirstWithNoMoney(t *testing.T) {
	set := planSettings(0)
	asOf := mustDay(t, "2027-02-01")
	lines := AssessAll(testPortfolio(t), asOf, set)
	plan := PayFirst(lines, asOf, set, 0)

	// With nothing to spend the plan still shows what is at stake and in what
	// order to pay when there is money, which is the point of asking.
	if len(plan.Actions) != 3 {
		t.Fatalf("plan has %d actions, want 3", len(plan.Actions))
	}
	if plan.CashUsed != 0 || plan.PaidInvoices != 0 {
		t.Errorf("cash used %s over %d invoices, want nil", Rupees(plan.CashUsed), plan.PaidInvoices)
	}
	if plan.Protected.Total() != 0 {
		t.Errorf("credit protected = %s, want nil", Rupees(plan.Protected.Total()))
	}
	if got, want := plan.StillAtRisk.Total(), int64(5_100_000); got != want {
		t.Errorf("credit still at risk = %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := plan.Shortfall(), int64(35_100_000); got != want {
		t.Errorf("shortfall = %s, want %s", Rupees(got), Rupees(want))
	}
}

func TestPayFirstHonoursTheHorizon(t *testing.T) {
	set := planSettings(60) // two months out: to 2 April 2027
	asOf := mustDay(t, "2027-02-01")
	lines := AssessAll(testPortfolio(t), asOf, set)
	plan := PayFirst(lines, asOf, set, NoBudgetLimit)

	if got, want := plan.Horizon.String(), "2027-04-02"; got != want {
		t.Errorf("horizon = %s, want %s", got, want)
	}
	// Invoice C falls due on the horizon itself and is included; invoice B,
	// due 23 May, is left for next month's cash.
	if len(plan.Actions) != 2 {
		t.Fatalf("plan has %d actions, want 2: %+v", len(plan.Actions), plan.Actions)
	}
	if plan.Actions[0].InvoiceNo != "KP/101" || plan.Actions[1].InvoiceNo != "SM/002" {
		t.Errorf("plan covers %s and %s, want KP/101 and SM/002", plan.Actions[0].InvoiceNo, plan.Actions[1].InvoiceNo)
	}
	if got, want := plan.AtStake.Total(), int64(3_300_000); got != want {
		t.Errorf("credit at stake inside the horizon = %s, want %s", Rupees(got), Rupees(want))
	}

	// A budget that runs out part way through the second invoice.
	tight := PayFirst(lines, asOf, set, 15_000_000)
	if got, want := tight.Actions[1].Pay, int64(2_200_000); got != want {
		t.Errorf("the tail payment = %s, want %s", Rupees(got), Rupees(want))
	}
	// ₹5,000 x ₹22,000 / ₹1,05,000 = ₹1,047.62
	if got, want := tight.Actions[1].Protected.Total(), int64(104_762); got != want {
		t.Errorf("the tail payment protects %s, want %s", Rupees(got), Rupees(want))
	}
	if got, want := tight.Protected.Total(), int64(2_904_762); got != want {
		t.Errorf("credit protected = %s, want %s", Rupees(got), Rupees(want))
	}

	// A one-day horizon reaches nothing, and says so rather than failing.
	none := PayFirst(lines, asOf, planSettings(1), NoBudgetLimit)
	if len(none.Actions) != 0 {
		t.Errorf("a one-day horizon planned %d payments: %+v", len(none.Actions), none.Actions)
	}
	if none.CashNeeded != 0 || none.AtStake.Total() != 0 {
		t.Error("an empty plan should total to nothing")
	}
}

func TestPayFirstIgnoresWhatPayingCannotSave(t *testing.T) {
	set := planSettings(0)
	asOf := mustDay(t, "2027-02-01")
	lines := AssessAll(testPortfolio(t), asOf, set)
	plan := PayFirst(lines, asOf, set, NoBudgetLimit)

	for _, act := range plan.Actions {
		// Invoice A is past its deadline: paying it now buys a re-claim in a
		// later return, not protection, so it has no place in this plan.
		if act.InvoiceNo == "SM/001" {
			t.Error("an invoice past its deadline should not be in the payment plan")
		}
		// The settled and out-of-scope invoices have nothing to protect.
		if act.InvoiceNo == "KP/100" || act.InvoiceNo == "GT/900" {
			t.Errorf("%s has nothing at stake and should not be in the plan", act.InvoiceNo)
		}
		if act.Unpaid <= 0 || act.AtStake.Total() <= 0 {
			t.Errorf("%s is in the plan with %s unpaid and %s at stake", act.InvoiceNo, Rupees(act.Unpaid), Rupees(act.AtStake.Total()))
		}
		if act.DaysLeft < 0 {
			t.Errorf("%s is in the plan with its deadline %d days past", act.InvoiceNo, -act.DaysLeft)
		}
	}
}

func TestPayFirstBreaksTiesByDeadline(t *testing.T) {
	// Two invoices taxed at the same rate protect the same credit per rupee, so
	// the one about to be lost is paid first.
	set := planSettings(0)
	asOf := mustDay(t, "2027-02-01")
	invoices := []Invoice{
		{
			ID: "later", SupplierName: "Later Supplier", InvoiceNo: "L/1",
			InvoiceDate:  mustDay(t, "2026-11-24"), // deadline 23 May 2027
			TaxableValue: 10_000_000, ITC: Heads{IGST: 1_800_000},
		},
		{
			ID: "sooner", SupplierName: "Sooner Supplier", InvoiceNo: "S/1",
			InvoiceDate:  mustDay(t, "2026-10-04"), // deadline 2 April 2027
			TaxableValue: 10_000_000, ITC: Heads{IGST: 1_800_000},
		},
	}
	plan := PayFirst(AssessAll(invoices, asOf, set), asOf, set, NoBudgetLimit)
	if len(plan.Actions) != 2 {
		t.Fatalf("plan has %d actions, want 2", len(plan.Actions))
	}
	if plan.Actions[0].ProtectedPerRupee != plan.Actions[1].ProtectedPerRupee {
		t.Fatal("this test needs two invoices at the same rate")
	}
	if plan.Actions[0].InvoiceNo != "S/1" {
		t.Errorf("the tie was broken towards %s, want the earlier deadline S/1", plan.Actions[0].InvoiceNo)
	}
}

func TestPayFirstIsStableAndSafeOnNothing(t *testing.T) {
	set := planSettings(0)
	asOf := mustDay(t, "2027-02-01")

	empty := PayFirst(nil, asOf, set, NoBudgetLimit)
	if len(empty.Actions) != 0 || empty.CashNeeded != 0 {
		t.Errorf("an empty plan is not empty: %+v", empty)
	}

	// Two runs over the same data must give the same order, so a printed plan
	// can be acted on tomorrow.
	lines := AssessAll(testPortfolio(t), asOf, set)
	a := PayFirst(lines, asOf, set, 20_000_000)
	b := PayFirst(lines, asOf, set, 20_000_000)
	if len(a.Actions) != len(b.Actions) {
		t.Fatal("two runs gave different plans")
	}
	for i := range a.Actions {
		if a.Actions[i] != b.Actions[i] {
			t.Errorf("position %d differs between runs: %+v and %+v", i, a.Actions[i], b.Actions[i])
		}
	}

	// A plan with no as-of date has no horizon to compute, and must not invent
	// one from the zero date.
	noDate := PayFirst(lines, Day{}, set, NoBudgetLimit)
	if !noDate.Horizon.IsZero() {
		t.Errorf("horizon = %s, want unset", noDate.Horizon)
	}
}

func TestSettingsNormaliseAndValidate(t *testing.T) {
	d := DefaultSettings()
	if d.PaymentWindowDays != PaymentWindowDays {
		t.Errorf("the default window is %d days, want %d", d.PaymentWindowDays, PaymentWindowDays)
	}
	if d.InterestRatePct != InterestRateAnnualPctDefault {
		t.Errorf("the default rate is %.2f, want %.2f", d.InterestRatePct, InterestRateAnnualPctDefault)
	}
	if d.ReturnDueDay != MonthlyDueDay || d.ReturnPeriodMonths != MonthlyPeriodMonths {
		t.Errorf("the default filer is monthly on the %dth: got %d months, day %d", MonthlyDueDay, d.ReturnPeriodMonths, d.ReturnDueDay)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("the default settings should validate: %v", err)
	}
	if d.Quarterly() {
		t.Error("the default filer is monthly")
	}

	// An empty settings object takes the statutory position for everything
	// that cannot meaningfully be nil.
	n := Settings{}.Normalise()
	if n.PaymentWindowDays != d.PaymentWindowDays || n.ReturnPeriodMonths != d.ReturnPeriodMonths ||
		n.ReturnDueDay != d.ReturnDueDay || n.InterestBasis != d.InterestBasis {
		t.Errorf("Settings{}.Normalise() = %+v, want the defaults %+v", n, d)
	}
	// The interest rate and the horizon are left alone, because nil is a
	// position a user may hold: no interest, and no horizon.
	if n.InterestRatePct != 0 || n.PlanHorizonDays != 0 {
		t.Errorf("Normalise overwrote a deliberate nil: rate %.2f, horizon %d", n.InterestRatePct, n.PlanHorizonDays)
	}
	if err := n.Validate(); err != nil {
		t.Errorf("normalised empty settings should validate: %v", err)
	}
	if len(n.BucketEdges) != len(d.BucketEdges) {
		t.Fatalf("normalised ageing edges %v, want %v", n.BucketEdges, d.BucketEdges)
	}
	for i := range n.BucketEdges {
		if n.BucketEdges[i] != d.BucketEdges[i] {
			t.Errorf("normalised ageing edges %v, want %v", n.BucketEdges, d.BucketEdges)
			break
		}
	}
	// Normalising must not hand back the package's own slice to be scribbled on.
	n.BucketEdges[0] = 999
	if DefaultBucketEdges[0] == 999 {
		t.Fatal("Normalise shared the package's ageing edges with its caller")
	}

	// A quarterly filer who has not said which group gets the earlier of the
	// two due days, which is the safer one to assume.
	q := Settings{ReturnPeriodMonths: QuarterlyPeriodMonths}.Normalise()
	if q.ReturnDueDay != QuarterlyDueDayEarly {
		t.Errorf("a quarterly filer defaults to the %dth, got the %dth", QuarterlyDueDayEarly, q.ReturnDueDay)
	}
	if !q.Quarterly() {
		t.Error("Quarterly() should be true")
	}

	// A period length that is neither monthly nor quarterly falls back rather
	// than producing a return period that does not exist.
	odd := Settings{ReturnPeriodMonths: 7}.Normalise()
	if odd.ReturnPeriodMonths != MonthlyPeriodMonths {
		t.Errorf("a %d-month period normalised to %d, want %d", 7, odd.ReturnPeriodMonths, MonthlyPeriodMonths)
	}

	bad := []struct {
		name string
		set  Settings
	}{
		{name: "no window", set: Settings{PaymentWindowDays: 0, ReturnPeriodMonths: 1, ReturnDueDay: 20, InterestBasis: InterestFromReversalDue}},
		{name: "an absurd window", set: Settings{PaymentWindowDays: 99_999, ReturnPeriodMonths: 1, ReturnDueDay: 20, InterestBasis: InterestFromReversalDue}},
		{name: "a two-month return period", set: Settings{PaymentWindowDays: 180, ReturnPeriodMonths: 2, ReturnDueDay: 20, InterestBasis: InterestFromReversalDue}},
		{name: "a due day of 31", set: Settings{PaymentWindowDays: 180, ReturnPeriodMonths: 1, ReturnDueDay: 31, InterestBasis: InterestFromReversalDue}},
		{name: "a negative rate", set: Settings{PaymentWindowDays: 180, ReturnPeriodMonths: 1, ReturnDueDay: 20, InterestRatePct: -1, InterestBasis: InterestFromReversalDue}},
		{name: "an absurd rate", set: Settings{PaymentWindowDays: 180, ReturnPeriodMonths: 1, ReturnDueDay: 20, InterestRatePct: 500, InterestBasis: InterestFromReversalDue}},
		{name: "an unknown interest basis", set: Settings{PaymentWindowDays: 180, ReturnPeriodMonths: 1, ReturnDueDay: 20, InterestBasis: "whenever"}},
		{name: "a negative horizon", set: Settings{PaymentWindowDays: 180, ReturnPeriodMonths: 1, ReturnDueDay: 20, InterestBasis: InterestFromReversalDue, PlanHorizonDays: -1}},
		{name: "a negative ageing edge", set: Settings{PaymentWindowDays: 180, ReturnPeriodMonths: 1, ReturnDueDay: 20, InterestBasis: InterestFromReversalDue, BucketEdges: []int{-5}}},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			if err := c.set.Validate(); err == nil {
				t.Error("want an error")
			}
		})
	}

	// A nil rate is a position a user may hold, not a mistake.
	nilRate := DefaultSettings()
	nilRate.InterestRatePct = 0
	if err := nilRate.Validate(); err != nil {
		t.Errorf("a nil interest rate should be allowed: %v", err)
	}
}

func TestInterestBasisHelpers(t *testing.T) {
	for _, b := range []InterestBasis{InterestFromReversalDue, InterestFromAvailment} {
		if !b.Valid() {
			t.Errorf("%q should be valid", b)
		}
		if b.Label() == "" {
			t.Errorf("%q should describe itself", b)
		}
	}
	if InterestBasis("someday").Valid() {
		t.Error("an unknown basis should not be valid")
	}
	if InterestFromReversalDue.Label() == InterestFromAvailment.Label() {
		t.Error("the two bases should describe themselves differently")
	}
}
