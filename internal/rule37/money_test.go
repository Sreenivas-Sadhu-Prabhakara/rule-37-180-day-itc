package rule37

import "testing"

func TestParseAmount(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int64
		bad  bool
	}{
		{name: "plain rupees", in: "100000", want: 10_000_000},
		{name: "two decimals", in: "18000.50", want: 1_800_050},
		{name: "one decimal", in: "18000.5", want: 1_800_050},
		{name: "indian grouping", in: "1,18,000.00", want: 11_800_000},
		{name: "western grouping", in: "118,000.00", want: 11_800_000},
		{name: "rupee sign", in: "₹1,18,000.00", want: 11_800_000},
		{name: "Rs prefix", in: "Rs. 1,18,000", want: 11_800_000},
		{name: "INR prefix", in: "INR 500", want: 50_000},
		{name: "spaces around", in: "  2500.25  ", want: 250_025},
		{name: "non-breaking space", in: "2 500.25", want: 250_025},
		{name: "blank is nil", in: "", want: 0},
		{name: "dash is nil", in: "-", want: 0},
		{name: "double dash is nil", in: "--", want: 0},
		{name: "en dash is nil", in: "–", want: 0},
		{name: "dot only is nil", in: ".", want: 0},
		{name: "accounting negative", in: "(500.00)", want: -50_000},
		{name: "signed negative", in: "-500.00", want: -50_000},
		{name: "signed positive", in: "+500.00", want: 50_000},
		{name: "bracket and sign", in: "(-500.00)", want: 50_000},
		{name: "third decimal rounds up", in: "10.005", want: 1001},
		{name: "third decimal rounds down", in: "10.004", want: 1000},
		{name: "leading dot", in: ".50", want: 50},
		{name: "zero", in: "0", want: 0},
		{name: "zero with decimals", in: "0.00", want: 0},
		{name: "apostrophe grouping", in: "1'18'000", want: 11_800_000},
		{name: "letters are not an amount", in: "abc", bad: true},
		{name: "two decimal points", in: "1.2.3", bad: true},
		{name: "amount with text", in: "100 rupees", bad: true},
		{name: "far too large", in: "99999999999999999999", bad: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseAmount(c.in)
			if c.bad {
				if err == nil {
					t.Fatalf("ParseAmount(%q) = %d, want an error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAmount(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("ParseAmount(%q) = %d paise, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestParseAmountRoundTripsThroughDecimal(t *testing.T) {
	for _, paise := range []int64{0, 1, 99, 100, 12_345, 11_800_000, -50_000, 1_037_288} {
		text := Decimal(paise)
		got, err := ParseAmount(text)
		if err != nil {
			t.Fatalf("ParseAmount(Decimal(%d)) = %q: %v", paise, text, err)
		}
		if got != paise {
			t.Errorf("Decimal(%d) = %q, which parses back as %d", paise, text, got)
		}
	}
}

func TestRupees(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{50, "0.50"},
		{100, "1.00"},
		{99_999, "999.99"},
		{100_000, "1,000.00"},
		{10_000_000, "1,00,000.00"},
		{1_234_567_890, "1,23,45,678.90"},
		{-11_800_000, "-1,18,000.00"},
		{1_037_288, "10,372.88"},
	}
	for _, c := range cases {
		if got := Rupees(c.in); got != c.want {
			t.Errorf("Rupees(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDecimal(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0.00"},
		{7, "0.07"},
		{1_800_000, "18000.00"},
		{-1_800_050, "-18000.50"},
	}
	for _, c := range cases {
		if got := Decimal(c.in); got != c.want {
			t.Errorf("Decimal(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMulDivRound(t *testing.T) {
	cases := []struct {
		name        string
		a, num, den int64
		want        int64
	}{
		{name: "whole proportion", a: 1_800_000, num: 11_800_000, den: 11_800_000, want: 1_800_000},
		{name: "exact half rounds away from zero", a: 3, num: 1, den: 2, want: 2},
		{name: "one half rounds up", a: 1, num: 1, den: 2, want: 1},
		{name: "negative half rounds away from zero", a: -3, num: 1, den: 2, want: -2},
		{name: "two thirds rounds up", a: 5, num: 1, den: 3, want: 2},
		{name: "one third rounds down", a: 1, num: 1, den: 3, want: 0},
		{name: "half the credit", a: 1_800_000, num: 5_900_000, den: 11_800_000, want: 900_000},
		{name: "the worked example", a: 1_800_000, num: 6_800_000, den: 11_800_000, want: 1_037_288},
		{name: "zero numerator", a: 1_800_000, num: 0, den: 11_800_000, want: 0},
		{name: "zero amount", a: 0, num: 5, den: 7, want: 0},
		{name: "zero denominator is nothing", a: 1_800_000, num: 5, den: 0, want: 0},
		{name: "negative denominator is nothing", a: 1_800_000, num: 5, den: -2, want: 0},
		// A product of two amounts in paise overflows an int64; this must not.
		{name: "no overflow on huge amounts", a: 1_800_000_000_000, num: 6_800_000_000_000, den: 11_800_000_000_000, want: 1_037_288_135_593},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MulDivRound(c.a, c.num, c.den); got != c.want {
				t.Errorf("MulDivRound(%d, %d, %d) = %d, want %d", c.a, c.num, c.den, got, c.want)
			}
		})
	}
}

func TestRoundToRupee(t *testing.T) {
	cases := []struct{ in, want int64 }{
		{0, 0},
		{1, 0},
		{49, 0},
		{50, 100},
		{51, 100},
		{100, 100},
		{149, 100},
		{150, 200},
		{1_037_288, 1_037_300},
		{1_037_250, 1_037_300},
		{1_037_249, 1_037_200},
		{-49, 0},
		{-50, -100},
		{-150, -200},
	}
	for _, c := range cases {
		if got := RoundToRupee(c.in); got != c.want {
			t.Errorf("RoundToRupee(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestCmpRatio(t *testing.T) {
	cases := []struct {
		name                   string
		aNum, aDen, bNum, bDen int64
		want                   int
	}{
		{name: "28 per cent beats 18 per cent", aNum: 28, aDen: 128, bNum: 18, bDen: 118, want: 1},
		{name: "18 per cent loses to 28", aNum: 18, aDen: 118, bNum: 28, bDen: 128, want: -1},
		{name: "same rate is a tie", aNum: 1_800_000, aDen: 11_800_000, bNum: 180_000, bDen: 1_180_000, want: 0},
		{name: "a paisa decides it", aNum: 1_800_001, aDen: 11_800_000, bNum: 1_800_000, bDen: 11_800_000, want: 1},
		{name: "nothing to pay sorts last", aNum: 100, aDen: 0, bNum: 1, bDen: 1_000_000, want: -1},
		{name: "both with nothing to pay tie", aNum: 100, aDen: 0, bNum: 50, bDen: 0, want: 0},
		// Products of two paise amounts overflow an int64; the comparison must not.
		{name: "no overflow comparing huge amounts", aNum: 1_800_000_000_000, aDen: 11_800_000_000_000, bNum: 2_800_000_000_000, bDen: 12_800_000_000_000, want: -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CmpRatio(c.aNum, c.aDen, c.bNum, c.bDen); got != c.want {
				t.Errorf("CmpRatio(%d/%d, %d/%d) = %d, want %d", c.aNum, c.aDen, c.bNum, c.bDen, got, c.want)
			}
		})
	}
}

func TestHeadsArithmetic(t *testing.T) {
	a := Heads{IGST: 100, CGST: 200, SGST: 300, Cess: 400}
	b := Heads{IGST: 1, CGST: 2, SGST: 3, Cess: 4}

	if got, want := a.Total(), int64(1000); got != want {
		t.Errorf("Total() = %d, want %d", got, want)
	}
	if got, want := a.Add(b), (Heads{IGST: 101, CGST: 202, SGST: 303, Cess: 404}); got != want {
		t.Errorf("Add() = %+v, want %+v", got, want)
	}
	if got, want := a.Sub(b), (Heads{IGST: 99, CGST: 198, SGST: 297, Cess: 396}); got != want {
		t.Errorf("Sub() = %+v, want %+v", got, want)
	}
	if got, want := a.Min(Heads{IGST: 150, CGST: 150, SGST: 150, Cess: 150}), (Heads{IGST: 100, CGST: 150, SGST: 150, Cess: 150}); got != want {
		t.Errorf("Min() = %+v, want %+v", got, want)
	}
	if !(Heads{}).IsZero() {
		t.Error("the zero Heads should report IsZero")
	}
	if b.IsZero() {
		t.Error("a non-zero Heads should not report IsZero")
	}
}

func TestHeadsScale(t *testing.T) {
	// The worked example: ₹1,18,000 invoice carrying ₹18,000 of credit, of
	// which ₹50,000 has been paid, so ₹68,000 is unpaid.
	itc := Heads{IGST: 1_800_000}
	got := itc.Scale(6_800_000, 11_800_000)
	if want := int64(1_037_288); got.IGST != want {
		t.Errorf("proportionate IGST = %d, want %d", got.IGST, want)
	}

	// Split between central and State tax, the two heads come to the same.
	split := Heads{CGST: 900_000, SGST: 900_000}
	gotSplit := split.Scale(6_800_000, 11_800_000)
	if gotSplit.CGST != 518_644 || gotSplit.SGST != 518_644 {
		t.Errorf("split heads = %+v, want 518644 each", gotSplit)
	}
	if gotSplit.Total() != got.IGST {
		t.Errorf("split total %d should match the single-head total %d", gotSplit.Total(), got.IGST)
	}

	// A wholly unpaid invoice reverses exactly the credit availed, with no
	// rounding drift in either direction.
	for _, n := range []int64{1, 7, 1_234_567, 99_999_999} {
		full := Heads{IGST: n, CGST: n + 1, SGST: n + 2, Cess: n + 3}
		if got := full.Scale(11_800_000, 11_800_000); got != full {
			t.Errorf("Scale(x, x) on %+v = %+v, want it untouched", full, got)
		}
	}

	// Nothing unpaid, or a nil invoice value, scales to nothing rather than
	// dividing by zero.
	if got := itc.Scale(0, 11_800_000); !got.IsZero() {
		t.Errorf("Scale(0, den) = %+v, want nil", got)
	}
	if got := itc.Scale(5, 0); !got.IsZero() {
		t.Errorf("Scale(num, 0) = %+v, want nil", got)
	}

	// Each head is rounded separately, because each is its own figure in the
	// return. One paisa split across two heads therefore scales to two paise,
	// not to one: the documented consequence of rounding head by head.
	tiny := Heads{CGST: 1, SGST: 1}
	half := tiny.Scale(1, 2)
	if half.CGST != 1 || half.SGST != 1 {
		t.Errorf("Scale(1,2) on one paisa per head = %+v, want 1 each", half)
	}
	if half.Total() != 2 {
		t.Errorf("heads total = %d, want 2 (each head rounded on its own)", half.Total())
	}
	if MulDivRound(tiny.Total(), 1, 2) != 1 {
		t.Error("scaling the total instead would give 1, which is why the heads are scaled separately")
	}
}

func TestHeadsRoundToRupee(t *testing.T) {
	in := Heads{IGST: 1_037_288, CGST: 49, SGST: 50, Cess: 0}
	want := Heads{IGST: 1_037_300, CGST: 0, SGST: 100, Cess: 0}
	if got := in.RoundToRupee(); got != want {
		t.Errorf("RoundToRupee() = %+v, want %+v", got, want)
	}
}
