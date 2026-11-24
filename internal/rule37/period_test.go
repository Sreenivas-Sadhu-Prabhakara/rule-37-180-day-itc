package rule37

import "testing"

func TestPeriodOf(t *testing.T) {
	cases := []struct {
		name   string
		date   string
		months int
		want   string // Code()
	}{
		{name: "monthly, mid-month", date: "2027-01-11", months: 1, want: "2027-01"},
		{name: "monthly, last day", date: "2027-01-31", months: 1, want: "2027-01"},
		{name: "monthly, first day", date: "2027-02-01", months: 1, want: "2027-02"},
		{name: "quarterly, April starts a quarter", date: "2027-04-01", months: 3, want: "2027-04..06"},
		{name: "quarterly, June ends it", date: "2027-06-30", months: 3, want: "2027-04..06"},
		{name: "quarterly, July starts the next", date: "2027-07-01", months: 3, want: "2027-07..09"},
		{name: "quarterly, December", date: "2026-12-20", months: 3, want: "2026-10..12"},
		{name: "quarterly, January is a quarter of its own", date: "2027-01-15", months: 3, want: "2027-01..03"},
		{name: "quarterly, March ends it", date: "2027-03-31", months: 3, want: "2027-01..03"},
		{name: "an odd length falls back to monthly", date: "2027-05-05", months: 2, want: "2027-05"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PeriodOf(mustDay(t, c.date), c.months).Code(); got != c.want {
				t.Errorf("PeriodOf(%s, %d) = %q, want %q", c.date, c.months, got, c.want)
			}
		})
	}
}

// TestReversalPeriodAndDueDate is the rule that decides which return carries
// the reversal: the tax period immediately following the one the 180 days
// expire in, and the day that return falls due.
func TestReversalPeriodAndDueDate(t *testing.T) {
	cases := []struct {
		name           string
		deadline       string
		months, dueDay int
		wantPeriod     string
		wantDue        string
	}{
		{name: "monthly, mid-January deadline", deadline: "2027-01-11", months: 1, dueDay: MonthlyDueDay,
			wantPeriod: "February 2027", wantDue: "2027-03-20"},
		// One day of invoice date moves the reversal a whole return period: a
		// deadline on the last day of June lands in the July return, one on the
		// first of July in the August return.
		{name: "monthly, deadline on the last day of June", deadline: "2027-06-30", months: 1, dueDay: MonthlyDueDay,
			wantPeriod: "July 2027", wantDue: "2027-08-20"},
		{name: "monthly, deadline on the first day of July", deadline: "2027-07-01", months: 1, dueDay: MonthlyDueDay,
			wantPeriod: "August 2027", wantDue: "2027-09-20"},
		{name: "monthly, deadline in December rolls the year", deadline: "2026-12-27", months: 1, dueDay: MonthlyDueDay,
			wantPeriod: "January 2027", wantDue: "2027-02-20"},
		{name: "monthly, deadline in a leap February", deadline: "2028-02-29", months: 1, dueDay: MonthlyDueDay,
			wantPeriod: "March 2028", wantDue: "2028-04-20"},
		{name: "quarterly, deadline in the April quarter", deadline: "2027-06-30", months: 3, dueDay: QuarterlyDueDayEarly,
			wantPeriod: "July–September 2027", wantDue: "2027-10-22"},
		{name: "quarterly, deadline in the July quarter", deadline: "2027-07-01", months: 3, dueDay: QuarterlyDueDayEarly,
			wantPeriod: "October–December 2027", wantDue: "2028-01-22"},
		{name: "quarterly, deadline in the January quarter", deadline: "2027-01-15", months: 3, dueDay: QuarterlyDueDayEarly,
			wantPeriod: "April–June 2027", wantDue: "2027-07-22"},
		{name: "quarterly, deadline in the October quarter", deadline: "2026-12-20", months: 3, dueDay: QuarterlyDueDayLate,
			wantPeriod: "January–March 2027", wantDue: "2027-04-24"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			period := PeriodOf(mustDay(t, c.deadline), c.months)
			for i := 0; i < ReversalReturnOffsetPeriods; i++ {
				period = period.Next()
			}
			if got := period.String(); got != c.wantPeriod {
				t.Errorf("reversal period = %q, want %q", got, c.wantPeriod)
			}
			if got := period.DueDate(c.dueDay).String(); got != c.wantDue {
				t.Errorf("return due = %s, want %s", got, c.wantDue)
			}
		})
	}
}

func TestPeriodStartEndAndNext(t *testing.T) {
	cases := []struct {
		name                         string
		year, month, months          int
		wantStart, wantEnd, wantNext string
	}{
		{name: "February in a common year", year: 2027, month: 2, months: 1,
			wantStart: "2027-02-01", wantEnd: "2027-02-28", wantNext: "2027-03"},
		{name: "February in a leap year", year: 2028, month: 2, months: 1,
			wantStart: "2028-02-01", wantEnd: "2028-02-29", wantNext: "2028-03"},
		{name: "December rolls the year", year: 2026, month: 12, months: 1,
			wantStart: "2026-12-01", wantEnd: "2026-12-31", wantNext: "2027-01"},
		{name: "a 30-day month", year: 2027, month: 4, months: 1,
			wantStart: "2027-04-01", wantEnd: "2027-04-30", wantNext: "2027-05"},
		{name: "the January to March quarter in a leap year", year: 2028, month: 1, months: 3,
			wantStart: "2028-01-01", wantEnd: "2028-03-31", wantNext: "2028-04..06"},
		{name: "the October to December quarter", year: 2026, month: 10, months: 3,
			wantStart: "2026-10-01", wantEnd: "2026-12-31", wantNext: "2027-01..03"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := TaxPeriod{Year: c.year, Month: c.month, Months: c.months}
			if got := p.Start().String(); got != c.wantStart {
				t.Errorf("Start() = %s, want %s", got, c.wantStart)
			}
			if got := p.End().String(); got != c.wantEnd {
				t.Errorf("End() = %s, want %s", got, c.wantEnd)
			}
			if got := p.Next().Code(); got != c.wantNext {
				t.Errorf("Next() = %s, want %s", got, c.wantNext)
			}
		})
	}
}

func TestPeriodDueDateClampsToTheMonth(t *testing.T) {
	// A due day past the end of the following month is clamped rather than
	// rolling into the month after, which would move the deadline silently.
	p := TaxPeriod{Year: 2027, Month: 1, Months: 1}
	if got, want := p.DueDate(31).String(), "2027-02-28"; got != want {
		t.Errorf("due day 31 after January 2027 = %s, want %s", got, want)
	}
	leap := TaxPeriod{Year: 2028, Month: 1, Months: 1}
	if got, want := leap.DueDate(31).String(), "2028-02-29"; got != want {
		t.Errorf("due day 31 after January 2028 = %s, want %s", got, want)
	}
	if got, want := p.DueDate(0).String(), "2027-02-20"; got != want {
		t.Errorf("an unset due day should fall back to the %dth: got %s, want %s", MonthlyDueDay, got, want)
	}
}

func TestPeriodContains(t *testing.T) {
	month := TaxPeriod{Year: 2027, Month: 2, Months: 1}
	quarter := TaxPeriod{Year: 2027, Month: 1, Months: 3}
	cases := []struct {
		period TaxPeriod
		date   string
		want   bool
	}{
		{month, "2027-02-01", true},
		{month, "2027-02-28", true},
		{month, "2027-01-31", false},
		{month, "2027-03-01", false},
		{quarter, "2027-01-01", true},
		{quarter, "2027-03-31", true},
		{quarter, "2027-04-01", false},
		{quarter, "2026-12-31", false},
	}
	for _, c := range cases {
		if got := c.period.Contains(mustDay(t, c.date)); got != c.want {
			t.Errorf("%s contains %s = %v, want %v", c.period.Code(), c.date, got, c.want)
		}
	}
	if (TaxPeriod{}).Contains(mustDay(t, "2027-01-01")) {
		t.Error("the zero period should contain nothing")
	}
	if month.Contains(Day{}) {
		t.Error("no period contains the zero date")
	}
}

func TestPeriodCompareAndZero(t *testing.T) {
	a := TaxPeriod{Year: 2027, Month: 1, Months: 1}
	b := TaxPeriod{Year: 2027, Month: 2, Months: 1}
	c := TaxPeriod{Year: 2028, Month: 1, Months: 1}
	if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Error("Compare within a year is wrong")
	}
	if a.Compare(c) != -1 || c.Compare(a) != 1 {
		t.Error("Compare across years is wrong")
	}
	var zero TaxPeriod
	if !zero.IsZero() {
		t.Error("the zero period should report IsZero")
	}
	if got := zero.Code(); got != "" {
		t.Errorf("the zero period's code is %q, want an empty string", got)
	}
	if got := zero.String(); got != "" {
		t.Errorf("the zero period's name is %q, want an empty string", got)
	}
	if !zero.Next().IsZero() || !zero.Start().IsZero() || !zero.End().IsZero() || !zero.DueDate(20).IsZero() {
		t.Error("nothing should be derivable from the zero period")
	}
}

func TestQuarterlyDueDayByState(t *testing.T) {
	// Rule 61(1)(ii) splits the States and Union territories into two groups.
	// Chandigarh and Delhi are in the later group, which is the entry most
	// often got wrong.
	cases := []struct {
		state string
		want  int
		known bool
	}{
		{state: "Karnataka", want: QuarterlyDueDayEarly, known: true},
		{state: "Maharashtra", want: QuarterlyDueDayEarly, known: true},
		{state: "Tamil Nadu", want: QuarterlyDueDayEarly, known: true},
		{state: "tamilnadu", want: QuarterlyDueDayEarly, known: true},
		{state: "Puducherry", want: QuarterlyDueDayEarly, known: true},
		{state: "Pondicherry", want: QuarterlyDueDayEarly, known: true},
		{state: "Lakshadweep", want: QuarterlyDueDayEarly, known: true},
		{state: "Delhi", want: QuarterlyDueDayLate, known: true},
		{state: "Chandigarh", want: QuarterlyDueDayLate, known: true},
		{state: "West Bengal", want: QuarterlyDueDayLate, known: true},
		{state: "Odisha", want: QuarterlyDueDayLate, known: true},
		{state: "Orissa", want: QuarterlyDueDayLate, known: true},
		{state: "Jammu and Kashmir", want: QuarterlyDueDayLate, known: true},
		{state: "  ladakh  ", want: QuarterlyDueDayLate, known: true},
		{state: "Atlantis", known: false},
		{state: "", known: false},
	}
	for _, c := range cases {
		got, ok := QuarterlyDueDayFor(c.state)
		if ok != c.known {
			t.Errorf("QuarterlyDueDayFor(%q) known = %v, want %v", c.state, ok, c.known)
			continue
		}
		if ok && got != c.want {
			t.Errorf("QuarterlyDueDayFor(%q) = %d, want %d", c.state, got, c.want)
		}
	}

	early := StatesByQuarterlyDueDay(QuarterlyDueDayEarly)
	late := StatesByQuarterlyDueDay(QuarterlyDueDayLate)
	if len(early) != 14 {
		t.Errorf("the %dth-day group has %d entries, want 14: %v", QuarterlyDueDayEarly, len(early), early)
	}
	if len(late) != 22 {
		t.Errorf("the %dth-day group has %d entries, want 22: %v", QuarterlyDueDayLate, len(late), late)
	}
	if len(early)+len(late) != 36 {
		t.Errorf("the two groups cover %d States and Union territories, want 36", len(early)+len(late))
	}
	for _, s := range early {
		if d, _ := QuarterlyDueDayFor(s); d != QuarterlyDueDayEarly {
			t.Errorf("%s is listed in the wrong group", s)
		}
	}
	if !StateNameLooksLike("Kerala") || StateNameLooksLike("Nowhere") || StateNameLooksLike("") {
		t.Error("StateNameLooksLike is wrong")
	}
}
