package kb

import (
	"math"
	"testing"
)

func TestUnionSpans(t *testing.T) {
	got := UnionSpans([][2]float64{{5, 6}, {1, 2}, {2, 3}, {1.5, 2.5}, {8, 8}, {4, 3}, {math.NaN(), 1}, {9, math.Inf(1)}})
	if want := [][2]float64{{1, 3}, {5, 6}, {8, 8}}; !sameSpans(got, want) {
		t.Fatalf("union %v, want %v", got, want)
	}
	if UnionSpans(nil) != nil || UnionSpans([][2]float64{{2, 1}}) != nil {
		t.Fatal("no spans")
	}
}

// CoveredShare counts the union: several short sentences that each cover a little of a segment count together, and
// overlapping ones once.
func TestCoveredShare(t *testing.T) {
	short := [][2]float64{{0, 2}, {3, 5}, {6, 8}, {1, 2.5}} // 0-2.5, 3-5, 6-8 of a 0-10 segment: 6.5 s
	for _, c := range []struct {
		name       string
		start, end float64
		by         [][2]float64
		want       float64
	}{
		{"together", 0, 10, short, 0.65},
		{"none", 20, 30, short, 0},
		{"all", 3.5, 4.5, short, 1},
		{"partly", 4, 7, short, 2.0 / 3},
		{"no spans", 0, 10, nil, 0},
		{"a moment inside", 4, 4, short, 1},
		{"a moment on an edge", 5, 5, short, 1},
		{"a moment outside", 5.5, 5.5, short, 0},
		{"backwards", 10, 0, short, 0.65},
		{"not finite", math.NaN(), 1, short, 0},
	} {
		if got := CoveredShare(c.start, c.end, c.by); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// GapsIn: what held lacks of [from, to], gaps closer than 0.1 s made one (the audio between them is too short), less
// than half a sample none.
func TestGapsIn(t *testing.T) {
	held := [][2]float64{{0, 10}, {12, 20}, {20.05, 30}, {30, 40}, {40.00001, 50}}
	for _, c := range []struct {
		name     string
		held     [][2]float64
		from, to float64
		want     [][2]float64
	}{
		{"holes", held, 0, 60, [][2]float64{{10, 12}, {20, 20.05}, {50, 60}}},
		{"within", held, 11, 15, [][2]float64{{11, 12}}},
		{"all held", held, 0, 10, nil},
		{"nothing held", nil, 0, 5, [][2]float64{{0, 5}}},
		{"empty range", held, 5, 5, nil},
	} {
		if got := GapsIn(c.held, c.from, c.to); !sameSpans(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// Two gaps 0.05 s apart are one; 0.2 s apart two.
	if got := GapsIn([][2]float64{{0, 1}, {2, 2.05}, {3, 3.2}, {4, 5}}, 0, 5); !sameSpans(got, [][2]float64{{1, 3}, {3.2, 4}}) {
		t.Fatalf("joined %v", got)
	}
}
