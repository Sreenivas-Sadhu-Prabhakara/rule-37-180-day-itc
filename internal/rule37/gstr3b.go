package rule37

import (
	"sort"
	"strings"
)

// Where the two entries go in FORM GSTR-3B.
//
// Circular No. 170/02/2022-GST dated 6 July 2022, paragraph 4.3(C): "Registered
// person will report reversal of ITC, which are not permanent in nature and can
// be reclaimed in future subject to fulfilment of specific conditions, such as
// on account of rule 37 of CGST Rules (non-payment of consideration to supplier
// within 180 days) ... in Table 4 (B) (2). Such ITC may be reclaimed in Table
// 4(A)(5) on fulfilment of necessary conditions. Further, all such reclaimed ITC
// shall also be shown in Table 4(D)(1)."
//
// Getting the table right matters beyond tidiness: a rule 37 reversal reported
// in table 4(B)(1) instead of 4(B)(2) is treated as a permanent reversal, does
// not reach the portal's reversal ledger, and leaves no balance to re-claim
// against when the supplier is finally paid.
const (
	// TableReversal is where the reversal is reported: a reversal that is not
	// permanent and may be reclaimed. Its heading in the form is "Others".
	TableReversal = "4(B)(2)"

	// TableReclaim is where the credit is taken again, under "All other ITC".
	TableReclaim = "4(A)(5)"

	// TableReclaimDisclosure is where the re-claim must also be disclosed. Its
	// heading, as substituted by Notification No. 14/2022-Central Tax dated
	// 5 July 2022, is "ITC reclaimed which was reversed under Table 4(B)(2) in
	// earlier tax period".
	TableReclaimDisclosure = "4(D)(1)"
)

// ReversalLedgerName is the statement the GST portal keeps of reversals
// reported in table 4(B)(2) and re-claims reported in tables 4(A)(5) and
// 4(D)(1), running from the August 2023 return period for monthly filers and
// the July–September 2023 quarter for quarterly ones.
//
// It matters here because the portal checks the re-claim against it: the credit
// reclaimed in table 4(D)(1) must not exceed the closing balance of the
// statement plus the reversal being reported in table 4(B)(2) of the same
// return. A re-claim this tool schedules is therefore only as good as the
// reversal that preceded it having been reported in the right table.
const ReversalLedgerName = "Electronic Credit Reversal and Re-claimed Statement"

// quarterlyDueDayByState maps the State or Union territory of the principal
// place of business to the day of the month a quarterly (QRMP) filer's FORM
// GSTR-3B is due, per the table in rule 61(1)(ii) of the Central Goods and
// Services Tax Rules, 2017.
//
// Note that Chandigarh and Delhi fall in the twenty-fourth-day group, which is
// the entry most often got wrong.
var quarterlyDueDayByState = buildStateDueDays()

func buildStateDueDays() map[string]int {
	early := []string{
		"Chhattisgarh", "Madhya Pradesh", "Gujarat", "Maharashtra", "Karnataka",
		"Goa", "Kerala", "Tamil Nadu", "Telangana", "Andhra Pradesh",
		"Daman and Diu and Dadra and Nagar Haveli", "Dadra and Nagar Haveli and Daman and Diu",
		"Dadra and Nagar Haveli", "Daman and Diu",
		"Puducherry", "Pondicherry", "Andaman and Nicobar Islands", "Lakshadweep",
	}
	late := []string{
		"Himachal Pradesh", "Punjab", "Uttarakhand", "Haryana", "Rajasthan",
		"Uttar Pradesh", "Bihar", "Sikkim", "Arunachal Pradesh", "Nagaland",
		"Manipur", "Mizoram", "Tripura", "Meghalaya", "Assam", "West Bengal",
		"Jharkhand", "Odisha", "Orissa", "Jammu and Kashmir", "Ladakh",
		"Chandigarh", "Delhi", "NCT of Delhi", "New Delhi",
	}
	out := make(map[string]int, len(early)+len(late))
	for _, s := range early {
		out[normaliseHeading(s)] = QuarterlyDueDayEarly
	}
	for _, s := range late {
		out[normaliseHeading(s)] = QuarterlyDueDayLate
	}
	return out
}

// QuarterlyDueDayFor returns the due day for a quarterly filer whose principal
// place of business is in the given State or Union territory, and whether the
// name was recognised. An unrecognised name leaves the caller to ask rather than
// guess: the two groups are three days apart and the later one is not the safe
// assumption.
func QuarterlyDueDayFor(state string) (int, bool) {
	day, ok := quarterlyDueDayByState[normaliseHeading(state)]
	return day, ok
}

// StatesByQuarterlyDueDay lists the recognised States and Union territories for
// one of the two quarterly due days, in alphabetical order, so a form can offer
// them. Alternative spellings of the same place are included, because a register
// may use any of them.
func StatesByQuarterlyDueDay(day int) []string {
	canonical := []string{
		"Andaman and Nicobar Islands", "Andhra Pradesh", "Arunachal Pradesh", "Assam",
		"Bihar", "Chandigarh", "Chhattisgarh", "Dadra and Nagar Haveli and Daman and Diu",
		"Delhi", "Goa", "Gujarat", "Haryana", "Himachal Pradesh", "Jammu and Kashmir",
		"Jharkhand", "Karnataka", "Kerala", "Ladakh", "Lakshadweep", "Madhya Pradesh",
		"Maharashtra", "Manipur", "Meghalaya", "Mizoram", "Nagaland", "Odisha",
		"Puducherry", "Punjab", "Rajasthan", "Sikkim", "Tamil Nadu", "Telangana",
		"Tripura", "Uttar Pradesh", "Uttarakhand", "West Bengal",
	}
	var out []string
	for _, s := range canonical {
		if d, ok := QuarterlyDueDayFor(s); ok && d == day {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// ReAvailmentNotTimeBarred records that the last date for taking input tax
// credit on an invoice does not stand in the way of taking back credit that was
// reversed under rule 37.
//
// Rule 37(4) of the Central Goods and Services Tax Rules, 2017 — which the 2022
// substitution left untouched: "The time limit specified in sub-section (4) of
// section 16 shall not apply to a claim for re-availing of any credit, in
// accordance with the provisions of the Act or the provisions of this Chapter,
// that had been reversed earlier."
//
// So a supplier paid two years late still restores the credit. That is worth
// knowing before writing off a reversal as lost, and it is why this tool
// schedules a re-claim however long after the deadline the payment is made.
const ReAvailmentNotTimeBarred = true

// StateNameLooksLike reports whether s is probably a State or Union territory
// name this package knows, which lets a form validate the field without
// insisting on an exact spelling.
func StateNameLooksLike(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	_, ok := QuarterlyDueDayFor(s)
	return ok
}
