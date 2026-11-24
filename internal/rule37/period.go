package rule37

import (
	"fmt"
	"time"
)

// MonthlyPeriodMonths and QuarterlyPeriodMonths are the two lengths a GST
// return period can have: one month for an ordinary filer, and three months for
// a filer who has opted into the Quarterly Return Monthly Payment scheme, whose
// FORM GSTR-3B covers a quarter.
const (
	MonthlyPeriodMonths   = 1
	QuarterlyPeriodMonths = 3
)

// MonthlyDueDay is the day of the following month by which a monthly filer must
// furnish FORM GSTR-3B — the twentieth — under section 39 of the Central Goods
// and Services Tax Act, 2017 read with rule 61 of the Central Goods and
// Services Tax Rules, 2017.
const MonthlyDueDay = 20

// QuarterlyDueDayEarly and QuarterlyDueDayLate are the two due days for a
// quarterly (QRMP) filer's FORM GSTR-3B — the twenty-second or the
// twenty-fourth of the month following the quarter, depending on which group of
// States and Union territories the principal place of business falls in.
const (
	QuarterlyDueDayEarly = 22
	QuarterlyDueDayLate  = 24
)

// TaxPeriod is one GST return period: a calendar month for a monthly filer, or
// a quarter for a filer under the Quarterly Return Monthly Payment scheme.
//
// Rule 37(1) fixes the reversal to the return "for the tax period immediately
// following the period of one hundred and eighty days", so which period an
// invoice's deadline falls in, and what the next period is, decide the return
// the entry belongs in. Both are computed here and nowhere else.
type TaxPeriod struct {
	Year int `json:"year"`
	// Month is 1-12. For a quarter it is the first month of the quarter.
	Month int `json:"month"`
	// Months is the length of the period: 1 for a monthly filer, 3 for a
	// quarterly (QRMP) filer.
	Months int `json:"months"`
}

// PeriodOf returns the return period of length months that contains d.
//
// Quarters are aligned to the financial year, so they begin in April, July,
// October and January — which is also what 1 + 3*((m-1)/3) gives, since the
// Indian financial year starts in the first month of a calendar quarter.
func PeriodOf(d Day, months int) TaxPeriod {
	if months != QuarterlyPeriodMonths {
		months = MonthlyPeriodMonths
	}
	m := int(d.Month())
	if months == QuarterlyPeriodMonths {
		m = 1 + 3*((m-1)/3)
	}
	return TaxPeriod{Year: d.Year(), Month: m, Months: months}
}

// IsZero reports whether the period is unset.
func (p TaxPeriod) IsZero() bool { return p.Year == 0 || p.Month == 0 }

// Next returns the period immediately following p.
func (p TaxPeriod) Next() TaxPeriod {
	if p.IsZero() {
		return p
	}
	step := p.Months
	if step <= 0 {
		step = MonthlyPeriodMonths
	}
	m := p.Month + step
	y := p.Year
	for m > 12 {
		m -= 12
		y++
	}
	return TaxPeriod{Year: y, Month: m, Months: p.Months}
}

// Start is the first day of the period.
func (p TaxPeriod) Start() Day {
	if p.IsZero() {
		return Day{}
	}
	return NewDay(p.Year, time.Month(p.Month), 1)
}

// End is the last day of the period: 31 March, 30 April, 28 or 29 February —
// whatever the calendar says, never a day that does not exist.
func (p TaxPeriod) End() Day {
	if p.IsZero() {
		return Day{}
	}
	return p.Next().Start().AddDays(-1)
}

// Contains reports whether d falls inside the period.
func (p TaxPeriod) Contains(d Day) bool {
	if p.IsZero() || d.IsZero() {
		return false
	}
	return !d.Before(p.Start()) && !d.After(p.End())
}

// DueDate is the day FORM GSTR-3B for this period must be furnished: dueDay of
// the month after the period ends. The day is clamped to the length of that
// month, so a due day of 31 never rolls into the next month.
func (p TaxPeriod) DueDate(dueDay int) Day {
	if p.IsZero() {
		return Day{}
	}
	if dueDay < 1 {
		dueDay = MonthlyDueDay
	}
	after := p.End().AddDays(1) // the first day of the month after the period
	if last := DaysInMonth(after.Year(), after.Month()); dueDay > last {
		dueDay = last
	}
	return NewDay(after.Year(), after.Month(), dueDay)
}

// Code is the sortable machine form of the period: "2026-07" for a month,
// "2026-07..09" for a quarter.
func (p TaxPeriod) Code() string {
	if p.IsZero() {
		return ""
	}
	if p.Months == QuarterlyPeriodMonths {
		return fmt.Sprintf("%04d-%02d..%02d", p.Year, p.Month, int(p.End().Month()))
	}
	return fmt.Sprintf("%04d-%02d", p.Year, p.Month)
}

// String names the period the way a return does: "July 2026", or
// "July–September 2026" for a quarter.
func (p TaxPeriod) String() string {
	if p.IsZero() {
		return ""
	}
	start := p.Start()
	if p.Months == QuarterlyPeriodMonths {
		end := p.End()
		if end.Year() != start.Year() {
			return fmt.Sprintf("%s %d–%s %d", start.Month(), start.Year(), end.Month(), end.Year())
		}
		return fmt.Sprintf("%s–%s %d", start.Month(), end.Month(), end.Year())
	}
	return fmt.Sprintf("%s %d", start.Month(), start.Year())
}

// Compare orders two periods in time: -1 when p is earlier, 0 when they begin
// on the same day, +1 when p is later.
func (p TaxPeriod) Compare(o TaxPeriod) int {
	switch {
	case p.Year != o.Year:
		if p.Year < o.Year {
			return -1
		}
		return 1
	case p.Month != o.Month:
		if p.Month < o.Month {
			return -1
		}
		return 1
	}
	return 0
}
