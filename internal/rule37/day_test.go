package rule37

import (
	"encoding/json"
	"testing"
	"time"
)

// TestAddDaysOverTheWindow checks the one piece of arithmetic the whole rule
// rests on: the invoice date plus 180 days. The cases are chosen so that a
// naive "six months" or "182 days" shortcut, or a span that miscounts a leap
// day, would fail.
func TestAddDaysOverTheWindow(t *testing.T) {
	cases := []struct {
		name          string
		invoice, want string
	}{
		{name: "new year, no leap day in the span", invoice: "2027-01-01", want: "2027-06-30"},
		{name: "one day later moves the deadline a day", invoice: "2027-01-02", want: "2027-07-01"},
		{name: "span crosses 29 February 2028", invoice: "2027-10-04", want: "2028-04-01"},
		{name: "same date a year earlier, no leap day", invoice: "2026-10-04", want: "2027-04-02"},
		{name: "invoice issued on a leap day", invoice: "2028-02-29", want: "2028-08-27"},
		{name: "last day of a year", invoice: "2026-12-31", want: "2027-06-29"},
		{name: "last day of a year, leap span", invoice: "2027-12-31", want: "2028-06-28"},
		{name: "31 January, a long month", invoice: "2026-01-31", want: "2026-07-30"},
		{name: "31 January in a leap year", invoice: "2028-01-31", want: "2028-07-29"},
		{name: "28 February in a common year", invoice: "2027-02-28", want: "2027-08-27"},
		{name: "30 June, a short month", invoice: "2026-06-30", want: "2026-12-27"},
		{name: "mid-month", invoice: "2026-07-15", want: "2027-01-11"},
		{name: "span ends on 2 March of a leap year", invoice: "2027-09-04", want: "2028-03-02"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv := mustDay(t, c.invoice)
			got := inv.AddDays(PaymentWindowDays)
			if got.String() != c.want {
				t.Errorf("%s + %d days = %s, want %s", c.invoice, PaymentWindowDays, got, c.want)
			}
			// The count back must agree, which is what makes "days left" exact.
			if n := inv.DaysUntil(got); n != PaymentWindowDays {
				t.Errorf("DaysUntil from %s to %s = %d, want %d", c.invoice, got, n, PaymentWindowDays)
			}
		})
	}
}

func TestDaysUntil(t *testing.T) {
	cases := []struct {
		from, to string
		want     int
	}{
		{"2026-11-24", "2027-05-23", 180},
		{"2026-11-24", "2027-05-22", 179},
		{"2026-11-24", "2026-11-24", 0},
		{"2027-05-23", "2026-11-24", -180},
		{"2028-02-28", "2028-03-01", 2}, // 29 February exists in 2028
		{"2027-02-28", "2027-03-01", 1}, // and not in 2027
		{"2027-12-31", "2028-01-01", 1},
	}
	for _, c := range cases {
		if got := mustDay(t, c.from).DaysUntil(mustDay(t, c.to)); got != c.want {
			t.Errorf("%s to %s = %d days, want %d", c.from, c.to, got, c.want)
		}
	}
}

func TestAddMonthsClampsToTheMonthEnd(t *testing.T) {
	cases := []struct {
		name   string
		from   string
		months int
		want   string
	}{
		{name: "31 January plus one month, common year", from: "2027-01-31", months: 1, want: "2027-02-28"},
		{name: "31 January plus one month, leap year", from: "2028-01-31", months: 1, want: "2028-02-29"},
		{name: "31 March plus one month", from: "2027-03-31", months: 1, want: "2027-04-30"},
		{name: "29 February plus twelve months", from: "2028-02-29", months: 12, want: "2029-02-28"},
		{name: "31 December plus one month", from: "2026-12-31", months: 1, want: "2027-01-31"},
		{name: "back a month from 31 March", from: "2027-03-31", months: -1, want: "2027-02-28"},
		{name: "mid-month is untouched", from: "2027-05-15", months: 3, want: "2027-08-15"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mustDay(t, c.from).AddMonths(c.months); got.String() != c.want {
				t.Errorf("%s plus %d months = %s, want %s", c.from, c.months, got, c.want)
			}
		})
	}
}

func TestDaysInMonth(t *testing.T) {
	cases := []struct {
		year  int
		month time.Month
		want  int
	}{
		{2027, time.February, 28},
		{2028, time.February, 29}, // divisible by four
		{2100, time.February, 28}, // a century that is not a leap year
		{2000, time.February, 29}, // and one that is
		{2027, time.January, 31},
		{2027, time.April, 30},
		{2027, time.December, 31},
	}
	for _, c := range cases {
		if got := DaysInMonth(c.year, c.month); got != c.want {
			t.Errorf("DaysInMonth(%d, %s) = %d, want %d", c.year, c.month, got, c.want)
		}
	}
}

func TestParseDay(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		isZero bool
		bad    bool
	}{
		{in: "2026-11-24", want: "2026-11-24"},
		{in: "  2026-11-24  ", want: "2026-11-24"},
		{in: "", isZero: true},
		{in: "   ", isZero: true},
		{in: "24-11-2026", bad: true},
		{in: "2026-13-01", bad: true},
		{in: "2027-02-29", bad: true}, // 2027 is not a leap year
		{in: "not a date", bad: true},
	}
	for _, c := range cases {
		got, err := ParseDay(c.in)
		switch {
		case c.bad:
			if err == nil {
				t.Errorf("ParseDay(%q) = %s, want an error", c.in, got)
			}
		case err != nil:
			t.Errorf("ParseDay(%q): %v", c.in, err)
		case c.isZero:
			if !got.IsZero() {
				t.Errorf("ParseDay(%q) = %s, want the zero date", c.in, got)
			}
		case got.String() != c.want:
			t.Errorf("ParseDay(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestParseLooseDate(t *testing.T) {
	cases := []struct {
		in   string
		want string
		bad  bool
	}{
		{in: "15-07-2026", want: "2026-07-15"},
		{in: "15/07/2026", want: "2026-07-15"},
		{in: "15.07.2026", want: "2026-07-15"},
		{in: "5-7-2026", want: "2026-07-05"},
		{in: "2026-07-15", want: "2026-07-15"},
		{in: "15-Jul-2026", want: "2026-07-15"},
		{in: "15 July 2026", want: "2026-07-15"},
		{in: "15-07-26", want: "2026-07-15"},
		{in: "15-07-2026 10:30:00", want: "2026-07-15"},
		// Indian order is tried first, so 05-04 is 5 April, not 4 May.
		{in: "05-04-2026", want: "2026-04-05"},
		{in: "29-02-2028", want: "2028-02-29"},
		// Impossible dates are refused rather than rolled into March.
		{in: "29-02-2027", bad: true},
		{in: "31-04-2026", bad: true},
		{in: "", bad: true},
		{in: "tomorrow", bad: true},
	}
	for _, c := range cases {
		got, err := ParseLooseDate(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("ParseLooseDate(%q) = %s, want an error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLooseDate(%q): %v", c.in, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("ParseLooseDate(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestDayFormatting(t *testing.T) {
	d := NewDay(2026, time.April, 2)
	if got, want := d.String(), "2026-04-02"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := d.Pretty(), "02 April 2026"; got != want {
		t.Errorf("Pretty() = %q, want %q", got, want)
	}
	if got, want := d.Indian(), "02-04-2026"; got != want {
		t.Errorf("Indian() = %q, want %q", got, want)
	}
	var zero Day
	if got := zero.String(); got != "" {
		t.Errorf("the zero date renders as %q, want an empty string", got)
	}
	if got, want := zero.Pretty(), "—"; got != want {
		t.Errorf("the zero date prints as %q, want %q", got, want)
	}
	if got := zero.Indian(); got != "" {
		t.Errorf("the zero date in Indian form is %q, want an empty string", got)
	}
	// A zero date must not acquire one by arithmetic.
	if !zero.AddDays(180).IsZero() || !zero.AddMonths(1).IsZero() {
		t.Error("arithmetic on the zero date should leave it unset")
	}
}

func TestDayComparisons(t *testing.T) {
	a := NewDay(2026, time.November, 24)
	b := NewDay(2026, time.November, 25)
	if !a.Before(b) || b.Before(a) {
		t.Error("Before is wrong")
	}
	if !b.After(a) || a.After(b) {
		t.Error("After is wrong")
	}
	if !a.Equal(NewDay(2026, time.November, 24)) {
		t.Error("Equal is wrong")
	}
	if !a.OnOrBefore(a) || !a.OnOrBefore(b) || b.OnOrBefore(a) {
		t.Error("OnOrBefore is wrong")
	}
}

func TestDayJSON(t *testing.T) {
	type wrapper struct {
		D Day `json:"d"`
	}
	raw, err := json.Marshal(wrapper{D: NewDay(2026, time.November, 24)})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"d":"2026-11-24"}`; got != want {
		t.Errorf("marshalled as %s, want %s", got, want)
	}
	raw, err = json.Marshal(wrapper{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"d":""}`; got != want {
		t.Errorf("the zero date marshalled as %s, want %s", got, want)
	}

	for _, in := range []string{`{"d":"2026-11-24"}`, `{"d":""}`, `{"d":null}`} {
		var w wrapper
		if err := json.Unmarshal([]byte(in), &w); err != nil {
			t.Errorf("unmarshalling %s: %v", in, err)
		}
	}
	for _, in := range []string{`{"d":"24-11-2026"}`, `{"d":5}`, `{"d":"nonsense"}`} {
		var w wrapper
		if err := json.Unmarshal([]byte(in), &w); err == nil {
			t.Errorf("unmarshalling %s should have failed, got %s", in, w.D)
		}
	}
}

func TestFinancialYear(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"2026-04-01", "2026-27"},
		{"2027-03-31", "2026-27"},
		{"2027-04-01", "2027-28"},
		{"2026-03-31", "2025-26"},
		{"2099-04-01", "2099-00"},
	}
	for _, c := range cases {
		if got := FinancialYear(mustDay(t, c.in)); got != c.want {
			t.Errorf("FinancialYear(%s) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := FinancialYear(Day{}); got != "" {
		t.Errorf("FinancialYear of the zero date = %q, want an empty string", got)
	}
}

func TestDayOfIgnoresTheClock(t *testing.T) {
	// A tool that reads "today" from a machine in any time zone must land on
	// the same calendar date the user would write on a cheque.
	loc := time.FixedZone("IST", 5*3600+1800)
	got := DayOf(time.Date(2026, time.November, 24, 23, 45, 0, 0, loc))
	if want := "2026-11-24"; got.String() != want {
		t.Errorf("DayOf(late evening in IST) = %s, want %s", got, want)
	}
}

func mustDay(t *testing.T, s string) Day {
	t.Helper()
	d, err := ParseDay(s)
	if err != nil {
		t.Fatalf("bad test date %q: %v", s, err)
	}
	return d
}
