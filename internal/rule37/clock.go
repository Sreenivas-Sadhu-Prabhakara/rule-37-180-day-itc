package rule37

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Stage is what an invoice needs doing about, as at the date it is assessed on.
type Stage string

const (
	// StageUnusable is a row that cannot be put on the clock at all, because
	// the invoice date it is counted from is missing.
	StageUnusable Stage = "unusable"

	// StageExcluded is an invoice the 180-day condition does not reach.
	StageExcluded Stage = "excluded"

	// StageSettled is an invoice paid in full within its 180 days. Nothing to
	// reverse, now or later.
	StageSettled Stage = "settled"

	// StageWatch is an invoice still inside its 180 days with something left
	// unpaid. The credit on the unpaid part is at risk and paying protects it.
	StageWatch Stage = "watch"

	// StageReverse is an invoice whose 180 days expired with an amount unpaid.
	// The credit on that amount must be reversed, and still is not fully paid.
	StageReverse Stage = "reverse"

	// StageReclaim is an invoice whose 180 days expired with an amount unpaid
	// which has since been paid in full. The reversal must still be reported,
	// and the whole of it can be taken again.
	StageReclaim Stage = "reclaim"
)

// Label is the stage in a few words, for a column heading or a filter.
func (s Stage) Label() string {
	switch s {
	case StageUnusable:
		return "Cannot be assessed"
	case StageExcluded:
		return "Out of scope"
	case StageSettled:
		return "Paid in time"
	case StageWatch:
		return "Credit at risk — pay by the deadline"
	case StageReverse:
		return "Reverse, and still to pay"
	case StageReclaim:
		return "Reverse and re-claim"
	default:
		return string(s)
	}
}

// PeriodHeads is an amount of tax attributed to one return period.
type PeriodHeads struct {
	Period  TaxPeriod `json:"period"`
	DueDate Day       `json:"dueDate"`
	Heads   Heads     `json:"heads"`
}

// Assessment is the whole position of one invoice under rule 37, as at a date.
// It is a value: nothing in it is computed lazily and nothing is cached, so two
// assessments of the same invoice on the same date are identical.
type Assessment struct {
	Stage     Stage     `json:"stage"`
	Reason    string    `json:"reason"`
	Exclusion Exclusion `json:"exclusion,omitempty"`

	// The clock.
	InvoiceDate Day `json:"invoiceDate"`
	Deadline    Day `json:"deadline"`
	WindowDays  int `json:"windowDays"`
	DaysElapsed int `json:"daysElapsed"`
	DaysLeft    int `json:"daysLeft"`

	// The money on the invoice.
	Payable          int64 `json:"payable"`
	ITC              Heads `json:"itc"`
	PaidByDeadline   int64 `json:"paidByDeadline"`
	UnpaidAtDeadline int64 `json:"unpaidAtDeadline"`
	PaidToDate       int64 `json:"paidToDate"`
	UnpaidToDate     int64 `json:"unpaidToDate"`
	PaidAfterDue     int64 `json:"paidAfterDeadline"`

	// AtRisk is the credit on the amount still unpaid, for an invoice whose
	// deadline has NOT yet passed: pay and it is safe, do not and it reverses.
	AtRisk Heads `json:"atRisk"`

	// Reversal is the credit that must be reversed because the deadline passed
	// with an amount unpaid — in proportion to that amount, as rule 37(1)
	// requires. Zero while the deadline is still ahead.
	Reversal Heads `json:"reversal"`

	// ReversalRupees is the same figure rounded to whole rupees, which is how
	// FORM GSTR-3B is filled in.
	ReversalRupees Heads `json:"reversalRupees"`

	// ReversalPeriod is the return period the reversal belongs in: the tax
	// period immediately following the one the 180 days expire in. For an
	// invoice still inside its window this is where the reversal WOULD land.
	ReversalPeriod TaxPeriod `json:"reversalPeriod"`
	ReversalDue    Day       `json:"reversalDue"`

	// Reclaim is the credit that may be taken again because the supplier has
	// since been paid, capped at what was reversed.
	Reclaim        Heads         `json:"reclaim"`
	ReclaimRupees  Heads         `json:"reclaimRupees"`
	ReclaimPeriods []PeriodHeads `json:"reclaimPeriods,omitempty"`

	// Indicative interest exposure where the reversal itself is late.
	LateDays        int           `json:"lateDays"`
	Interest        int64         `json:"interest"`
	InterestBasis   InterestBasis `json:"interestBasis,omitempty"`
	InterestFromDay Day           `json:"interestFrom"`

	Bucket Bucket   `json:"bucket"`
	Flags  []string `json:"flags,omitempty"`
}

// NetAddition is what the reversal and the re-claim come to together: what the
// invoice adds to the output tax liability of the return period it lands in,
// before any re-claim in a later period.
func (a Assessment) NetAddition() int64 { return a.Reversal.Total() - a.Reclaim.Total() }

// NeedsAction reports whether the invoice calls for something to be done.
func (a Assessment) NeedsAction() bool {
	switch a.Stage {
	case StageWatch, StageReverse, StageReclaim:
		return true
	}
	return false
}

// Assess puts one invoice on the 180-day clock and works out everything that
// follows: the deadline, what is unpaid, what must be reversed and in which
// return, and what can be taken again.
//
// It never panics and never returns an error: a row it cannot use comes back as
// StageUnusable with the reason, because losing a row silently is worse than
// reporting it.
func Assess(inv Invoice, asOf Day, set Settings) Assessment {
	set = set.Normalise()
	a := Assessment{
		InvoiceDate:   inv.InvoiceDate,
		WindowDays:    set.PaymentWindowDays,
		ITC:           inv.ITC,
		Payable:       inv.Payable(),
		InterestBasis: set.InterestBasis,
	}

	if inv.InvoiceDate.IsZero() {
		a.Stage = StageUnusable
		a.Reason = "no invoice date, so the 180 days cannot be counted"
		a.Flags = append(a.Flags, "The invoice date is missing. The 180 days run from the date the supplier issued the invoice, so nothing can be worked out without it.")
		return a
	}
	if asOf.IsZero() {
		asOf = inv.InvoiceDate
	}

	a.Deadline = inv.InvoiceDate.AddDays(set.PaymentWindowDays)
	a.DaysElapsed = inv.InvoiceDate.DaysUntil(asOf)
	a.DaysLeft = asOf.DaysUntil(a.Deadline)
	a.Bucket = BucketFor(a.DaysElapsed, set.BucketEdges)
	a.ReversalPeriod = PeriodOf(a.Deadline, set.ReturnPeriodMonths)
	for i := 0; i < ReversalReturnOffsetPeriods; i++ {
		a.ReversalPeriod = a.ReversalPeriod.Next()
	}
	a.ReversalDue = a.ReversalPeriod.DueDate(set.ReturnDueDay)

	// Payments. An undated payment counts from the date the assessment is run
	// on, which is all such a figure states: it is paid by now, and whether it
	// was in time is unknown.
	a.PaidByDeadline = min64(inv.PaidBy(a.Deadline, asOf), a.Payable)
	a.PaidToDate = min64(inv.PaidBy(asOf, asOf), a.Payable)
	if a.Payable > 0 {
		a.UnpaidAtDeadline = max64(a.Payable-a.PaidByDeadline, 0)
		a.UnpaidToDate = max64(a.Payable-a.PaidToDate, 0)
	}
	a.PaidAfterDue = max64(a.PaidToDate-a.PaidByDeadline, 0)

	a.Flags = append(a.Flags, flagsFor(inv, asOf)...)

	// Out of scope: the four reasons the law gives, and the case where no
	// credit was taken, which rule 37 cannot reach because it reverses credit
	// availed.
	switch {
	case inv.Exclusion != ExclusionNone:
		a.Stage, a.Exclusion = StageExcluded, inv.Exclusion
		a.Reason = inv.Exclusion.Label()
		return a
	case inv.ITC.Total() == 0:
		a.Stage, a.Exclusion = StageExcluded, ExclusionNoCredit
		a.Reason = ExclusionNoCredit.Label()
		return a
	case a.Payable <= 0:
		a.Stage = StageUnusable
		a.Reason = "the invoice has no value to pay, so there is no proportion to reverse"
		a.Flags = append(a.Flags, "This invoice carries input tax credit but no amount payable. Enter the taxable value, or the invoice total including tax.")
		return a
	}

	if a.DaysLeft >= 0 {
		// Still inside the window. Everything unpaid is credit at risk, and the
		// reversal period says which return it would land in.
		if a.UnpaidToDate == 0 {
			a.Stage = StageSettled
			a.Reason = fmt.Sprintf("paid in full with %s to spare", plural(a.DaysLeft, "day", "days"))
			return a
		}
		a.Stage = StageWatch
		a.AtRisk = inv.ITC.Scale(a.UnpaidToDate, a.Payable)
		a.Reason = fmt.Sprintf("%s left to pay %s, or %s of credit reverses in the %s return",
			plural(a.DaysLeft, "day", "days"), Rupees(a.UnpaidToDate), Rupees(a.AtRisk.Total()), a.ReversalPeriod)
		return a
	}

	// The 180 days have expired.
	if a.UnpaidAtDeadline == 0 {
		a.Stage = StageSettled
		a.Reason = "paid in full within the 180 days"
		return a
	}

	a.Reversal = inv.ITC.Scale(a.UnpaidAtDeadline, a.Payable)
	a.ReversalRupees = a.Reversal.RoundToRupee()

	if a.PaidAfterDue > 0 {
		a.Reclaim = inv.ITC.Scale(a.PaidAfterDue, a.Payable).Min(a.Reversal)
		a.ReclaimRupees = a.Reclaim.RoundToRupee()
		a.ReclaimPeriods = reclaimByPeriod(inv, a, asOf, set)
	}

	if a.UnpaidToDate == 0 {
		a.Stage = StageReclaim
		a.Reason = fmt.Sprintf("the 180 days ran out on %s with %s unpaid, since paid — reverse %s in the %s return and take it again",
			a.Deadline.Pretty(), Rupees(a.UnpaidAtDeadline), Rupees(a.Reversal.Total()), a.ReversalPeriod)
	} else {
		a.Stage = StageReverse
		a.Reason = fmt.Sprintf("the 180 days ran out %s ago with %s unpaid — reverse %s in the %s return",
			plural(-a.DaysLeft, "day", "days"), Rupees(a.UnpaidAtDeadline), Rupees(a.Reversal.Total()), a.ReversalPeriod)
	}

	a.LateDays, a.InterestFromDay = lateness(a, asOf, set)
	a.Interest = InterestOn(a.Reversal.Total(), set.InterestRatePct, a.LateDays)
	return a
}

// lateness works out whether the reversal itself is late, and from when
// interest would run if it is.
//
// Nothing is late until the return the reversal belongs in has fallen due. Once
// it has, the period depends on the basis the user chose: see InterestBasis.
func lateness(a Assessment, asOf Day, set Settings) (int, Day) {
	if a.ReversalDue.IsZero() || !asOf.After(a.ReversalDue) {
		return 0, Day{}
	}
	from := a.ReversalDue
	if set.InterestBasis == InterestFromAvailment {
		availed := PeriodOf(a.InvoiceDate, set.ReturnPeriodMonths).DueDate(set.ReturnDueDay)
		if !availed.IsZero() && availed.Before(from) {
			from = availed
		}
	}
	days := from.DaysUntil(asOf)
	if days < 0 {
		days = 0
	}
	return days, from
}

// InterestOn returns simple interest on a principal in paise at an annual
// percentage rate for a whole number of days, rounded to the nearest paisa.
//
// The computation is exact: the rate is taken to two decimal places as basis
// points, so no float ever touches the money. The year is DaysInYearForInterest
// days long, which is what rule 88B's day count implies.
func InterestOn(principal int64, ratePct float64, days int) int64 {
	if principal <= 0 || days <= 0 || ratePct <= 0 || math.IsNaN(ratePct) || math.IsInf(ratePct, 0) {
		return 0
	}
	bp := int64(math.Round(ratePct * 100)) // 18.00% -> 1800 basis points
	if bp <= 0 {
		return 0
	}
	return MulDivRound(principal, bp*int64(days), 10_000*int64(DaysInYearForInterest))
}

// reclaimByPeriod splits the re-claim across the return periods the payments
// were made in, because rule 37(2) entitles the recipient to take the credit
// again when the payment is made, and the return for that period is where it
// goes.
//
// A re-claim is never put in a return EARLIER than the one carrying its
// reversal, even where the payment falls in one. That happens to a quarterly
// filer who pays soon after day 180: the payment can fall in the quarter the
// 180 days expired in, while the reversal belongs in the quarter after. There
// would then be nothing yet reversed to take back, and the portal's reversal
// statement rejects a re-claim that exceeds the balance against it. Such a
// payment is carried forward to the reversal's own return, where the two
// cancel.
//
// The last period absorbs any rounding difference, so the periods always add up
// to exactly the re-claim: a paisa may move between returns but none is created
// and none is lost.
func reclaimByPeriod(inv Invoice, a Assessment, asOf Day, set Settings) []PeriodHeads {
	byCode := map[string]int64{}
	periods := map[string]TaxPeriod{}
	var order []string
	for _, p := range inv.Payments {
		if p.Amount <= 0 {
			continue
		}
		eff := p.Date
		if eff.IsZero() {
			eff = asOf
		}
		if !eff.After(a.Deadline) || eff.After(asOf) {
			continue // paid in time, or not yet paid as at the assessment date
		}
		per := PeriodOf(eff, set.ReturnPeriodMonths)
		if !a.ReversalPeriod.IsZero() && per.Compare(a.ReversalPeriod) < 0 {
			per = a.ReversalPeriod
		}
		code := per.Code()
		if _, seen := byCode[code]; !seen {
			order = append(order, code)
			periods[code] = per
		}
		byCode[code] += p.Amount
	}
	if len(order) == 0 {
		return nil
	}
	sort.Strings(order)

	out := make([]PeriodHeads, 0, len(order))
	running := Heads{}
	for i, code := range order {
		per := periods[code]
		h := a.Reclaim.Scale(byCode[code], a.Payable)
		if i == len(order)-1 {
			h = a.Reclaim.Sub(running) // the residual, so the parts add up exactly
		}
		running = running.Add(h)
		out = append(out, PeriodHeads{Period: per, DueDate: per.DueDate(set.ReturnDueDay), Heads: h})
	}
	return out
}

// flagsFor reports what looks wrong or incomplete about an invoice without
// refusing it. Each flag is a sentence the user can act on.
func flagsFor(inv Invoice, asOf Day) []string {
	var out []string
	if inv.HasUndatedPayment() {
		out = append(out, "An amount is recorded as paid but not the date it was paid on. It is counted as paid by today, so the 180-day test treats it as late. Add the payment date if it was made in time.")
	}
	if paid := inv.PaidTotal(); paid > inv.Payable() && inv.Payable() > 0 {
		out = append(out, fmt.Sprintf("More has been recorded as paid (%s) than the invoice is for (%s). The excess is ignored.", Rupees(paid), Rupees(inv.Payable())))
	}
	if inv.TaxableValue == 0 && inv.InvoiceValue == 0 && inv.ITC.Total() > 0 {
		out = append(out, "This invoice has no taxable value and no invoice total, so the amount payable has been taken as the tax alone and every proportion computed from it is wrong. Enter the value of the supply.")
	}
	if inv.InvoiceValue > 0 && inv.InvoiceValue < inv.TaxableValue+inv.ITC.Total() {
		out = append(out, fmt.Sprintf("The invoice total (%s) is less than the taxable value plus the credit heads (%s). The condition is satisfied only by paying the value of the supply together with the tax on it, so check which figure is right.",
			Rupees(inv.InvoiceValue), Rupees(inv.TaxableValue+inv.ITC.Total())))
	}
	if !inv.InvoiceDate.IsZero() && inv.InvoiceDate.After(asOf) {
		out = append(out, "The invoice is dated in the future, so its 180 days have not started.")
	}
	if strings.TrimSpace(inv.SupplierName) == "" && strings.TrimSpace(inv.SupplierGSTIN) == "" {
		out = append(out, "No supplier is recorded against this invoice, so the tool cannot tell you whom to pay to protect the credit on it.")
	}
	if g := strings.TrimSpace(inv.SupplierGSTIN); g != "" && !ValidGSTINFormat(g) {
		out = append(out, fmt.Sprintf("The supplier GSTIN %q is not in the standard 15-character form. It does not affect the arithmetic, but check it.", g))
	}
	for _, p := range inv.Payments {
		if !p.Date.IsZero() && !inv.InvoiceDate.IsZero() && p.Date.Before(inv.InvoiceDate) {
			out = append(out, fmt.Sprintf("A payment is dated %s, before the invoice itself (%s). It still counts, but check the dates.", p.Date.Indian(), inv.InvoiceDate.Indian()))
			break
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 || n == -1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
