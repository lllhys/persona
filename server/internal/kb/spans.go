package kb

import (
	"cmp"
	"math"
	"slices"
)

// Spans of recording seconds ([from, to], from <= to): the audio the hub holds, the gaps it lacks, the spans Persona
// spoke in, the time a set of sentences covers (docs/assistant/meeting-minutes.md "Phase 5: hub transcription").

// gapJoin is how close two gaps may come before they count as one (the held audio between them is shorter).
const gapJoin = 0.1

// UnionSpans is the union of spans: sorted, with overlapping and touching ones made one. A span that is not one (a
// number that is not finite, from after to) is left out.
func UnionSpans(spans [][2]float64) [][2]float64 {
	var out [][2]float64
	for _, s := range spans {
		if finite(s[0]) && finite(s[1]) && s[0] <= s[1] {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b [2]float64) int {
		if c := cmp.Compare(a[0], b[0]); c != 0 {
			return c
		}
		return cmp.Compare(a[1], b[1])
	})
	merged := out[:0]
	for _, s := range out {
		if n := len(merged); n > 0 && s[0] <= merged[n-1][1] {
			merged[n-1][1] = max(merged[n-1][1], s[1])
			continue
		}
		merged = append(merged, s)
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// CoveredShare is the share of [start, end] inside the union of by: 0 to 1. Spans that overlap count once, so several
// short sentences that each cover a little count together. A span of no length is covered (1) when it lies in one of
// by, else not (0).
func CoveredShare(start, end float64, by [][2]float64) float64 {
	return coveredShare(start, end, UnionSpans(by))
}

// coveredShare is CoveredShare with by a union already (UnionSpans): sorted and apart, so it finds the spans by
// bisection.
func coveredShare(start, end float64, union [][2]float64) float64 {
	if !finite(start) || !finite(end) {
		return 0
	}
	if end < start {
		start, end = end, start
	}
	// The first span that ends at or after start; those before it cover nothing of [start, end].
	i, _ := slices.BinarySearchFunc(union, start, func(s [2]float64, t float64) int { return cmp.Compare(s[1], t) })
	union = union[i:]
	if end == start {
		if len(union) > 0 && union[0][0] <= start {
			return 1
		}
		return 0
	}
	covered := 0.0
	for _, s := range union {
		if s[0] >= end {
			break
		}
		if lo, hi := max(s[0], start), min(s[1], end); hi > lo {
			covered += hi - lo
		}
	}
	return min(1, covered/(end-start))
}

// GapsIn are the spans of [from, to] that held does not cover, merged when less than 0.1 s apart (the held audio
// between them is shorter than that): what the hub lacks of a recording's audio. A gap shorter than half a sample is
// rounding, not a gap.
func GapsIn(held [][2]float64, from, to float64) [][2]float64 {
	if !finite(from) || !finite(to) || to <= from {
		return nil
	}
	var gaps [][2]float64
	at := from
	for _, s := range UnionSpans(held) {
		if s[1] <= at {
			continue
		}
		if s[0] >= to {
			break
		}
		if s[0] > at {
			gaps = append(gaps, [2]float64{at, s[0]})
		}
		at = max(at, s[1])
	}
	if at < to {
		gaps = append(gaps, [2]float64{at, to})
	}
	out := gaps[:0]
	for _, g := range gaps {
		if g[1]-g[0] < 0.5/AudioRate {
			continue
		}
		if n := len(out); n > 0 && g[0]-out[n-1][1] < gapJoin {
			out[n-1][1] = g[1]
			continue
		}
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sameSpans: a and b are the same spans (nil and empty alike).
func sameSpans(a, b [][2]float64) bool {
	return len(a) == len(b) && (len(a) == 0 || slices.Equal(a, b))
}

// atSample is t seconds rounded to a sample of the hub's audio (1/AudioRate s); -1 for a time that is not one.
func atSample(t float64) int64 {
	if !finite(t) || t < 0 {
		return -1
	}
	return int64(math.Round(t * AudioRate))
}

// sampleAt is sample n in seconds.
func sampleAt(n int64) float64 { return float64(n) / AudioRate }
