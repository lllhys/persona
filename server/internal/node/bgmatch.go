package node

import (
	"slices"
	"sync"

	"github.com/nwverse/persona/server/internal/transcript"
)

// bgMatcher finds whether a line holds any of scanBackground's markers in one pass over it: an Aho-Corasick automaton
// turned into a table, one lookup per byte. Testing each marker in turn (containsAny) read every line 28 times, and on a
// node's first list, which scans each listed transcript whole, that was most of the CPU it spent (docs/lessons/agents.md).
type bgMatcher struct {
	// class numbers the bytes the markers use from 1; every other byte is class 0, which leads where any foreign byte does.
	class  [256]int32
	stride int
	// next is the table: entry state+class is the state after that byte, a state being its row's offset (row*stride);
	// bgMatched once a marker ends there.
	next   []int32
	always bool // an empty marker: every line matches
}

const bgMatched = -1

// bgMatch is the matcher for the static markers, built on the first scan, not at start: the same binary runs as
// `personad hook` on each Claude Code event (docs/lessons/agents.md, "Package-level work").
var bgMatch = sync.OnceValue(func() *bgMatcher {
	return newBGMatcher(slices.Concat(bgMarkers, transcript.GoalMarkers, transcript.UltracodeMarkers, transcript.PlanMarkers))
})

func newBGMatcher(markers [][]byte) *bgMatcher {
	m := &bgMatcher{}
	classes := 1
	for _, k := range markers {
		if len(k) == 0 {
			m.always = true
		}
		for _, c := range k {
			if m.class[c] == 0 {
				m.class[c] = int32(classes)
				classes++
			}
		}
	}
	m.stride = classes
	// The trie of the markers: goto_[state][class], 0 for no edge (the root is never an edge's target).
	goto_ := [][]int32{make([]int32, classes)}
	end := []bool{false}
	for _, k := range markers {
		s := int32(0)
		for _, c := range k {
			c := m.class[c]
			if goto_[s][c] == 0 {
				goto_ = append(goto_, make([]int32, classes))
				end = append(end, false)
				goto_[s][c] = int32(len(goto_) - 1)
			}
			s = goto_[s][c]
		}
		end[s] = true
	}
	// Breadth first, each state's missing edges follow its failure link (the longest proper suffix that is a trie
	// state), and a state ends a marker when its failure state does.
	delta := make([][]int32, len(goto_))
	fail := make([]int32, len(goto_))
	delta[0] = slices.Clone(goto_[0])
	queue := make([]int32, 0, len(goto_))
	for _, t := range goto_[0] {
		if t != 0 {
			queue = append(queue, t)
		}
	}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		end[s] = end[s] || end[fail[s]]
		delta[s] = make([]int32, classes)
		for c, t := range goto_[s] {
			if t != 0 {
				fail[t] = delta[fail[s]][c]
				delta[s][c] = t
				queue = append(queue, t)
			} else {
				delta[s][c] = delta[fail[s]][c]
			}
		}
	}
	m.next = make([]int32, len(delta)*classes)
	for s, row := range delta {
		for c, t := range row {
			if end[t] {
				m.next[s*classes+c] = bgMatched
			} else {
				m.next[s*classes+c] = t * int32(classes)
			}
		}
	}
	return m
}

// match: line holds one of the markers (what containsAny says of it with all of them).
func (m *bgMatcher) match(line []byte) bool {
	if m.always {
		return true
	}
	next, class := m.next, &m.class
	s := int32(0)
	for _, c := range line {
		s = next[s+class[c]]
		if s < 0 {
			return true
		}
	}
	return false
}
