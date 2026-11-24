package rule37

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// maxFileBytes refuses a file far larger than any small business's purchase
// register, before it is read into memory.
const maxFileBytes = 32 << 20

// maxProblems caps the list of row problems carried back to the user. Past this
// many, something is wrong with the whole file rather than with its rows, and a
// list of ten thousand complaints helps nobody.
const maxProblems = 200

// RowProblem is one thing wrong with one line of a file. Every problem names
// the line as the file numbers it, so the row can be found and fixed; the row
// is left out of the import and nothing else is affected by it.
type RowProblem struct {
	Line   int    `json:"line"`
	Field  string `json:"field,omitempty"`
	Value  string `json:"value,omitempty"`
	Reason string `json:"reason"`
}

func (p RowProblem) String() string {
	switch {
	case p.Field != "" && p.Value != "":
		return fmt.Sprintf("line %d, %s %q: %s", p.Line, p.Field, p.Value, p.Reason)
	case p.Field != "":
		return fmt.Sprintf("line %d, %s: %s", p.Line, p.Field, p.Reason)
	default:
		return fmt.Sprintf("line %d: %s", p.Line, p.Reason)
	}
}

// Import is the result of reading a purchase register.
type Import struct {
	Invoices []Invoice    `json:"invoices"`
	Problems []RowProblem `json:"problems,omitempty"`
	Notes    []string     `json:"notes,omitempty"`
	Rows     int          `json:"rows"`
	Skipped  int          `json:"skipped"`
}

// PaymentRow is one payment read from a payments file, still to be matched to
// the invoice it is against.
type PaymentRow struct {
	GSTIN     string  `json:"gstin"`
	InvoiceNo string  `json:"invoiceNo"`
	Payment   Payment `json:"payment"`
}

// PaymentImport is the result of reading a payments file.
type PaymentImport struct {
	Rows     []PaymentRow `json:"rows"`
	Problems []RowProblem `json:"problems,omitempty"`
	Notes    []string     `json:"notes,omitempty"`
	Read     int          `json:"read"`
	Skipped  int          `json:"skipped"`
}

// Column identifiers. One register column may be spelled a dozen ways; the
// identifier is what the rest of the code uses.
const (
	colGSTIN     = "gstin"
	colName      = "name"
	colInvNo     = "invno"
	colInvDate   = "invdate"
	colTaxable   = "taxable"
	colIGST      = "igst"
	colCGST      = "cgst"
	colSGST      = "sgst"
	colCess      = "cess"
	colValue     = "value"
	colPaid      = "paid"
	colPaidDate  = "paiddate"
	colExclusion = "exclusion"
	colNote      = "note"
	colAmount    = "amount"
	colRef       = "ref"
)

// registerAliases lists the headings a purchase register exported from a
// spreadsheet or an accounting package actually uses. Headings are compared
// with case, spaces, punctuation and currency marks removed, and a heading that
// merely starts with an alias also matches, so "Invoice date (dd-mm-yyyy)" and
// "IGST Amount (₹)" are both understood.
var registerAliases = map[string][]string{
	colGSTIN: {"suppliergstin", "suppliergstinuin", "suppliergst", "vendorgstin", "partygstin", "gstinofsupplier", "gstinofthesupplier", "gstinuin", "gstinno", "gstno", "gstin", "ctin"},
	colName:  {"suppliername", "nameofsupplier", "supplierpartyname", "partyname", "vendorname", "tradename", "supplier", "vendor", "party"},
	colInvNo: {"supplierinvoiceno", "invoicenumber", "invoiceno", "invoicenum", "invoiceid", "billnumber", "documentnumber", "voucherno", "billno", "docno", "invno", "inum"},
	colInvDate: {"invoicedate", "dateofinvoice", "documentdate", "voucherdate", "billdate", "invdate", "docdate",
		"idt"},
	colTaxable:   {"taxablevalueofsupply", "taxablevalue", "taxableamount", "assessablevalue", "taxableamt", "basevalue", "netvalue", "taxable", "txval"},
	colIGST:      {"igstcredit", "igstamount", "igstavailed", "igstamt", "igstitc", "igsttax", "igst", "iamt"},
	colCGST:      {"cgstcredit", "cgstamount", "cgstavailed", "cgstamt", "cgstitc", "cgsttax", "cgst", "camt"},
	colSGST:      {"sgstutgstamount", "sgstutgst", "sgstcredit", "sgstamount", "sgstavailed", "sgstamt", "sgstitc", "sgsttax", "utgst", "sgst", "samt"},
	colCess:      {"cesscredit", "cessamount", "cessavailed", "cessamt", "cessitc", "cess", "csamt"},
	colValue:     {"totalinvoicevalue", "invoicetotal", "invoicevalue", "invoiceamount", "grossamount", "grosstotal", "grandtotal", "billamount", "totalvalue", "totalamount", "payable", "val"},
	colPaid:      {"amountpaidtosupplier", "amountpaid", "paidamount", "paidtosupplier", "paidtodate", "paidsofar", "amountsettled", "paymentamount", "paid"},
	colPaidDate:  {"dateofpayment", "paymentdate", "datepaid", "paiddate", "payondate", "settledon", "paidon"},
	colExclusion: {"outofscopereason", "exclusionreason", "reversecharge", "exclusion", "scope", "basis", "rcm"},
	colNote:      {"remarks", "narration", "comment", "notes", "note"},
}

// paymentAliases are the headings of a payments file. They are kept apart from
// the register's because the same words mean different columns in the two
// files: "Amount paid" is one invoice's running total in a register and one
// payment's amount here, and a single table of aliases would have them fight
// over it.
var paymentAliases = map[string][]string{
	colGSTIN:    registerAliases[colGSTIN],
	colInvNo:    registerAliases[colInvNo],
	colPaidDate: append([]string{"paymentdate", "dateofpayment", "datepaid", "paiddate", "valuedate", "paidon", "date"}, registerAliases[colPaidDate]...),
	colAmount:   {"paymentamount", "amountpaid", "amountpaidtosupplier", "paidamount", "amount", "paid", "value", "amt"},
	colRef:      {"paymentreference", "referenceno", "reference", "utrno", "utr", "chequeno", "neftno", "voucherno", "ref"},
}

// colTitles are the names used when talking to the user about a column.
var colTitles = map[string]string{
	colGSTIN:     "Supplier GSTIN",
	colName:      "Supplier name",
	colInvNo:     "Invoice no",
	colInvDate:   "Invoice date",
	colTaxable:   "Taxable value",
	colIGST:      "IGST",
	colCGST:      "CGST",
	colSGST:      "SGST",
	colCess:      "Cess",
	colValue:     "Invoice value",
	colPaid:      "Amount paid",
	colPaidDate:  "Payment date",
	colExclusion: "Out-of-scope reason",
	colNote:      "Remarks",
	colAmount:    "Amount",
	colRef:       "Reference",
}

// ParseRegisterCSV reads a purchase register: one line per invoice on which
// input tax credit has been taken, with what has been paid against it.
//
// It is deliberately forgiving about the shape of the file and unforgiving
// about losing a row. It finds the heading row itself, so title rows above it
// are skipped; it accepts comma, semicolon, tab and pipe separated files;
// blank, "-" and "nil" cells read as zero; and a total line at the foot is
// recognised and left out. A row it cannot use is reported as a problem naming
// the line and the field, and the rest of the file still imports.
//
// It returns an error only where there is nothing to import at all: an
// unreadable file, no heading row, or no usable rows under one.
func ParseRegisterCSV(r io.Reader) (Import, error) {
	var out Import
	rows, delim, err := readRows(r, "purchase register")
	if err != nil {
		return out, err
	}

	headerAt, cols := findHeader(rows, registerAliases, colInvNo, colInvDate)
	if headerAt < 0 {
		return out, fmt.Errorf("could not find the heading row in the purchase register — it needs a column for the invoice number and one for the invoice date, for example: Supplier GSTIN, Supplier name, Invoice no, Invoice date, Taxable value, IGST, CGST, SGST, Cess, Invoice value, Amount paid, Payment date")
	}
	out.Notes = append(out.Notes, delimiterNote(delim)...)

	var missing []string
	for _, c := range []string{colIGST, colCGST, colSGST, colCess} {
		if _, ok := cols[c]; !ok {
			missing = append(missing, colTitles[c])
		}
	}
	if len(missing) == 4 {
		return out, fmt.Errorf("the purchase register has no column for IGST, CGST, SGST or cess — rule 37 reverses the input tax credit availed, so at least one of those columns is needed")
	}
	if len(missing) > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("No column for %s, so %s read as nil.", joinAnd(missing), pick(len(missing), "that head was", "those heads were")))
	}
	if _, ok := cols[colPaid]; !ok {
		out.Notes = append(out.Notes, "No \"Amount paid\" column, so every invoice is read as wholly unpaid. Add one — and a \"Payment date\" column beside it — to see the real position.")
	} else if _, ok := cols[colPaidDate]; !ok {
		out.Notes = append(out.Notes, "There is an \"Amount paid\" column but no \"Payment date\" column. A payment with no date is counted as paid by today, which means the 180-day test treats it as late. Add the dates to get the reversals right.")
	}
	if _, ok := cols[colGSTIN]; !ok {
		out.Notes = append(out.Notes, "No supplier GSTIN column. The invoices still work, but a payments file cannot be matched to them without it.")
	}

	seen := map[string]int{}
	var badGSTIN []int
	for _, rw := range rows[headerAt+1:] {
		cell := cellFunc(cols, rw.cells)
		if allBlank(rw.cells) {
			continue
		}
		no := cell(colInvNo)
		gstinRaw := cell(colGSTIN)
		if isTotalLabel(no) || isTotalLabel(gstinRaw) || isTotalLabel(firstNonBlank(rw.cells)) {
			out.Skipped++
			continue
		}
		out.Rows++

		if no == "" {
			out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colInvNo], Reason: "no invoice number, so the row cannot be identified"})
			continue
		}
		dateText := cell(colInvDate)
		if dateText == "" {
			out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colInvDate], Reason: fmt.Sprintf("invoice %s has no invoice date, and the 180 days are counted from it", no)})
			continue
		}
		invDate, err := ParseLooseDate(dateText)
		if err != nil {
			out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colInvDate], Value: dateText, Reason: err.Error()})
			continue
		}

		amounts, amountErr := readAmounts(cell, []string{colTaxable, colIGST, colCGST, colSGST, colCess, colValue, colPaid})
		if amountErr != nil {
			out.addProblem(RowProblem{Line: rw.line, Field: amountErr.field, Value: amountErr.value, Reason: amountErr.reason})
			continue
		}

		exclusion, err := ParseExclusion(cell(colExclusion))
		if err != nil {
			out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colExclusion], Value: cell(colExclusion), Reason: err.Error()})
			continue
		}

		inv := Invoice{
			SupplierName:  cell(colName),
			SupplierGSTIN: NormaliseGSTIN(gstinRaw),
			InvoiceNo:     no,
			InvoiceDate:   invDate,
			TaxableValue:  amounts[colTaxable],
			ITC: Heads{
				IGST: amounts[colIGST],
				CGST: amounts[colCGST],
				SGST: amounts[colSGST],
				Cess: amounts[colCess],
			},
			InvoiceValue: amounts[colValue],
			Exclusion:    exclusion,
			Note:         cell(colNote),
			Line:         rw.line,
		}

		if paid := amounts[colPaid]; paid != 0 {
			p := Payment{Amount: paid, Line: rw.line}
			if text := cell(colPaidDate); text != "" {
				d, err := ParseLooseDate(text)
				if err != nil {
					out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colPaidDate], Value: text, Reason: err.Error()})
					continue
				}
				p.Date = d
			}
			inv.Payments = append(inv.Payments, p)
		}

		if err := inv.Validate(); err != nil {
			out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colInvNo], Value: no, Reason: err.Error()})
			continue
		}
		if g := inv.SupplierGSTIN; g != "" && !ValidGSTINFormat(g) {
			badGSTIN = append(badGSTIN, rw.line)
		}

		key := NormaliseGSTIN(inv.SupplierGSTIN) + "|" + NormaliseInvoiceNo(inv.InvoiceNo) + "|" + FinancialYear(inv.InvoiceDate)
		if first, dup := seen[key]; dup {
			out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colInvNo], Value: no, Reason: fmt.Sprintf("the same invoice is already on line %d — importing it twice would reverse the credit twice", first)})
			continue
		}
		seen[key] = rw.line

		out.Invoices = append(out.Invoices, inv)
	}

	if len(out.Invoices) == 0 {
		if len(out.Problems) > 0 {
			return out, fmt.Errorf("no usable invoice rows in the purchase register — %s", out.Problems[0].String())
		}
		return out, fmt.Errorf("the purchase register has a heading row but no invoice rows under it")
	}
	if out.Skipped > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("Left out %s that looked like a total rather than an invoice.", plural(out.Skipped, "row", "rows")))
	}
	if len(badGSTIN) > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%s a supplier GSTIN that is not in the standard 15-character form (%s). The arithmetic is unaffected; check them anyway.",
			countPhrase(len(badGSTIN), "One line has", "lines have"), linesList(badGSTIN)))
	}
	out.noteProblems("purchase register")
	return out, nil
}

// ParsePaymentsCSV reads a file of payments made to suppliers, one per line,
// each naming the invoice it is against. It is the second half of the picture:
// the register says what is owed, this says what has actually gone out.
func ParsePaymentsCSV(r io.Reader) (PaymentImport, error) {
	var out PaymentImport
	rows, delim, err := readRows(r, "payments file")
	if err != nil {
		return out, err
	}

	headerAt, cols := findHeader(rows, paymentAliases, colInvNo, colPaidDate)
	if headerAt < 0 {
		headerAt, cols = findHeader(rows, paymentAliases, colInvNo, colAmount)
	}
	if headerAt < 0 {
		return out, fmt.Errorf("could not find the heading row in the payments file — it needs a column for the invoice number and one for the amount, for example: Supplier GSTIN, Invoice no, Payment date, Amount, Reference")
	}
	if _, ok := cols[colAmount]; !ok {
		return out, fmt.Errorf("the payments file has no amount column — add one headed \"Amount\"")
	}
	out.Notes = append(out.Notes, delimiterNote(delim)...)
	if _, ok := cols[colPaidDate]; !ok {
		out.Notes = append(out.Notes, "No payment date column. Every payment is counted as paid by today, which means the 180-day test treats it as late; add the dates to get the reversals right.")
	}

	for _, rw := range rows[headerAt+1:] {
		cell := cellFunc(cols, rw.cells)
		if allBlank(rw.cells) {
			continue
		}
		no := cell(colInvNo)
		if isTotalLabel(no) || isTotalLabel(firstNonBlank(rw.cells)) {
			out.Skipped++
			continue
		}
		out.Read++
		if no == "" {
			out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colInvNo], Reason: "no invoice number, so the payment cannot be matched to an invoice"})
			continue
		}
		amount, err := ParseAmount(cell(colAmount))
		if err != nil {
			out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colAmount], Value: cell(colAmount), Reason: err.Error()})
			continue
		}
		if amount == 0 {
			out.Skipped++
			continue
		}
		if amount < 0 {
			out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colAmount], Value: cell(colAmount), Reason: "a payment cannot be negative — record a refund by reducing the amount paid"})
			continue
		}
		pay := Payment{Amount: amount, Ref: cell(colRef), Line: rw.line}
		if text := cell(colPaidDate); text != "" {
			d, err := ParseLooseDate(text)
			if err != nil {
				out.addProblem(RowProblem{Line: rw.line, Field: colTitles[colPaidDate], Value: text, Reason: err.Error()})
				continue
			}
			pay.Date = d
		}
		out.Rows = append(out.Rows, PaymentRow{GSTIN: NormaliseGSTIN(cell(colGSTIN)), InvoiceNo: no, Payment: pay})
	}

	if len(out.Rows) == 0 {
		if len(out.Problems) > 0 {
			return out, fmt.Errorf("no usable payment rows — %s", out.Problems[0].String())
		}
		return out, fmt.Errorf("the payments file has a heading row but no payment rows under it")
	}
	out.noteProblems("payments file")
	return out, nil
}

// AttachPayments matches payment rows to invoices and returns the invoices with
// the payments added.
//
// A payment is matched on the supplier GSTIN and the invoice number, both
// normalised, so a slash or three leading zeros does not lose a payment. Where
// the payments file gives no GSTIN, the invoice number alone is used; where that
// is ambiguous — the same number from two suppliers, or in two financial years
// — the payment is reported rather than guessed at, because attaching it to the
// wrong invoice would reverse the credit on the wrong one.
//
// The invoices are returned as a new slice; the input is not modified.
func AttachPayments(invoices []Invoice, rows []PaymentRow) ([]Invoice, []RowProblem) {
	out := make([]Invoice, len(invoices))
	for i, inv := range invoices {
		out[i] = inv
		out[i].Payments = append([]Payment(nil), inv.Payments...)
	}

	byFull := map[string][]int{}
	byNo := map[string][]int{}
	for i, inv := range out {
		no := NormaliseInvoiceNo(inv.InvoiceNo)
		byFull[NormaliseGSTIN(inv.SupplierGSTIN)+"|"+no] = append(byFull[NormaliseGSTIN(inv.SupplierGSTIN)+"|"+no], i)
		byNo[no] = append(byNo[no], i)
	}

	var problems []RowProblem
	for _, row := range rows {
		no := NormaliseInvoiceNo(row.InvoiceNo)
		var hits []int
		if row.GSTIN != "" {
			hits = byFull[row.GSTIN+"|"+no]
		}
		if len(hits) == 0 {
			hits = byNo[no]
		}
		switch len(hits) {
		case 0:
			problems = append(problems, RowProblem{
				Line:   row.Payment.Line,
				Field:  colTitles[colInvNo],
				Value:  row.InvoiceNo,
				Reason: "no invoice with that number in the purchase register, so this payment was not applied",
			})
		case 1:
			out[hits[0]].Payments = append(out[hits[0]].Payments, row.Payment)
		default:
			lines := make([]int, 0, len(hits))
			for _, h := range hits {
				lines = append(lines, out[h].Line)
			}
			sort.Ints(lines)
			problems = append(problems, RowProblem{
				Line:   row.Payment.Line,
				Field:  colTitles[colInvNo],
				Value:  row.InvoiceNo,
				Reason: fmt.Sprintf("more than one invoice has that number (%s) — add the supplier GSTIN to the payment so it can be matched", linesList(lines)),
			})
		}
	}
	for i := range out {
		out[i].SortPayments()
	}
	return out, problems
}

/* ------------------------------------------------------------------ reading */

type rawRow struct {
	line  int
	cells []string
}

// readRows reads the whole file into rows, tolerating the separators, quoting
// and byte-order marks that real exports carry.
func readRows(r io.Reader, what string) ([]rawRow, rune, error) {
	if r == nil {
		return nil, ',', fmt.Errorf("no %s was given", what)
	}
	raw, err := io.ReadAll(io.LimitReader(r, maxFileBytes+1))
	if err != nil {
		return nil, ',', fmt.Errorf("could not read the %s: %w", what, err)
	}
	if len(raw) > maxFileBytes {
		return nil, ',', fmt.Errorf("the %s is larger than %d MB, which is too large to be a register", what, maxFileBytes>>20)
	}
	raw = bytes.TrimPrefix(raw, []byte("\ufeff"))
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, ',', fmt.Errorf("the %s is empty", what)
	}

	delim := detectDelimiter(raw)
	rd := csv.NewReader(bytes.NewReader(raw))
	rd.Comma = delim
	rd.FieldsPerRecord = -1
	rd.LazyQuotes = true
	rd.TrimLeadingSpace = true
	rd.ReuseRecord = false

	var rows []rawRow
	for {
		rec, err := rd.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				return nil, delim, fmt.Errorf("line %d of the %s could not be read: %v — check the quotation marks on that line", pe.Line, what, pe.Err)
			}
			return nil, delim, fmt.Errorf("could not read the %s: %w", what, err)
		}
		if len(rec) == 0 {
			continue
		}
		line, _ := rd.FieldPos(0)
		rows = append(rows, rawRow{line: line, cells: rec})
	}
	if len(rows) == 0 {
		return nil, delim, fmt.Errorf("the %s has no rows", what)
	}
	return rows, delim, nil
}

// findHeader looks for the heading row, which is not always the first: registers
// often carry a title or a date range above it. A row counts as the headings
// when it names every column in must.
func findHeader(rows []rawRow, aliases map[string][]string, must ...string) (int, map[string]int) {
	const scan = 25
	for i, rw := range rows {
		if i >= scan {
			break
		}
		m := mapColumns(rw.cells, aliases)
		ok := true
		for _, c := range must {
			if _, has := m[c]; !has {
				ok = false
				break
			}
		}
		if ok {
			return i, m
		}
	}
	return -1, nil
}

// mapColumns works out which column holds which field, preferring the longest
// alias that matches so "Supplier GSTIN" does not lose to "GSTIN" and
// "Payment date" does not lose to "Date".
func mapColumns(cells []string, byColumn map[string][]string) map[string]int {
	type aliasRef struct{ col, alias string }
	var refs []aliasRef
	for col, aliases := range byColumn {
		for _, a := range aliases {
			refs = append(refs, aliasRef{col: col, alias: a})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if len(refs[i].alias) != len(refs[j].alias) {
			return len(refs[i].alias) > len(refs[j].alias)
		}
		if refs[i].alias != refs[j].alias {
			return refs[i].alias < refs[j].alias
		}
		return refs[i].col < refs[j].col
	})

	out := map[string]int{}
	for i, c := range cells {
		h := normaliseHeading(c)
		if h == "" {
			continue
		}
		// A rate or percentage column is never an amount column.
		if strings.Contains(h, "rate") || strings.Contains(h, "percent") {
			continue
		}
		for _, ref := range refs {
			if h != ref.alias && !strings.HasPrefix(h, ref.alias) {
				continue
			}
			if _, taken := out[ref.col]; !taken {
				out[ref.col] = i
			}
			break
		}
	}
	return out
}

// normaliseHeading reduces a heading to lower-case letters and digits.
func normaliseHeading(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// detectDelimiter picks the separator from the line with the most of one, so a
// semicolon, tab or pipe separated export also reads.
func detectDelimiter(raw []byte) rune {
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		best, bestN := ',', strings.Count(line, ",")
		for _, c := range []rune{';', '\t', '|'} {
			if n := strings.Count(line, string(c)); n > bestN {
				best, bestN = c, n
			}
		}
		return best
	}
	return ','
}

func delimiterNote(delim rune) []string {
	switch delim {
	case ';':
		return []string{"Read as a semicolon-separated file."}
	case '\t':
		return []string{"Read as a tab-separated file."}
	case '|':
		return []string{"Read as a pipe-separated file."}
	}
	return nil
}

// cellFunc returns a reader for one row's cells by column identifier, safe
// against a short row.
func cellFunc(cols map[string]int, cells []string) func(string) string {
	return func(col string) string {
		i, ok := cols[col]
		if !ok || i < 0 || i >= len(cells) {
			return ""
		}
		return strings.TrimSpace(cells[i])
	}
}

type amountError struct{ field, value, reason string }

func (e *amountError) Error() string { return e.reason }

// readAmounts reads every money column of a row, naming the column in any
// complaint so the user knows which cell to look at.
func readAmounts(cell func(string) string, columns []string) (map[string]int64, *amountError) {
	out := make(map[string]int64, len(columns))
	for _, c := range columns {
		text := cell(c)
		v, err := ParseAmount(text)
		if err != nil {
			return nil, &amountError{field: colTitles[c], value: text, reason: err.Error()}
		}
		out[c] = v
	}
	return out, nil
}

func (im *Import) addProblem(p RowProblem) {
	if len(im.Problems) < maxProblems {
		im.Problems = append(im.Problems, p)
	}
}

func (im *Import) noteProblems(what string) {
	if n := len(im.Problems); n > 0 {
		im.Notes = append(im.Notes, fmt.Sprintf("%s of the %s could not be used and %s left out. Each one is listed with its line number.",
			countPhrase(n, "One line", "lines"), what, pick(n, "was", "were")))
	}
	if len(im.Problems) >= maxProblems {
		im.Notes = append(im.Notes, fmt.Sprintf("Only the first %d problems are listed.", maxProblems))
	}
}

func (pi *PaymentImport) addProblem(p RowProblem) {
	if len(pi.Problems) < maxProblems {
		pi.Problems = append(pi.Problems, p)
	}
}

func (pi *PaymentImport) noteProblems(what string) {
	if n := len(pi.Problems); n > 0 {
		pi.Notes = append(pi.Notes, fmt.Sprintf("%s of the %s could not be used and %s left out. Each one is listed with its line number.",
			countPhrase(n, "One line", "lines"), what, pick(n, "was", "were")))
	}
}

// pick chooses between a singular and a plural word without printing the count.
func pick(n int, one, many string) string {
	if n == 1 || n == -1 {
		return one
	}
	return many
}

// countPhrase reads "One line has" for a single item and "7 lines have" for
// several, so a sentence can start with it.
func countPhrase(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func allBlank(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func firstNonBlank(cells []string) string {
	for _, c := range cells {
		if t := strings.TrimSpace(c); t != "" {
			return t
		}
	}
	return ""
}

// isTotalLabel spots the summary line at the foot of a register.
func isTotalLabel(s string) bool {
	switch normaliseHeading(s) {
	case "total", "totals", "grandtotal", "subtotal", "sumtotal", "sum":
		return true
	}
	return false
}

func linesList(lines []int) string {
	const show = 8
	parts := make([]string, 0, show+1)
	for i, l := range lines {
		if i == show {
			parts = append(parts, fmt.Sprintf("and %d more", len(lines)-show))
			break
		}
		parts = append(parts, fmt.Sprintf("line %d", l))
	}
	return strings.Join(parts, ", ")
}

func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
