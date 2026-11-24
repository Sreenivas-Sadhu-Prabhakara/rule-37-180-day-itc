package rule37

import (
	"fmt"
	"math/big"
	"strings"
)

// All money in this package is held as whole paise in an int64, so that sums,
// proportions and comparisons are exact and never drift. 100 paise = ₹1.00.

// RupeePaise is the number of paise in a rupee.
const RupeePaise int64 = 100

// ParseAmount reads a rupee amount written the way accounting exports write it
// and returns whole paise.
//
// It accepts a leading currency marker (₹, Rs, Rs., INR), Indian or Western
// digit grouping ("1,00,000.50" and "100,000.50"), a sign, accounting negatives
// in brackets, surrounding spaces, and the dashes and blanks that spreadsheets
// use for nil ("", "-", "--"). Anything finer than a paisa is rounded half away
// from zero, because no tax amount is finer than that.
func ParseAmount(s string) (int64, error) {
	t := strings.TrimSpace(s)
	t = strings.NewReplacer(" ", "", " ", "", "₹", "", ",", "", "'", "").Replace(t)
	// Drop a currency word, in any case: "Rs", "Rs.", "INR".
	for _, p := range []string{"INR", "RS.", "RS"} {
		if len(t) >= len(p) && strings.EqualFold(t[:len(p)], p) {
			t = t[len(p):]
			break
		}
	}
	neg := false
	// Accounting negatives: (500) means -500.
	if len(t) >= 2 && t[0] == '(' && t[len(t)-1] == ')' {
		neg = true
		t = t[1 : len(t)-1]
	}
	if t != "" && (t[0] == '+' || t[0] == '-') {
		neg = neg != (t[0] == '-')
		t = t[1:]
	}
	// Blank and dash-only cells mean zero, not a mistake.
	if t == "" || strings.Trim(t, "-–—.") == "" {
		return 0, nil
	}

	intPart, fracPart := t, ""
	if i := strings.IndexByte(t, '.'); i >= 0 {
		intPart, fracPart = t[:i], t[i+1:]
		if strings.ContainsRune(fracPart, '.') {
			return 0, fmt.Errorf("%q has more than one decimal point", s)
		}
	}
	if intPart == "" {
		intPart = "0"
	}
	var rupees int64
	for i := 0; i < len(intPart); i++ {
		c := intPart[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%q is not an amount", s)
		}
		if rupees > maxRupees/10 {
			return 0, fmt.Errorf("%q is too large to be an invoice amount", s)
		}
		rupees = rupees*10 + int64(c-'0')
	}
	if rupees > maxRupees {
		return 0, fmt.Errorf("%q is too large to be an invoice amount", s)
	}
	for i := 0; i < len(fracPart); i++ {
		if fracPart[i] < '0' || fracPart[i] > '9' {
			return 0, fmt.Errorf("%q is not an amount", s)
		}
	}
	paise := rupees * RupeePaise
	if fracPart != "" {
		// Pad or round to exactly two decimal places.
		switch {
		case len(fracPart) == 1:
			paise += int64(fracPart[0]-'0') * 10
		default:
			p := int64(fracPart[0]-'0')*10 + int64(fracPart[1]-'0')
			if len(fracPart) > 2 && fracPart[2] >= '5' {
				p++
			}
			paise += p
		}
	}
	if neg {
		paise = -paise
	}
	return paise, nil
}

// maxRupees bounds an amount read from a file at one lakh crore rupees. It is a
// sanity limit, not a statutory one: a figure larger than this in a small
// business's purchase register is a mis-keyed cell, and refusing it keeps every
// later multiplication far inside an int64.
const maxRupees int64 = 100_000_00_00_000

// Rupees formats paise the Indian way — groups of two above the last three
// digits — with two decimal places and no currency symbol, so 1234567890 paise
// becomes "1,23,45,678.90".
func Rupees(paise int64) string {
	neg := paise < 0
	if neg {
		paise = -paise
	}
	whole := paise / RupeePaise
	frac := paise % RupeePaise
	digits := fmt.Sprintf("%d", whole)

	var grouped string
	if len(digits) <= 3 {
		grouped = digits
	} else {
		head, tail := digits[:len(digits)-3], digits[len(digits)-3:]
		var parts []string
		for len(head) > 2 {
			parts = append([]string{head[len(head)-2:]}, parts...)
			head = head[:len(head)-2]
		}
		if head != "" {
			parts = append([]string{head}, parts...)
		}
		grouped = strings.Join(parts, ",") + "," + tail
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%s.%02d", sign, grouped, frac)
}

// Decimal formats paise as a plain machine-readable decimal ("18000.00") for
// CSV columns a spreadsheet or accounting package will read back.
func Decimal(paise int64) string {
	neg := paise < 0
	if neg {
		paise = -paise
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%02d", sign, paise/RupeePaise, paise%RupeePaise)
}

// MulDivRound returns a * num / den rounded half away from zero, computed
// exactly.
//
// This is the proportion arithmetic rule 37(1) calls for — input tax credit
// "proportionate to the amount not paid to the supplier" — so it must not lose
// paise and must not overflow: an invoice value in paise times an ITC figure in
// paise is far beyond an int64, which is why the multiplication is done in
// arbitrary precision and only the result comes back as an int64.
//
// A zero or negative denominator yields zero: there is no proportion of nothing,
// and the caller reports the invoice as unusable rather than guessing.
func MulDivRound(a, num, den int64) int64 {
	if den <= 0 || a == 0 || num == 0 {
		return 0
	}
	if num == den {
		return a
	}
	x := new(big.Int).Mul(big.NewInt(a), big.NewInt(num))
	d := big.NewInt(den)
	q, r := new(big.Int).QuoRem(x, d, new(big.Int))
	// Round half away from zero: compare twice the remainder with the divisor.
	twice := new(big.Int).Abs(r)
	twice.Lsh(twice, 1)
	if twice.Cmp(new(big.Int).Abs(d)) >= 0 {
		if (x.Sign() < 0) != (d.Sign() < 0) {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q.Int64()
}

// CmpRatio compares the fractions aNum/aDen and bNum/bDen exactly, returning
// -1, 0 or +1. Denominators must be positive; a non-positive denominator sorts
// last, since an invoice with nothing left to pay protects nothing per rupee.
//
// It exists so the payment plan can rank invoices by credit protected per rupee
// without ever converting to a float: two invoices whose ratios differ in the
// last paisa must still order deterministically.
func CmpRatio(aNum, aDen, bNum, bDen int64) int {
	switch {
	case aDen <= 0 && bDen <= 0:
		return 0
	case aDen <= 0:
		return -1
	case bDen <= 0:
		return 1
	}
	left := new(big.Int).Mul(big.NewInt(aNum), big.NewInt(bDen))
	right := new(big.Int).Mul(big.NewInt(bNum), big.NewInt(aDen))
	return left.Cmp(right)
}

// RoundToRupee rounds paise to a whole number of rupees, still expressed in
// paise, the way section 170 of the Central Goods and Services Tax Act, 2017
// requires every amount payable under the Act to be rounded: fifty paise or
// more is rounded up to a rupee, less than fifty paise is ignored.
//
// Figures are entered in FORM GSTR-3B in whole rupees, so this is applied to
// the staged reversal and re-claim entries — never to the running totals, which
// stay exact in paise.
func RoundToRupee(paise int64) int64 {
	neg := paise < 0
	if neg {
		paise = -paise
	}
	rupees := (paise + RupeePaise/2) / RupeePaise
	out := rupees * RupeePaise
	if neg {
		return -out
	}
	return out
}

// Heads holds an amount of tax split the way a GST invoice and FORM GSTR-3B
// split it: integrated tax, central tax, State or Union territory tax, and
// cess. Every reversal and re-claim is reported head by head, because the
// return has a separate column for each and the credit ledgers are separate.
type Heads struct {
	IGST int64 `json:"igst"`
	CGST int64 `json:"cgst"`
	SGST int64 `json:"sgst"`
	Cess int64 `json:"cess"`
}

// Total is the tax across all four heads.
func (h Heads) Total() int64 { return h.IGST + h.CGST + h.SGST + h.Cess }

// IsZero reports whether every head is nil.
func (h Heads) IsZero() bool { return h == Heads{} }

// Add returns the head-by-head sum of h and o.
func (h Heads) Add(o Heads) Heads {
	return Heads{IGST: h.IGST + o.IGST, CGST: h.CGST + o.CGST, SGST: h.SGST + o.SGST, Cess: h.Cess + o.Cess}
}

// Sub returns the head-by-head difference h - o.
func (h Heads) Sub(o Heads) Heads {
	return Heads{IGST: h.IGST - o.IGST, CGST: h.CGST - o.CGST, SGST: h.SGST - o.SGST, Cess: h.Cess - o.Cess}
}

// Scale returns the part of h proportionate to num/den, each head rounded
// separately to the nearest paisa.
//
// Each head is scaled on its own because each is a separate figure in the
// return and a separate balance in the electronic credit ledger; the total of
// the scaled heads is therefore the sum of four rounded figures, which can
// differ from the rounded total by a paisa or two. That is the right way round:
// the return is filed head by head.
//
// num == den returns h untouched, so a wholly unpaid invoice reverses exactly
// the credit availed and never a paisa less through rounding.
func (h Heads) Scale(num, den int64) Heads {
	if num == den {
		return h
	}
	if num <= 0 || den <= 0 {
		return Heads{}
	}
	return Heads{
		IGST: MulDivRound(h.IGST, num, den),
		CGST: MulDivRound(h.CGST, num, den),
		SGST: MulDivRound(h.SGST, num, den),
		Cess: MulDivRound(h.Cess, num, den),
	}
}

// RoundToRupee rounds every head to a whole rupee for entry in the return.
func (h Heads) RoundToRupee() Heads {
	return Heads{
		IGST: RoundToRupee(h.IGST),
		CGST: RoundToRupee(h.CGST),
		SGST: RoundToRupee(h.SGST),
		Cess: RoundToRupee(h.Cess),
	}
}

// Min returns the head-by-head lesser of h and o. It caps a re-claim at the
// reversal that was made, so paying an invoice can never restore more credit
// than was given up on it.
func (h Heads) Min(o Heads) Heads {
	return Heads{
		IGST: min64(h.IGST, o.IGST),
		CGST: min64(h.CGST, o.CGST),
		SGST: min64(h.SGST, o.SGST),
		Cess: min64(h.Cess, o.Cess),
	}
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
