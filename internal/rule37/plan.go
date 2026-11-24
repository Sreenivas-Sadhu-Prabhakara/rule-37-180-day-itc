package rule37

import "sort"

// NoBudgetLimit, or any negative budget, plans every payment that protects
// credit without limiting the cash.
const NoBudgetLimit int64 = -1

// PayAction is one payment worth making, and what it buys.
type PayAction struct {
	InvoiceID     string `json:"invoiceId"`
	SupplierName  string `json:"supplierName"`
	SupplierGSTIN string `json:"supplierGstin"`
	InvoiceNo     string `json:"invoiceNo"`
	InvoiceDate   Day    `json:"invoiceDate"`

	Deadline Day `json:"deadline"`
	DaysLeft int `json:"daysLeft"`

	// Unpaid is what is still owed on the invoice; Pay is what this plan
	// recommends paying out of the budget, which is less only when the budget
	// runs out part way through an invoice.
	Unpaid int64 `json:"unpaid"`
	Pay    int64 `json:"pay"`

	// AtStake is the credit on the whole unpaid amount; Protected is the part
	// of it that paying Pay protects.
	AtStake   Heads `json:"atStake"`
	Protected Heads `json:"protected"`

	// ProtectedPerRupee is paise of credit protected per rupee paid, which is
	// the order this plan is in.
	ProtectedPerRupee int64 `json:"protectedPerRupee"`

	// Partial is true when the budget covered only part of the invoice. A part
	// payment still protects its own proportion of the credit, which is what
	// makes paying what you can worth doing.
	Partial bool `json:"partial"`
}

// Plan is the answer to "there is this much cash — whom do I pay?".
type Plan struct {
	Budget  int64 `json:"budget"`
	Limited bool  `json:"limited"`

	// Horizon is the last deadline the plan looks at. Invoices falling due
	// after it are left out: they can be dealt with out of next month's cash.
	Horizon     Day `json:"horizon"`
	HorizonDays int `json:"horizonDays"`

	Actions []PayAction `json:"actions"`

	CashNeeded   int64 `json:"cashNeeded"`   // to protect everything in the horizon
	CashUsed     int64 `json:"cashUsed"`     // what the budget actually spends
	Protected    Heads `json:"protected"`    // credit the plan protects
	AtStake      Heads `json:"atStake"`      // credit in the horizon altogether
	StillAtRisk  Heads `json:"stillAtRisk"`  // credit the budget cannot reach
	Invoices     int   `json:"invoices"`     // invoices in the horizon
	PaidInvoices int   `json:"paidInvoices"` // invoices the plan pays, whole or part
}

// Shortfall is the cash still needed after the budget is spent.
func (p Plan) Shortfall() int64 { return max64(p.CashNeeded-p.CashUsed, 0) }

// PayFirst ranks the payments that protect the most input tax credit for the
// cash available.
//
// Only invoices still inside their 180 days are worth paying for this purpose:
// once the deadline has passed the credit must be reversed whatever happens,
// and paying then buys a re-claim in a later return rather than protection now.
//
// The order is by credit protected per rupee paid, highest first. That is not a
// heuristic but the exact answer: rule 37(1) reverses credit "proportionate to
// the amount not paid", so a part payment protects exactly its own proportion
// of the credit, which makes this a divisible allocation problem and ranking by
// the ratio provably optimal. Where two invoices protect the same credit per
// rupee — which is what happens when both are taxed at the same rate — the
// earlier deadline is paid first, because it is the one about to be lost.
//
// A negative budget means no limit, and the plan then lists every payment worth
// making.
func PayFirst(lines []Line, asOf Day, set Settings, budget int64) Plan {
	set = set.Normalise()
	limited := budget >= 0
	p := Plan{Budget: budget, Limited: limited, HorizonDays: set.PlanHorizonDays}
	if !limited {
		p.Budget = 0
	}
	if set.PlanHorizonDays > 0 && !asOf.IsZero() {
		p.Horizon = asOf.AddDays(set.PlanHorizonDays)
	}

	type candidate struct {
		action PayAction
		rate   int64 // paise of credit per rupee paid, for the reported figure
	}
	var cands []candidate
	for _, ln := range lines {
		a := ln.Assessment
		if a.Stage != StageWatch || a.UnpaidToDate <= 0 || a.AtRisk.Total() <= 0 {
			continue
		}
		if !p.Horizon.IsZero() && a.Deadline.After(p.Horizon) {
			continue
		}
		act := PayAction{
			InvoiceID:         ln.Invoice.ID,
			SupplierName:      ln.Invoice.SupplierName,
			SupplierGSTIN:     ln.Invoice.SupplierGSTIN,
			InvoiceNo:         ln.Invoice.InvoiceNo,
			InvoiceDate:       ln.Invoice.InvoiceDate,
			Deadline:          a.Deadline,
			DaysLeft:          a.DaysLeft,
			Unpaid:            a.UnpaidToDate,
			AtStake:           a.AtRisk,
			ProtectedPerRupee: MulDivRound(a.AtRisk.Total(), RupeePaise, a.UnpaidToDate),
		}
		cands = append(cands, candidate{action: act, rate: act.ProtectedPerRupee})
	}

	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i].action, cands[j].action
		// Exact ratio comparison: credit at stake per rupee unpaid.
		if c := CmpRatio(a.AtStake.Total(), a.Unpaid, b.AtStake.Total(), b.Unpaid); c != 0 {
			return c > 0
		}
		if !a.Deadline.Equal(b.Deadline) {
			return a.Deadline.Before(b.Deadline)
		}
		if a.AtStake.Total() != b.AtStake.Total() {
			return a.AtStake.Total() > b.AtStake.Total()
		}
		if a.SupplierName != b.SupplierName {
			return a.SupplierName < b.SupplierName
		}
		return a.InvoiceNo < b.InvoiceNo
	})

	left := budget
	for _, c := range cands {
		act := c.action
		p.Invoices++
		p.CashNeeded += act.Unpaid
		p.AtStake = p.AtStake.Add(act.AtStake)

		switch {
		case !limited || left >= act.Unpaid:
			act.Pay = act.Unpaid
			act.Protected = act.AtStake
		case left > 0:
			act.Pay = left
			act.Partial = true
			act.Protected = act.AtStake.Scale(left, act.Unpaid)
		default:
			act.Pay = 0
		}
		if limited {
			left -= act.Pay
			if left < 0 {
				left = 0
			}
		}
		if act.Pay > 0 {
			p.PaidInvoices++
			p.CashUsed += act.Pay
			p.Protected = p.Protected.Add(act.Protected)
		}
		p.Actions = append(p.Actions, act)
	}
	p.StillAtRisk = p.AtStake.Sub(p.Protected)
	return p
}
