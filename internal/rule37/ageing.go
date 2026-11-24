package rule37

import "fmt"

// Bucket is one ageing band, measured in days elapsed since the invoice date.
//
// The ageing is deliberately counted from the invoice date and not from a due
// date agreed with the supplier, because that is where the 180 days of the
// second proviso to section 16(2) are counted from. An invoice can be well
// within the credit period the supplier allowed and still be about to cost its
// buyer the input tax credit.
type Bucket struct {
	Index int    `json:"index"`
	From  int    `json:"from"`  // first day of the band, inclusive
	To    int    `json:"to"`    // last day of the band, inclusive; -1 when open-ended
	Label string `json:"label"` // e.g. "121–150 days" or "Over 180 days"
}

// Open reports whether the band has no upper bound.
func (b Bucket) Open() bool { return b.To < 0 }

// Contains reports whether a number of days elapsed falls in the band. Days
// before the invoice date fall in the first band, which is where an invoice
// dated in the future belongs: its clock has not started.
func (b Bucket) Contains(days int) bool {
	if days < b.From {
		// A negative count means the invoice is dated in the future, so its
		// clock has not started: it belongs in the first band.
		return b.From == 0
	}
	return b.Open() || days <= b.To
}

// Buckets builds the ageing bands from the edges in Settings. Each edge is the
// last day of its band, and a final open-ended band is added past the last
// edge, so {60, 120, 150, 180} gives 0–60, 61–120, 121–150, 151–180 and
// "Over 180 days".
func Buckets(edges []int) []Bucket {
	edges = cleanEdges(edges, DefaultBucketEdges)
	out := make([]Bucket, 0, len(edges)+1)
	from := 0
	for i, e := range edges {
		out = append(out, Bucket{Index: i, From: from, To: e, Label: fmt.Sprintf("%d–%d days", from, e)})
		from = e + 1
	}
	last := edges[len(edges)-1]
	out = append(out, Bucket{Index: len(edges), From: from, To: -1, Label: fmt.Sprintf("Over %d days", last)})
	return out
}

// BucketFor returns the band a number of days elapsed falls in.
func BucketFor(days int, edges []int) Bucket {
	bands := Buckets(edges)
	for _, b := range bands {
		if b.Contains(days) {
			return b
		}
	}
	return bands[len(bands)-1]
}

// BucketTotal is one ageing band with the invoices in it added up.
type BucketTotal struct {
	Bucket   Bucket `json:"bucket"`
	Invoices int    `json:"invoices"`
	Unpaid   int64  `json:"unpaid"`

	// AtRisk is the credit on invoices in this band still inside their window.
	AtRisk Heads `json:"atRisk"`
	// Reversal is the credit on invoices in this band whose window has closed.
	Reversal Heads `json:"reversal"`

	// Earliest and Latest are the first and last payment deadlines in the band,
	// so the band can be read as a date range as well as a day count.
	Earliest Day `json:"earliest"`
	Latest   Day `json:"latest"`
}

// Exposure is all the credit riding on the band, whether still at risk or
// already fallen due for reversal.
func (b BucketTotal) Exposure() int64 { return b.AtRisk.Total() + b.Reversal.Total() }

// Ageing groups assessments into the ageing bands, counting only invoices that
// still need something done about them. Bands with nothing in them are kept, so
// the shape of the ageing is visible and the table does not jump about between
// runs.
func Ageing(assessed []Assessment, edges []int) []BucketTotal {
	bands := Buckets(edges)
	out := make([]BucketTotal, len(bands))
	for i, b := range bands {
		out[i] = BucketTotal{Bucket: b}
	}
	for _, a := range assessed {
		if !a.NeedsAction() {
			continue
		}
		i := a.Bucket.Index
		if i < 0 || i >= len(out) {
			i = len(out) - 1
		}
		t := &out[i]
		t.Invoices++
		t.Unpaid += a.UnpaidToDate
		t.AtRisk = t.AtRisk.Add(a.AtRisk)
		t.Reversal = t.Reversal.Add(a.Reversal)
		if !a.Deadline.IsZero() {
			if t.Earliest.IsZero() || a.Deadline.Before(t.Earliest) {
				t.Earliest = a.Deadline
			}
			if t.Latest.IsZero() || a.Deadline.After(t.Latest) {
				t.Latest = a.Deadline
			}
		}
	}
	return out
}
