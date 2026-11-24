package rule37

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Business is who the register belongs to. It appears on the working paper so
// the paper can be filed with the return it supports.
type Business struct {
	Name  string `json:"name"`
	GSTIN string `json:"gstin"`
}

// View names one of the lists this tool can hand back as a CSV.
type View string

const (
	// ViewWatchlist is every invoice still inside its 180 days with something
	// unpaid: the credit that paying protects, soonest deadline first.
	ViewWatchlist View = "watchlist"
	// ViewReversals is every invoice whose 180 days expired with an amount
	// unpaid, with the reversal per head and the return it belongs in.
	ViewReversals View = "reversals"
	// ViewReclaims is every reversal that can now be taken again because the
	// supplier has been paid.
	ViewReclaims View = "reclaims"
	// ViewPlan is the payment plan: whom to pay, in what order, for how much.
	ViewPlan View = "plan"
	// ViewAll is every invoice with its stage, for the record.
	ViewAll View = "all"
)

// Views lists the exports in the order a user meets them.
var Views = []View{ViewWatchlist, ViewReversals, ViewReclaims, ViewPlan, ViewAll}

// Title names the view for a heading or a file name.
func (v View) Title() string {
	switch v {
	case ViewWatchlist:
		return "Credit at risk — pay before the deadline"
	case ViewReversals:
		return "Reversals required"
	case ViewReclaims:
		return "Re-claims available"
	case ViewPlan:
		return "Payment plan"
	case ViewAll:
		return "Every invoice"
	}
	return string(v)
}

// Valid reports whether v is a view this package can build.
func (v View) Valid() bool {
	for _, k := range Views {
		if k == v {
			return true
		}
	}
	return false
}

// CSV renders one view as a CSV with plain decimal amounts, ready for a
// spreadsheet or an accounting package. Amounts are written without a currency
// mark or digit grouping so they read back as numbers.
func CSV(v View, lines []Line, plan Plan, asOf Day, set Settings) ([]byte, error) {
	if !v.Valid() {
		return nil, fmt.Errorf("%q is not a list this tool can export", v)
	}
	set = set.Normalise()
	rows := rowsFor(v, lines, plan, set)

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	for _, r := range rows {
		if err := w.Write(r); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func rowsFor(v View, lines []Line, plan Plan, set Settings) [][]string {
	switch v {
	case ViewWatchlist:
		return watchlistRows(lines)
	case ViewReversals:
		return reversalRows(lines)
	case ViewReclaims:
		return reclaimRows(lines)
	case ViewPlan:
		return planRows(plan)
	default:
		return allRows(lines)
	}
}

func watchlistRows(lines []Line) [][]string {
	out := [][]string{{
		"Supplier", "Supplier GSTIN", "Invoice no", "Invoice date", "Invoice total",
		"Paid", "Unpaid", "Pay by", "Days left", "Days since invoice",
		"ITC at risk IGST", "ITC at risk CGST", "ITC at risk SGST", "ITC at risk cess", "ITC at risk total",
		"Reverses in return", "Return due",
	}}
	sel := selectLines(lines, func(a Assessment) bool { return a.Stage == StageWatch })
	sort.SliceStable(sel, func(i, j int) bool {
		return sel[i].Assessment.Deadline.Before(sel[j].Assessment.Deadline)
	})
	for _, ln := range sel {
		a := ln.Assessment
		out = append(out, []string{
			ln.Invoice.SupplierName, ln.Invoice.SupplierGSTIN, ln.Invoice.InvoiceNo, ln.Invoice.InvoiceDate.Indian(),
			Decimal(a.Payable), Decimal(a.PaidToDate), Decimal(a.UnpaidToDate),
			a.Deadline.Indian(), strconv.Itoa(a.DaysLeft), strconv.Itoa(a.DaysElapsed),
			Decimal(a.AtRisk.IGST), Decimal(a.AtRisk.CGST), Decimal(a.AtRisk.SGST), Decimal(a.AtRisk.Cess), Decimal(a.AtRisk.Total()),
			a.ReversalPeriod.String(), a.ReversalDue.Indian(),
		})
	}
	return out
}

func reversalRows(lines []Line) [][]string {
	out := [][]string{{
		"Supplier", "Supplier GSTIN", "Invoice no", "Invoice date", "Invoice total",
		"Paid by day 180", "Unpaid on day 180", "Deadline", "Days past deadline",
		"Reverse IGST", "Reverse CGST", "Reverse SGST", "Reverse cess", "Reverse total",
		"Reverse in return", "Return due", "Re-claim total", "Still unpaid", "Indicative interest",
	}}
	sel := selectLines(lines, func(a Assessment) bool {
		return a.Stage == StageReverse || a.Stage == StageReclaim
	})
	sort.SliceStable(sel, func(i, j int) bool {
		return sel[i].Assessment.Deadline.Before(sel[j].Assessment.Deadline)
	})
	for _, ln := range sel {
		a := ln.Assessment
		r := a.ReversalRupees
		out = append(out, []string{
			ln.Invoice.SupplierName, ln.Invoice.SupplierGSTIN, ln.Invoice.InvoiceNo, ln.Invoice.InvoiceDate.Indian(),
			Decimal(a.Payable), Decimal(a.PaidByDeadline), Decimal(a.UnpaidAtDeadline),
			a.Deadline.Indian(), strconv.Itoa(-a.DaysLeft),
			Decimal(r.IGST), Decimal(r.CGST), Decimal(r.SGST), Decimal(r.Cess), Decimal(r.Total()),
			a.ReversalPeriod.String(), a.ReversalDue.Indian(),
			Decimal(a.Reclaim.Total()), Decimal(a.UnpaidToDate), Decimal(a.Interest),
		})
	}
	return out
}

func reclaimRows(lines []Line) [][]string {
	out := [][]string{{
		"Supplier", "Supplier GSTIN", "Invoice no", "Invoice date", "Paid after deadline",
		"Re-claim IGST", "Re-claim CGST", "Re-claim SGST", "Re-claim cess", "Re-claim total",
		"Claim in return", "Return due",
	}}
	sel := selectLines(lines, func(a Assessment) bool { return !a.Reclaim.IsZero() })
	sort.SliceStable(sel, func(i, j int) bool {
		return sel[i].Assessment.Deadline.Before(sel[j].Assessment.Deadline)
	})
	for _, ln := range sel {
		a := ln.Assessment
		for _, rp := range a.ReclaimPeriods {
			h := rp.Heads.RoundToRupee()
			out = append(out, []string{
				ln.Invoice.SupplierName, ln.Invoice.SupplierGSTIN, ln.Invoice.InvoiceNo, ln.Invoice.InvoiceDate.Indian(),
				Decimal(a.PaidAfterDue),
				Decimal(h.IGST), Decimal(h.CGST), Decimal(h.SGST), Decimal(h.Cess), Decimal(h.Total()),
				rp.Period.String(), rp.DueDate.Indian(),
			})
		}
	}
	return out
}

func planRows(plan Plan) [][]string {
	out := [][]string{{
		"Order", "Supplier", "Supplier GSTIN", "Invoice no", "Pay by", "Days left",
		"Unpaid", "Pay now", "Part payment", "Credit protected", "Credit at stake", "Paise protected per rupee",
	}}
	for i, act := range plan.Actions {
		part := "no"
		if act.Partial {
			part = "yes"
		}
		out = append(out, []string{
			strconv.Itoa(i + 1), act.SupplierName, act.SupplierGSTIN, act.InvoiceNo,
			act.Deadline.Indian(), strconv.Itoa(act.DaysLeft),
			Decimal(act.Unpaid), Decimal(act.Pay), part,
			Decimal(act.Protected.Total()), Decimal(act.AtStake.Total()), strconv.FormatInt(act.ProtectedPerRupee, 10),
		})
	}
	return out
}

func allRows(lines []Line) [][]string {
	out := [][]string{{
		"Stage", "Supplier", "Supplier GSTIN", "Invoice no", "Invoice date", "Taxable value",
		"IGST", "CGST", "SGST", "Cess", "Invoice total", "Paid to date", "Unpaid",
		"Pay by", "Days left", "ITC at risk", "Reversal", "Re-claim", "Reverse in return", "Out of scope", "Position",
	}}
	for _, ln := range lines {
		a := ln.Assessment
		out = append(out, []string{
			string(a.Stage), ln.Invoice.SupplierName, ln.Invoice.SupplierGSTIN, ln.Invoice.InvoiceNo, ln.Invoice.InvoiceDate.Indian(),
			Decimal(ln.Invoice.TaxableValue),
			Decimal(ln.Invoice.ITC.IGST), Decimal(ln.Invoice.ITC.CGST), Decimal(ln.Invoice.ITC.SGST), Decimal(ln.Invoice.ITC.Cess),
			Decimal(a.Payable), Decimal(a.PaidToDate), Decimal(a.UnpaidToDate),
			a.Deadline.Indian(), strconv.Itoa(a.DaysLeft),
			Decimal(a.AtRisk.Total()), Decimal(a.Reversal.Total()), Decimal(a.Reclaim.Total()),
			a.ReversalPeriod.String(), string(a.Exclusion), a.Reason,
		})
	}
	return out
}

func selectLines(lines []Line, keep func(Assessment) bool) []Line {
	out := make([]Line, 0, len(lines))
	for _, ln := range lines {
		if keep(ln.Assessment) {
			out = append(out, ln)
		}
	}
	return out
}

// WorkingPaper writes the paper that supports one return: what is being
// reversed under rule 37 in it, what is being taken again, which table of FORM
// GSTR-3B each figure goes in, and the invoices behind every number.
//
// It is a working paper, not a return. Nothing is filed, nothing is sent: the
// text is for printing, filing with the period's papers, and handing to whoever
// prepares the return.
func WorkingPaper(biz Business, period TaxPeriod, lines []Line, asOf Day, set Settings) string {
	set = set.Normalise()
	var b strings.Builder
	line := func(f string, args ...any) { fmt.Fprintf(&b, f+"\n", args...) }

	line("INPUT TAX CREDIT REVERSED AND RE-CLAIMED UNDER RULE 37")
	line("Working paper for FORM GSTR-3B — %s", period)
	line("")
	if n := strings.TrimSpace(biz.Name); n != "" {
		line("Registered person   : %s", n)
	}
	if g := strings.TrimSpace(biz.GSTIN); g != "" {
		line("GSTIN               : %s", NormaliseGSTIN(g))
	}
	filer := "monthly"
	if set.Quarterly() {
		filer = "quarterly, under the QRMP scheme"
	}
	line("Return period       : %s  (%s filer)", period, filer)
	line("Return due on       : %s", period.DueDate(set.ReturnDueDay).Pretty())
	line("Prepared as at      : %s", asOf.Pretty())
	line("Payment window      : %d days from the date of issue of the invoice", set.PaymentWindowDays)
	line("")
	line("Authority: second proviso to s.16(2) of the Central Goods and Services Tax Act,")
	line("2017, and rule 37 of the Central Goods and Services Tax Rules, 2017, as")
	line("substituted with effect from 1 October 2022.")
	line("")

	var reversals, reclaims []Line
	for _, ln := range lines {
		a := ln.Assessment
		if !a.Reversal.IsZero() && a.ReversalPeriod.Compare(period) == 0 {
			reversals = append(reversals, ln)
		}
		for _, rp := range a.ReclaimPeriods {
			if rp.Period.Compare(period) == 0 {
				reclaims = append(reclaims, ln)
				break
			}
		}
	}
	sort.SliceStable(reversals, func(i, j int) bool {
		return reversals[i].Assessment.Deadline.Before(reversals[j].Assessment.Deadline)
	})
	sort.SliceStable(reclaims, func(i, j int) bool {
		return reclaims[i].Assessment.Deadline.Before(reclaims[j].Assessment.Deadline)
	})

	revTotal := Heads{}
	line("1. REVERSAL — FORM GSTR-3B TABLE %s", TableReversal)
	line("   \"Others\": a reversal that may be reclaimed when the supplier is paid.")
	line("   Reported in any other table it becomes permanent and cannot be taken back.")
	line("")
	if len(reversals) == 0 {
		line("   Nothing to reverse in this return.")
	} else {
		line("   %-18s %-11s %-11s %12s %12s %12s %12s %12s", "Invoice", "Date", "Day 180", "Unpaid", "IGST", "CGST", "SGST", "Cess")
		line("   %s", strings.Repeat("-", 103))
		for _, ln := range reversals {
			a := ln.Assessment
			h := a.ReversalRupees
			revTotal = revTotal.Add(h)
			line("   %-18s %-11s %-11s %12s %12s %12s %12s %12s",
				trunc(ln.Invoice.InvoiceNo, 18), ln.Invoice.InvoiceDate.Indian(), a.Deadline.Indian(),
				Rupees(a.UnpaidAtDeadline), Rupees(h.IGST), Rupees(h.CGST), Rupees(h.SGST), Rupees(h.Cess))
			line("     %s", trunc(supplierOf(ln.Invoice), 96))
		}
		line("   %s", strings.Repeat("-", 103))
		line("   %-42s %12s %12s %12s %12s %12s", "TOTAL TO REVERSE", "",
			Rupees(revTotal.IGST), Rupees(revTotal.CGST), Rupees(revTotal.SGST), Rupees(revTotal.Cess))
		line("   TOTAL TO REVERSE, ALL FOUR HEADS: %s", Rupees(revTotal.Total()))
	}
	line("")

	recTotal := Heads{}
	line("2. RE-CLAIM — FORM GSTR-3B TABLE %s, DISCLOSED IN TABLE %s", TableReclaim, TableReclaimDisclosure)
	line("   Credit reversed earlier under rule 37 and taken again now the supplier is")
	line("   paid, as rule 37(2) permits. Rule 37(4) keeps the s.16(4) time limit out of")
	line("   the way, so a late payment still restores the credit.")
	line("")
	if len(reclaims) == 0 {
		line("   Nothing to re-claim in this return.")
	} else {
		line("   %-18s %-20s %12s %12s %12s %12s", "Invoice", "Claim in return", "IGST", "CGST", "SGST", "Cess")
		line("   %s", strings.Repeat("-", 89))
		for _, ln := range reclaims {
			a := ln.Assessment
			for _, rp := range a.ReclaimPeriods {
				if rp.Period.Compare(period) != 0 {
					continue
				}
				h := rp.Heads.RoundToRupee()
				recTotal = recTotal.Add(h)
				line("   %-18s %-20s %12s %12s %12s %12s",
					trunc(ln.Invoice.InvoiceNo, 18), trunc(rp.Period.String(), 20),
					Rupees(h.IGST), Rupees(h.CGST), Rupees(h.SGST), Rupees(h.Cess))
				line("     %s", trunc(supplierOf(ln.Invoice), 82))
			}
		}
		line("   %s", strings.Repeat("-", 89))
		line("   %-39s %12s %12s %12s %12s", "TOTAL TO RE-CLAIM",
			Rupees(recTotal.IGST), Rupees(recTotal.CGST), Rupees(recTotal.SGST), Rupees(recTotal.Cess))
		line("   TOTAL TO RE-CLAIM, ALL FOUR HEADS: %s", Rupees(recTotal.Total()))
	}
	line("")

	line("3. NET EFFECT ON THIS RETURN")
	line("   %-34s %18s", "Reversed in table "+TableReversal, Rupees(revTotal.Total()))
	line("   %-34s %18s", "Re-claimed in table "+TableReclaim, Rupees(recTotal.Total()))
	line("   %s", strings.Repeat("-", 53))
	line("   %-34s %18s", "Net added to output tax liability", Rupees(revTotal.Total()-recTotal.Total()))
	line("")
	if !recTotal.IsZero() {
		line("   Before filing, check the re-claim against the closing balance of the")
		line("   %s on the portal: the", ReversalLedgerName)
		line("   credit reclaimed in table %s may not exceed that balance plus the", TableReclaimDisclosure)
		line("   reversal reported in table %s of this same return.", TableReversal)
		line("")
	}

	var interest int64
	for _, ln := range reversals {
		interest += ln.Assessment.Interest
	}
	if interest > 0 {
		line("4. INTEREST — INDICATIVE ONLY")
		line("   Indicative interest: %s, at %.2f%% a year (the rate notified under", Rupees(interest), set.InterestRatePct)
		line("   s.50(3)), computed %s.", set.InterestBasis.Label())
		line("   Interest arises because the reversal is being made after the return it")
		line("   belonged in fell due. A reversal made in the correct return is widely")
		line("   taken to attract none. This figure is an estimate of exposure, not a")
		line("   demand: confirm it with your practitioner.")
		line("")
	}

	line("Prepared by ........................  Date ....................")
	line("")
	line("This working paper is not a return and has not been filed. Check every figure")
	line("against the invoices and the ledger before it is used.")
	return b.String()
}

func supplierOf(inv Invoice) string {
	who := strings.TrimSpace(inv.SupplierName)
	switch {
	case who == "" && inv.SupplierGSTIN == "":
		return "(supplier not recorded)"
	case who == "":
		return inv.SupplierGSTIN
	case inv.SupplierGSTIN == "":
		return who
	}
	return who + " · " + inv.SupplierGSTIN
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
