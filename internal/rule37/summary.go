package rule37

import (
	"fmt"
	"sort"
	"strings"
)

// Line is one invoice together with its assessment, which is what every view in
// this tool is built from.
type Line struct {
	Invoice    Invoice    `json:"invoice"`
	Assessment Assessment `json:"assessment"`
}

// AssessAll puts every invoice on the clock as at one date.
func AssessAll(invoices []Invoice, asOf Day, set Settings) []Line {
	set = set.Normalise()
	out := make([]Line, 0, len(invoices))
	for _, inv := range invoices {
		out = append(out, Line{Invoice: inv, Assessment: Assess(inv, asOf, set)})
	}
	return out
}

// PeriodPosition is what one return period has to carry: the reversals that
// fall in it, the re-claims that can be taken in it, and the net addition to
// the output tax liability of that return.
type PeriodPosition struct {
	Period  TaxPeriod `json:"period"`
	DueDate Day       `json:"dueDate"`

	// Overdue is true when the return for this period has already fallen due as
	// at the assessment date, so anything in it should already have been
	// reported.
	Overdue bool `json:"overdue"`

	// Reversal is the credit that must be reversed in this period's return
	// because the 180 days have already expired.
	Reversal Heads `json:"reversal"`
	// Projected is the credit that WOULD be reversed in this period's return if
	// nothing more were paid on invoices still inside their window.
	Projected Heads `json:"projected"`
	// Reclaim is the credit that may be taken again in this period's return
	// because the supplier was paid during it.
	Reclaim Heads `json:"reclaim"`

	ReversalInvoices  int `json:"reversalInvoices"`
	ProjectedInvoices int `json:"projectedInvoices"`
	ReclaimInvoices   int `json:"reclaimInvoices"`

	// CashToAvoid is what would have to reach suppliers before their deadlines
	// to stop the projected reversal happening at all.
	CashToAvoid int64 `json:"cashToAvoid"`
}

// NetAddition is the amount this return adds to the output tax liability:
// everything reversed in it, less everything taken again in it.
func (p PeriodPosition) NetAddition() int64 { return p.Reversal.Total() - p.Reclaim.Total() }

// SupplierTotal is one supplier's whole exposure, which is the unit a payment
// decision is actually made in: you pay a supplier, not an invoice.
type SupplierTotal struct {
	Name     string `json:"name"`
	GSTIN    string `json:"gstin"`
	Invoices int    `json:"invoices"`

	// Unpaid is what is still owed on the invoices that need action.
	Unpaid int64 `json:"unpaid"`
	// AtRisk is the credit still protectable by paying.
	AtRisk Heads `json:"atRisk"`
	// Reversal is the credit already lost to a missed deadline, recoverable
	// only by paying and taking it again in a later return.
	Reversal Heads `json:"reversal"`

	// NextDeadline is the first payment deadline still ahead.
	NextDeadline Day `json:"nextDeadline"`
	// DaysToNext is how long there is until it, negative when all the deadlines
	// have passed.
	DaysToNext int `json:"daysToNext"`

	// ProtectedPerRupee is paise of credit protected per rupee paid, on the
	// invoices still inside their window. It is what decides who to pay first
	// when there is not enough cash for everyone: a 28 per cent purchase
	// protects more than twice the credit of a 5 per cent one for the same
	// money.
	ProtectedPerRupee int64 `json:"protectedPerRupee"`
}

// Exposure is all the credit involved, at risk or already fallen due.
func (s SupplierTotal) Exposure() int64 { return s.AtRisk.Total() + s.Reversal.Total() }

// Summary is the whole position in the few figures that decide what to do. It
// is the money view: what is at risk, what reverses if nothing is paid, which
// return carries it, and whom to pay first.
type Summary struct {
	AsOf     Day      `json:"asOf"`
	Settings Settings `json:"settings"`

	Invoices int           `json:"invoices"`
	Counted  map[Stage]int `json:"counted"`

	// AtRisk is the headline: input tax credit on everything still unpaid whose
	// 180 days have not yet run out. Every rupee of it is protectable today and
	// lost to a reversal if the deadline passes.
	AtRisk         Heads `json:"atRisk"`
	AtRiskInvoices int   `json:"atRiskInvoices"`
	// CashToProtectAll is what would have to reach suppliers to protect all of
	// it.
	CashToProtectAll int64 `json:"cashToProtectAll"`

	// ThisPeriod is the return period the assessment date falls in, and
	// NextPeriod the one after it.
	ThisPeriod TaxPeriod `json:"thisPeriod"`
	NextPeriod TaxPeriod `json:"nextPeriod"`

	// RiskThisPeriod is the credit on invoices whose 180 days expire inside the
	// current return period: the deadlines that fall this period, and therefore
	// the reversals that land in the next return if nothing is paid.
	RiskThisPeriod         Heads `json:"riskThisPeriod"`
	RiskThisPeriodInvoices int   `json:"riskThisPeriodInvoices"`
	CashThisPeriod         int64 `json:"cashThisPeriod"`

	// ReversalDue is the credit that must already be reversed: the 180 days
	// have expired with an amount unpaid.
	ReversalDue         Heads `json:"reversalDue"`
	ReversalDueInvoices int   `json:"reversalDueInvoices"`
	// ReversalOverdue is the part of it whose return has already fallen due, so
	// it should have been reported already.
	ReversalOverdue Heads `json:"reversalOverdue"`

	// ReclaimAvailable is the credit that can be taken again because the
	// supplier has since been paid.
	ReclaimAvailable         Heads `json:"reclaimAvailable"`
	ReclaimAvailableInvoices int   `json:"reclaimAvailableInvoices"`

	// Interest is the indicative exposure where a reversal is itself late.
	Interest         int64 `json:"interest"`
	InterestInvoices int   `json:"interestInvoices"`

	// Periods is the return-by-return schedule, earliest first.
	Periods []PeriodPosition `json:"periods"`
	// Buckets is the ageing from the invoice date.
	Buckets []BucketTotal `json:"buckets"`
	// Suppliers is the exposure per supplier, most credit at risk first.
	Suppliers []SupplierTotal `json:"suppliers"`

	// Excluded and Unusable count the rows the rule does not reach and the rows
	// that could not be assessed, so nothing disappears quietly.
	Excluded int `json:"excluded"`
	Unusable int `json:"unusable"`

	// Flags gathers what the whole set needs looking at, each with a count.
	Flags []string `json:"flags,omitempty"`
}

// Totals rolls every assessed invoice up into the money view.
func Totals(lines []Line, asOf Day, set Settings) Summary {
	set = set.Normalise()
	s := Summary{
		AsOf:       asOf,
		Settings:   set,
		Invoices:   len(lines),
		Counted:    map[Stage]int{},
		ThisPeriod: PeriodOf(asOf, set.ReturnPeriodMonths),
	}
	s.NextPeriod = s.ThisPeriod.Next()

	positions := map[string]*PeriodPosition{}
	position := func(p TaxPeriod) *PeriodPosition {
		code := p.Code()
		if got, ok := positions[code]; ok {
			return got
		}
		due := p.DueDate(set.ReturnDueDay)
		pos := &PeriodPosition{Period: p, DueDate: due, Overdue: !asOf.IsZero() && asOf.After(due)}
		positions[code] = pos
		return pos
	}

	type supKey struct{ gstin, name string }
	sups := map[supKey]*SupplierTotal{}
	flagCount := map[string]int{}
	var assessments []Assessment

	for _, ln := range lines {
		a := ln.Assessment
		assessments = append(assessments, a)
		s.Counted[a.Stage]++
		for _, f := range a.Flags {
			flagCount[f]++
		}

		switch a.Stage {
		case StageExcluded:
			s.Excluded++
			continue
		case StageUnusable:
			s.Unusable++
			continue
		case StageSettled:
			continue
		}

		pos := position(a.ReversalPeriod)

		if a.Stage == StageWatch {
			s.AtRisk = s.AtRisk.Add(a.AtRisk)
			s.AtRiskInvoices++
			s.CashToProtectAll += a.UnpaidToDate
			pos.Projected = pos.Projected.Add(a.AtRisk)
			pos.ProjectedInvoices++
			pos.CashToAvoid += a.UnpaidToDate
			if s.ThisPeriod.Contains(a.Deadline) {
				s.RiskThisPeriod = s.RiskThisPeriod.Add(a.AtRisk)
				s.RiskThisPeriodInvoices++
				s.CashThisPeriod += a.UnpaidToDate
			}
		} else {
			s.ReversalDue = s.ReversalDue.Add(a.Reversal)
			s.ReversalDueInvoices++
			pos.Reversal = pos.Reversal.Add(a.Reversal)
			pos.ReversalInvoices++
			if pos.Overdue {
				s.ReversalOverdue = s.ReversalOverdue.Add(a.Reversal)
			}
			if a.Interest > 0 {
				s.Interest += a.Interest
				s.InterestInvoices++
			}
			if !a.Reclaim.IsZero() {
				s.ReclaimAvailable = s.ReclaimAvailable.Add(a.Reclaim)
				s.ReclaimAvailableInvoices++
				for _, rp := range a.ReclaimPeriods {
					rpos := position(rp.Period)
					rpos.Reclaim = rpos.Reclaim.Add(rp.Heads)
					rpos.ReclaimInvoices++
				}
			}
		}

		key := supKey{gstin: NormaliseGSTIN(ln.Invoice.SupplierGSTIN), name: strings.TrimSpace(ln.Invoice.SupplierName)}
		sup, ok := sups[key]
		if !ok {
			sup = &SupplierTotal{Name: key.name, GSTIN: key.gstin}
			if sup.Name == "" {
				sup.Name = key.gstin
			}
			sups[key] = sup
		}
		sup.Invoices++
		sup.Unpaid += a.UnpaidToDate
		sup.AtRisk = sup.AtRisk.Add(a.AtRisk)
		sup.Reversal = sup.Reversal.Add(a.Reversal)
		if a.Stage == StageWatch && !a.Deadline.IsZero() {
			if sup.NextDeadline.IsZero() || a.Deadline.Before(sup.NextDeadline) {
				sup.NextDeadline = a.Deadline
				sup.DaysToNext = a.DaysLeft
			}
		}
	}

	s.Buckets = Ageing(assessments, set.BucketEdges)

	s.Suppliers = make([]SupplierTotal, 0, len(sups))
	for _, sup := range sups {
		if sup.Unpaid > 0 {
			sup.ProtectedPerRupee = MulDivRound(sup.AtRisk.Total(), RupeePaise, sup.Unpaid)
		}
		s.Suppliers = append(s.Suppliers, *sup)
	}
	sort.SliceStable(s.Suppliers, func(i, j int) bool {
		a, b := s.Suppliers[i], s.Suppliers[j]
		if a.Exposure() != b.Exposure() {
			return a.Exposure() > b.Exposure()
		}
		if a.Unpaid != b.Unpaid {
			return a.Unpaid > b.Unpaid
		}
		return a.Name < b.Name
	})

	s.Periods = make([]PeriodPosition, 0, len(positions))
	for _, pos := range positions {
		s.Periods = append(s.Periods, *pos)
	}
	sort.SliceStable(s.Periods, func(i, j int) bool {
		return s.Periods[i].Period.Compare(s.Periods[j].Period) < 0
	})

	s.Flags = summariseFlags(flagCount)
	return s
}

// Exposure is every rupee of credit this set of invoices puts at stake: what is
// still protectable plus what has already fallen due for reversal.
func (s Summary) Exposure() int64 { return s.AtRisk.Total() + s.ReversalDue.Total() }

// NetThisReturn is what the reversals and re-claims already fallen due add to
// the output tax liability, net.
func (s Summary) NetThisReturn() int64 {
	return s.ReversalDue.Total() - s.ReclaimAvailable.Total()
}

// PositionFor returns the schedule entry for one period, and whether there is
// one.
func (s Summary) PositionFor(p TaxPeriod) (PeriodPosition, bool) {
	for _, pos := range s.Periods {
		if pos.Period.Compare(p) == 0 {
			return pos, true
		}
	}
	return PeriodPosition{}, false
}

// summariseFlags turns the per-invoice flags into one line each, with the
// number of invoices affected, in a stable order.
func summariseFlags(counts map[string]int) []string {
	if len(counts) == 0 {
		return nil
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if n := counts[k]; n > 1 {
			out = append(out, fmt.Sprintf("%d invoices: %s", n, k))
		} else {
			out = append(out, k)
		}
	}
	return out
}
