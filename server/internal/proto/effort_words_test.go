package proto

import (
	"slices"
	"testing"
)

// A gateway's levels in another list's words: kept where that list names them, minimal and ultra moved to low and max
// where it doesn't, the rest left out; weakest first, each once.
func TestProjectEfforts(t *testing.T) {
	for _, tc := range []struct {
		levels, vocab, want []string
	}{
		{[]string{"none", "minimal", "low", "high", "ultra"}, EffortLevels, []string{"low", "high", "max"}},
		{[]string{"ultra", "minimal", "max"}, EffortLevels, []string{"low", "max"}},
		{[]string{"none", "minimal", "ultra"}, CodexEffortLevels, []string{"minimal", "ultra"}},
		{[]string{"none", "minimal", "xhigh"}, append([]string{"none"}, EffortLevels...), []string{"none", "low", "xhigh"}},
		{[]string{"none"}, EffortLevels, nil},
		{[]string{"light", "High"}, EffortLevels, nil},
		{nil, CodexEffortLevels, nil},
	} {
		if got := ProjectEfforts(tc.levels, tc.vocab); !slices.Equal(got, tc.want) {
			t.Errorf("%v in %v: %v, want %v", tc.levels, tc.vocab, got, tc.want)
		}
	}
	for _, list := range [][]string{EffortLevels, CodexEffortLevels} {
		if !slices.Equal(ProjectEfforts(list, EffortWords), list) {
			t.Errorf("%v is not in EffortWords' order", list)
		}
	}
}
