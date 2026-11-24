// Package rule37 implements the one hundred and eighty day payment condition
// that input tax credit is subject to under the goods and services tax, and the
// reversal and re-claim that follow from it.
//
// The rule, in one sentence: a registered person who has taken input tax credit
// on a purchase but has not paid the supplier the value of that supply together
// with the tax on it within 180 days of the date the supplier issued the
// invoice must reverse the credit, in proportion to the amount left unpaid, in
// the return for the tax period immediately following those 180 days — and may
// take the credit again once the supplier is paid.
//
// Authority: the second proviso to section 16(2) of the Central Goods and
// Services Tax Act, 2017, and rule 37 of the Central Goods and Services Tax
// Rules, 2017, as substituted by Notification No. 19/2022-Central Tax dated
// 28 September 2022 with effect from 1 October 2022 and amended by Notification
// No. 26/2022-Central Tax dated 26 December 2022 with retrospective effect from
// the same date.
//
// Every figure the rule turns on is a named constant in this file, with the
// provision it comes from written beside it, and every one of them can be
// overridden by the user through Settings. Nothing in this package reads a
// clock, a file or a network: the whole of it is pure functions over values, so
// the arithmetic can be tested on a fixed date.
package rule37

import (
	"fmt"
	"sort"
)

// PaymentWindowDays is the period within which the recipient of a supply must
// pay the supplier the amount towards the value of the supply together with the
// tax payable on it, counted from the date the supplier issued the invoice.
//
// Second proviso to section 16(2) of the Central Goods and Services Tax Act,
// 2017: "Provided further that where a recipient fails to pay to the supplier of
// goods or services or both, other than the supplies on which tax is payable on
// reverse charge basis, the amount towards the value of supply along with tax
// payable thereon within a period of one hundred and eighty days from the date
// of issue of invoice by the supplier, an amount equal to the input tax credit
// availed by the recipient shall be added to his output tax liability, along
// with interest thereon, in such manner as may be prescribed."
//
// The third proviso is the other half of it, and the authority for the re-claim
// this tool schedules: "Provided also that the recipient shall be entitled to
// avail of the credit of input tax on payment made by him of the amount towards
// the value of supply of goods or services or both along with tax payable
// thereon."
const PaymentWindowDays = 180

// InvoiceDateCountsAsDayZero records the day-counting convention this tool uses:
// the 180 days are counted from the day AFTER the date of the invoice, so the
// last day on which the supplier can be paid is the invoice date plus 180 days.
//
// This is a convention, not something the rule spells out. Neither the second
// proviso to section 16(2) nor rule 37 says whether the date of the invoice is
// itself day one. The convention follows section 9 of the General Clauses Act,
// 1897, under which the word "from" excludes the first day of a period, and it
// is what practice does. The alternative — counting the invoice date as day one
// — makes every deadline one day earlier.
//
// It almost never changes the answer, because rule 37 keys the reversal to a
// whole tax period rather than to a date: it matters only where day 180 falls on
// the first or the last day of a month. The deadline this tool computes is shown
// on every invoice so the count can be checked.
const InvoiceDateCountsAsDayZero = true

// ReversalReturnOffsetPeriods is how many return periods on from the one in
// which the 180 days expire the reversal must be made.
//
// Rule 37(1) of the Central Goods and Services Tax Rules, 2017, as substituted
// with effect from 1 October 2022, requires the amount to be paid or reversed
// "while furnishing the return in FORM GSTR-3B for the tax period immediately
// following the period of one hundred and eighty days from the date of the issue
// of the invoice" — one period on, which is why this is 1.
//
// It is the period AFTER the one day 180 falls in, which is not the same as the
// period the 181st day falls in: those coincide only when day 180 is the last
// day of a period.
const ReversalReturnOffsetPeriods = 1

// InterestRateAnnualPctDefault is the rate of interest notified under
// sub-section (3) of section 50 of the Central Goods and Services Tax Act, 2017
// — eighteen per cent a year — being the rate that applies to input tax credit
// wrongly availed and utilised.
//
// Notified by Notification No. 13/2017-Central Tax dated 28 June 2017, which
// section 116 of the Finance Act, 2022 read with the Sixth Schedule to that Act
// amended retrospectively with effect from 1 July 2017 to substitute eighteen
// per cent for twenty-four per cent against the section 50(3) entry. Section
// 50(3) itself caps the rate the Government may notify at twenty-four per cent.
//
// This tool charges interest only on a reversal made LATE, and labels the figure
// indicative. See InterestBasis for why — and set the rate to nil to take the
// view that none arises at all.
const InterestRateAnnualPctDefault = 18.0

// SectionFiftyCeilingPct is the ceiling section 50(3) puts on the rate the
// Government may notify: "at such rate not exceeding twenty-four per cent as may
// be notified by the Government, on the recommendations of the Council". The
// notified rate is InterestRateAnnualPctDefault; this is only the bound.
const SectionFiftyCeilingPct = 24.0

// DaysInYearForInterest is the denominator used to turn an annual rate into a
// daily one. Rule 88B of the Central Goods and Services Tax Rules, 2017
// computes interest for the period in days at the annual rate, so the year is
// taken as 365 days throughout, including a leap year — which is the convention
// the portal's own interest computation follows.
const DaysInYearForInterest = 365

// MaxInterestRatePct is a sanity bound on the overridable interest rate. It is
// not a statutory figure: no rate notified under section 50 has ever approached
// it, and refusing anything higher catches a mis-keyed percentage.
const MaxInterestRatePct = 100.0

// MaxPaymentWindowDays bounds the overridable payment window at ten years. Not
// a statutory figure either — it exists so that a mis-typed window cannot push
// every deadline past the end of the calendar.
const MaxPaymentWindowDays = 3650

// InterestBasis says what period an indicative interest figure is computed for.
//
// The position on interest changed when rule 37 was substituted with effect from
// 1 October 2022. The rule as it stood before that date carried a sub-rule (3)
// which ran interest from the date the credit was availed until the amount was
// added back to the output tax liability. That sub-rule was omitted by the same
// notification that substituted the rest of the rule. What is left requires the
// amount to be paid or reversed "along with interest payable thereon under
// section 50" in the return for the period immediately following the 180 days,
// without saying under which sub-section of section 50 or for what period.
//
// Section 50(3) now reaches only credit that "has been wrongly availed and
// utilised", and rule 88B(3) computes interest from the date of utilisation to
// the date of reversal — where, by the Explanation to that sub-rule, credit
// counts as utilised only once the balance in the electronic credit ledger falls
// below it. A rule 37 credit was validly availed when it was taken, so there is
// a respectable view that a reversal made in the correct return attracts no
// interest at all, and a further view that a taxpayer who always held enough
// credit in the ledger has no utilisation date and so no period to compute on.
// Against that, some practitioners run interest from the date of availment, on
// the footing that the credit was conditional from the start.
//
// Nothing in any circular settles it. This tool therefore computes interest only
// on a LATE reversal, from one of two starting points the user chooses, labels
// the figure indicative, and never presents it as a demand. Setting the rate to
// nil takes the view that none arises.
type InterestBasis string

const (
	// InterestFromReversalDue runs interest from the due date of the return in
	// which the reversal was required, to the date it is actually reversed.
	// This is the conservative compliance reading, and the default: nothing is
	// late until the return it belonged in has fallen due.
	InterestFromReversalDue InterestBasis = "reversal-due"

	// InterestFromAvailment runs interest from the due date of the return for
	// the tax period of the invoice to the date of reversal. That date is what
	// Explanation (2)(a) to rule 88B(3) treats as the date of utilisation where
	// the credit went to pay tax through the return, so it is the wider reading
	// a proper officer may take. It is offered so the larger exposure can be
	// seen before somebody else works it out.
	InterestFromAvailment InterestBasis = "availment"
)

// Valid reports whether b is one of the two bases.
func (b InterestBasis) Valid() bool {
	switch b {
	case InterestFromReversalDue, InterestFromAvailment:
		return true
	}
	return false
}

// Label describes the basis in a line of plain English.
func (b InterestBasis) Label() string {
	switch b {
	case InterestFromAvailment:
		return "from the due date of the return in which the credit was taken, to the date of reversal"
	default:
		return "from the day after the reversal was due, to the date of reversal"
	}
}

// DefaultBucketEdges are the ageing boundaries, in days elapsed since the
// invoice date, that the watch-list is grouped by. They are chosen around the
// 180-day deadline rather than the 30-day steps of an ordinary payables ageing:
// nothing needs attention in the first two months, and the last month is where
// the credit is actually lost.
//
// Each edge is the last day of its bucket, so {60, 120, 150, 180} gives
// 0–60, 61–120, 121–150, 151–180 and over 180.
var DefaultBucketEdges = []int{60, 120, 150, 180}

// DefaultPlanHorizonDays is how far ahead the payment plan looks by default
// when deciding which invoices are worth paying now: two months, which is long
// enough to cover the current return period and the next.
const DefaultPlanHorizonDays = 60

// Settings are the figures the user may change. Every one of them starts at the
// statutory or conventional default above; they are held in the data file so a
// change survives a restart, and they are reported alongside every result so
// that a number can always be traced to the figures it was computed from.
type Settings struct {
	// PaymentWindowDays is the 180-day window. Overridable because a statutory
	// period can be amended, and a tool that hard-codes it goes quietly wrong.
	PaymentWindowDays int `json:"paymentWindowDays"`

	// ReturnPeriodMonths is 1 for a monthly filer and 3 for a quarterly (QRMP)
	// filer, and decides which return a reversal falls in.
	ReturnPeriodMonths int `json:"returnPeriodMonths"`

	// ReturnDueDay is the day of the following month by which FORM GSTR-3B must
	// be furnished: 20 for a monthly filer, 22 or 24 for a quarterly one
	// depending on the State or Union territory.
	ReturnDueDay int `json:"returnDueDay"`

	// InterestRatePct is the annual rate used for the indicative interest on a
	// late reversal.
	InterestRatePct float64 `json:"interestRatePct"`

	// InterestBasis is the period interest is computed for.
	InterestBasis InterestBasis `json:"interestBasis"`

	// BucketEdges are the ageing boundaries in days since the invoice date.
	BucketEdges []int `json:"bucketEdges"`

	// PlanHorizonDays limits the payment plan to invoices whose deadline falls
	// within this many days. Zero means every invoice still inside its window.
	PlanHorizonDays int `json:"planHorizonDays"`
}

// DefaultSettings returns the statutory position as this tool reads it.
func DefaultSettings() Settings {
	edges := make([]int, len(DefaultBucketEdges))
	copy(edges, DefaultBucketEdges)
	return Settings{
		PaymentWindowDays:  PaymentWindowDays,
		ReturnPeriodMonths: MonthlyPeriodMonths,
		ReturnDueDay:       MonthlyDueDay,
		InterestRatePct:    InterestRateAnnualPctDefault,
		InterestBasis:      InterestFromReversalDue,
		BucketEdges:        edges,
		PlanHorizonDays:    DefaultPlanHorizonDays,
	}
}

// Normalise fills in anything that cannot meaningfully be nil with the
// default, and sorts and de-duplicates the ageing edges. It never rejects:
// Validate does that. This is what makes a half-filled settings object from an
// older data file or a sparse request safe to compute with.
//
// Two settings are deliberately left alone when they are nil, because nil is a
// position a user may hold rather than an omission: an interest rate of nil
// means "no interest arises on a rule 37 reversal", and a planning horizon of
// nil means "look at every open invoice". Seeding a new data file with
// DefaultSettings is what supplies the statutory figures for those two.
func (s Settings) Normalise() Settings {
	d := DefaultSettings()
	if s.PaymentWindowDays <= 0 {
		s.PaymentWindowDays = d.PaymentWindowDays
	}
	if s.ReturnPeriodMonths != QuarterlyPeriodMonths {
		s.ReturnPeriodMonths = MonthlyPeriodMonths
	}
	if s.ReturnDueDay <= 0 {
		if s.ReturnPeriodMonths == QuarterlyPeriodMonths {
			s.ReturnDueDay = QuarterlyDueDayEarly
		} else {
			s.ReturnDueDay = MonthlyDueDay
		}
	}
	if s.InterestRatePct < 0 {
		s.InterestRatePct = d.InterestRatePct
	}
	if !s.InterestBasis.Valid() {
		s.InterestBasis = d.InterestBasis
	}
	if s.PlanHorizonDays < 0 {
		s.PlanHorizonDays = 0
	}
	s.BucketEdges = cleanEdges(s.BucketEdges, d.BucketEdges)
	return s
}

// Validate reports a setting a human would not have meant, with a message aimed
// at whoever typed it.
func (s Settings) Validate() error {
	switch {
	case s.PaymentWindowDays <= 0 || s.PaymentWindowDays > MaxPaymentWindowDays:
		return fmt.Errorf("the payment window must be between 1 and %d days; the rule says %d", MaxPaymentWindowDays, PaymentWindowDays)
	case s.ReturnPeriodMonths != MonthlyPeriodMonths && s.ReturnPeriodMonths != QuarterlyPeriodMonths:
		return fmt.Errorf("a return period is either %d month (monthly filer) or %d months (quarterly, under the QRMP scheme)", MonthlyPeriodMonths, QuarterlyPeriodMonths)
	case s.ReturnDueDay < 1 || s.ReturnDueDay > 28:
		return fmt.Errorf("the return due day must be a day of the month between 1 and 28; it is %d for a monthly filer and %d or %d for a quarterly one", MonthlyDueDay, QuarterlyDueDayEarly, QuarterlyDueDayLate)
	case s.InterestRatePct < 0 || s.InterestRatePct > MaxInterestRatePct:
		return fmt.Errorf("the interest rate must be between 0 and %.0f per cent a year; the rate notified under section 50(3) is %.0f", MaxInterestRatePct, InterestRateAnnualPctDefault)
	case !s.InterestBasis.Valid():
		return fmt.Errorf("the interest basis must be %q or %q", InterestFromReversalDue, InterestFromAvailment)
	case s.PlanHorizonDays < 0:
		return fmt.Errorf("the planning horizon cannot be negative")
	}
	for _, e := range s.BucketEdges {
		if e <= 0 {
			return fmt.Errorf("an ageing boundary must be a positive number of days; %d is not", e)
		}
	}
	return nil
}

// Quarterly reports whether the business files FORM GSTR-3B quarterly.
func (s Settings) Quarterly() bool { return s.ReturnPeriodMonths == QuarterlyPeriodMonths }

// cleanEdges sorts the ageing boundaries, drops duplicates and anything
// non-positive, and falls back to the default when nothing usable is left.
func cleanEdges(edges, fallback []int) []int {
	out := make([]int, 0, len(edges))
	seen := map[int]bool{}
	for _, e := range edges {
		if e <= 0 || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	if len(out) == 0 {
		out = make([]int, len(fallback))
		copy(out, fallback)
		return out
	}
	sort.Ints(out)
	return out
}
