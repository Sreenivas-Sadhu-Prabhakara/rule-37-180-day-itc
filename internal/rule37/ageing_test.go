package rule37

import "testing"

func TestBuckets(t *testing.T) {
	bands := Buckets(DefaultBucketEdges)
	want := []struct {
		from, to int
		label    string
	}{
		{0, 60, "0–60 days"},
		{61, 120, "61–120 days"},
		{121, 150, "121–150 days"},
		{151, 180, "151–180 days"},
		{181, -1, "Over 180 days"},
	}
	if len(bands) != len(want) {
		t.Fatalf("got %d bands, want %d: %+v", len(bands), len(want), bands)
	}
	for i, w := range want {
		b := bands[i]
		if b.Index != i || b.From != w.from || b.To != w.to || b.Label != w.label {
			t.Errorf("band %d = %+v, want from %d to %d labelled %q", i, b, w.from, w.to, w.label)
		}
	}
	if !bands[len(bands)-1].Open() {
		t.Error("the last band should be open-ended")
	}
	if bands[0].Open() {
		t.Error("the first band should not be open-ended")
	}
}

func TestBucketForBoundaries(t *testing.T) {
	// The boundaries are where an ageing report goes wrong, so every one of
	// them is checked from both sides.
	cases := []struct {
		days      int
		wantIndex int
	}{
		{-30, 0}, // an invoice dated in the future has not started its clock
		{-1, 0},
		{0, 0},
		{1, 0},
		{60, 0},
		{61, 1},
		{119, 1},
		{120, 1},
		{121, 2},
		{149, 2},
		{150, 2},
		{151, 3},
		{179, 3},
		{180, 3}, // the last day of the window is still inside it
		{181, 4}, // and the day after is past it
		{5000, 4},
	}
	for _, c := range cases {
		if got := BucketFor(c.days, DefaultBucketEdges); got.Index != c.wantIndex {
			t.Errorf("%d days elapsed lands in band %d (%s), want band %d", c.days, got.Index, got.Label, c.wantIndex)
		}
	}
}

func TestBucketsWithCustomEdges(t *testing.T) {
	bands := Buckets([]int{30, 90})
	if len(bands) != 3 {
		t.Fatalf("got %d bands, want 3: %+v", len(bands), bands)
	}
	if bands[0].Label != "0–30 days" || bands[1].Label != "31–90 days" || bands[2].Label != "Over 90 days" {
		t.Errorf("labels = %q, %q, %q", bands[0].Label, bands[1].Label, bands[2].Label)
	}

	// Edges arrive from a form in any order, with duplicates and nonsense.
	// They are sorted, de-duplicated and cleaned without complaint.
	messy := Buckets([]int{90, 30, 30, -5, 0, 90})
	if len(messy) != 3 {
		t.Fatalf("messy edges gave %d bands, want 3: %+v", len(messy), messy)
	}
	if messy[0].To != 30 || messy[1].To != 90 {
		t.Errorf("messy edges sorted to %+v", messy)
	}

	// Nothing usable falls back to the statutory shape rather than producing a
	// single band covering everything.
	fallback := Buckets([]int{0, -1})
	if len(fallback) != len(DefaultBucketEdges)+1 {
		t.Errorf("empty edges gave %d bands, want the default %d", len(fallback), len(DefaultBucketEdges)+1)
	}
	if got := Buckets(nil); len(got) != len(DefaultBucketEdges)+1 {
		t.Errorf("nil edges gave %d bands, want the default", len(got))
	}
}

func TestAgeing(t *testing.T) {
	set := DefaultSettings()
	lines := AssessAll(testPortfolio(t), mustDay(t, "2027-02-01"), set)
	bands := Ageing(collectAssessments(lines), set.BucketEdges)

	if len(bands) != 5 {
		t.Fatalf("got %d bands, want 5", len(bands))
	}
	wantCounts := []int{0, 2, 0, 1, 1}
	for i, want := range wantCounts {
		if got := bands[i].Invoices; got != want {
			t.Errorf("band %q holds %d invoices, want %d", bands[i].Bucket.Label, got, want)
		}
	}

	// The 61–120 day band holds the two invoices still inside their window,
	// one of them exactly on the 120-day boundary.
	mid := bands[1]
	if got, want := mid.AtRisk.Total(), int64(2_300_000); got != want {
		t.Errorf("credit at risk in %q = %s, want %s", mid.Bucket.Label, Rupees(got), Rupees(want))
	}
	if !mid.Reversal.IsZero() {
		t.Errorf("nothing in %q has passed its deadline, so nothing should be reversing", mid.Bucket.Label)
	}
	if got, want := mid.Unpaid, int64(22_300_000); got != want {
		t.Errorf("unpaid in %q = %s, want %s", mid.Bucket.Label, Rupees(got), Rupees(want))
	}
	if got, want := mid.Earliest.String(), "2027-04-02"; got != want {
		t.Errorf("earliest deadline in %q = %s, want %s", mid.Bucket.Label, got, want)
	}
	if got, want := mid.Latest.String(), "2027-05-23"; got != want {
		t.Errorf("latest deadline in %q = %s, want %s", mid.Bucket.Label, got, want)
	}
	if got, want := mid.Exposure(), int64(2_300_000); got != want {
		t.Errorf("exposure in %q = %s, want %s", mid.Bucket.Label, Rupees(got), Rupees(want))
	}

	// Everything past 180 days is a reversal, not a risk: paying no longer
	// prevents it.
	over := bands[4]
	if got, want := over.Reversal.Total(), int64(1_800_000); got != want {
		t.Errorf("reversal in %q = %s, want %s", over.Bucket.Label, Rupees(got), Rupees(want))
	}
	if !over.AtRisk.IsZero() {
		t.Errorf("nothing in %q can still be protected by paying", over.Bucket.Label)
	}

	// Settled and out-of-scope invoices are not in the ageing at all: it is a
	// list of things to do.
	var total int
	for _, b := range bands {
		total += b.Invoices
	}
	if total != 4 {
		t.Errorf("the ageing holds %d invoices, want the 4 that need action", total)
	}

	// An empty ageing keeps its shape, so the table does not jump about.
	empty := Ageing(nil, set.BucketEdges)
	if len(empty) != 5 {
		t.Errorf("an empty ageing has %d bands, want 5", len(empty))
	}
	for _, b := range empty {
		if b.Invoices != 0 || !b.Earliest.IsZero() {
			t.Errorf("band %q is not empty: %+v", b.Bucket.Label, b)
		}
	}
}

func collectAssessments(lines []Line) []Assessment {
	out := make([]Assessment, 0, len(lines))
	for _, ln := range lines {
		out = append(out, ln.Assessment)
	}
	return out
}
