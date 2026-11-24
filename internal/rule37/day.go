package rule37

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DayLayout is the calendar-date format used in the JSON API and the data file.
const DayLayout = "2006-01-02"

// Day is a calendar date with no clock time and no time zone.
//
// The 180-day period in the second proviso to section 16(2) is counted in whole
// days from the date the supplier issued the invoice, so the domain
// deliberately has no notion of hours: every Day is held as midnight UTC, which
// makes day subtraction exact and immune to daylight-saving shifts and to the
// machine's own time zone.
//
// The zero Day means "not set" — an invoice not yet paid, a payment whose date
// the register did not record — and encodes to an empty JSON string.
type Day struct {
	t time.Time
}

// NewDay builds a Day from a year, month and day of month. Out-of-range values
// normalise the way time.Date does (31 April becomes 1 May).
func NewDay(year int, month time.Month, day int) Day {
	return Day{t: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// DayOf truncates any time to the calendar date it falls on in its own location.
func DayOf(t time.Time) Day {
	y, m, d := t.Date()
	return NewDay(y, m, d)
}

// ParseDay reads a YYYY-MM-DD date. An empty or blank string yields the zero
// Day, which the domain reads as "not set".
func ParseDay(s string) (Day, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Day{}, nil
	}
	t, err := time.Parse(DayLayout, s)
	if err != nil {
		return Day{}, fmt.Errorf("invalid date %q: write it as YYYY-MM-DD", s)
	}
	return Day{t: t.UTC()}, nil
}

// IsZero reports whether the date is unset.
func (d Day) IsZero() bool { return d.t.IsZero() }

// String renders the date as YYYY-MM-DD, or an empty string when unset.
func (d Day) String() string {
	if d.IsZero() {
		return ""
	}
	return d.t.Format(DayLayout)
}

// Pretty renders the date for printed working papers, e.g. "02 April 2026".
func (d Day) Pretty() string {
	if d.IsZero() {
		return "—"
	}
	return d.t.Format("02 January 2006")
}

// Indian renders the date the way Indian invoices write it, dd-mm-yyyy.
func (d Day) Indian() string {
	if d.IsZero() {
		return ""
	}
	return d.t.Format("02-01-2006")
}

// Time returns the underlying midnight-UTC instant.
func (d Day) Time() time.Time { return d.t }

// Year, Month and DayOfMonth break the date into its parts.
func (d Day) Year() int         { return d.t.Year() }
func (d Day) Month() time.Month { return d.t.Month() }
func (d Day) DayOfMonth() int   { return d.t.Day() }

// AddDays returns the date n days later (n may be negative). The calendar does
// the work, so a leap day inside the span is counted exactly once.
func (d Day) AddDays(n int) Day {
	if d.IsZero() {
		return d
	}
	return Day{t: d.t.AddDate(0, 0, n)}
}

// AddMonths returns the date n calendar months later, clamped to the last day
// of the target month: 31 January plus one month is 28 February (29 February in
// a leap year), never 2 or 3 March.
func (d Day) AddMonths(n int) Day {
	if d.IsZero() {
		return d
	}
	year, month, dom := d.t.Date()
	// Shift from the first of the month so AddDate never rolls into the next
	// month, then clamp the day of month.
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, n, 0)
	ty, tm, _ := first.Date()
	if last := DaysInMonth(ty, tm); dom > last {
		dom = last
	}
	return NewDay(ty, tm, dom)
}

// DaysInMonth returns the number of days in the given month.
func DaysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// Before reports whether d falls before o.
func (d Day) Before(o Day) bool { return d.t.Before(o.t) }

// After reports whether d falls after o.
func (d Day) After(o Day) bool { return d.t.After(o.t) }

// Equal reports whether d and o are the same calendar date.
func (d Day) Equal(o Day) bool { return d.t.Equal(o.t) }

// OnOrBefore reports whether d falls on or before o.
func (d Day) OnOrBefore(o Day) bool { return !d.t.After(o.t) }

// DaysUntil counts whole days from d to o. The result is negative when o falls
// before d and zero when they are the same date.
func (d Day) DaysUntil(o Day) int {
	return int(o.t.Sub(d.t) / (24 * time.Hour))
}

// MarshalJSON encodes the date as a YYYY-MM-DD string ("" when unset).
func (d Day) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalJSON decodes a YYYY-MM-DD string; "" and null mean "not set".
func (d *Day) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*d = Day{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("date must be a YYYY-MM-DD string: %w", err)
	}
	parsed, err := ParseDay(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// FinancialYearStart is the month the Indian financial year begins in.
const FinancialYearStart = time.April

// FinancialYear returns the Indian financial year containing d, written the way
// returns and ledgers write it: "2026-27". The year runs 1 April to 31 March,
// so 31 March 2027 belongs to 2026-27 and 1 April 2027 begins 2027-28.
func FinancialYear(d Day) string {
	if d.IsZero() {
		return ""
	}
	y := d.Year()
	if d.Month() < FinancialYearStart {
		y--
	}
	return fmt.Sprintf("%d-%02d", y, (y+1)%100)
}

// dateLayouts are the ways invoice and payment dates turn up in registers
// exported from spreadsheets and accounting packages. dd-mm-yyyy is the Indian
// convention and is tried first, so 05-04-2026 reads as 5 April, not 4 May.
var dateLayouts = []string{
	"02-01-2006",
	"02/01/2006",
	"02.01.2006",
	"2-1-2006",
	"2/1/2006",
	"2006-01-02",
	"2006/01/02",
	"02-Jan-2006",
	"02 Jan 2006",
	"2-Jan-2006",
	"02-January-2006",
	"02 January 2006",
	"02-01-06",
	"02/01/06",
	"02-01-2006 15:04:05",
	"02/01/2006 15:04:05",
	"2006-01-02 15:04:05",
	time.RFC3339,
}

// ParseLooseDate reads a date written any of the usual ways. Impossible dates
// are rejected rather than rolled over, so 29-02-2027 is an error while
// 29-02-2028 is valid.
func ParseLooseDate(s string) (Day, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return Day{}, fmt.Errorf("the date is empty")
	}
	for _, layout := range dateLayouts {
		if d, err := time.Parse(layout, t); err == nil {
			return NewDay(d.Year(), d.Month(), d.Day()), nil
		}
	}
	return Day{}, fmt.Errorf("%q is not a date — write it as dd-mm-yyyy, for example 15-07-2026", t)
}
