package transcript

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"
)

// naiveInputClipNode is deliberately independent of the production size index and heaps. The reference below re-encodes
// the whole value and walks only attached children before every removal.
type naiveInputClipNode struct {
	value any
	kind  byte
	key   string
	kids  []*naiveInputClipNode
	fixed int
	order int
}

func buildNaiveInputClip(v any, order *int) *naiveInputClipNode {
	n := &naiveInputClipNode{value: v}
	switch v := v.(type) {
	case []any:
		n.kind = '['
		n.order = *order
		*order++
		for _, e := range v {
			n.kids = append(n.kids, buildNaiveInputClip(e, order))
		}
	case map[string]any:
		n.kind = '{'
		n.order = *order
		*order++
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := buildNaiveInputClip(v[k], order)
			child.key = k
			n.kids = append(n.kids, child)
		}
	}
	return n
}

func (n *naiveInputClipNode) out() any {
	switch n.kind {
	case '[':
		out := make([]any, len(n.kids))
		for i, child := range n.kids {
			out[i] = child.out()
		}
		return out
	case '{':
		out := make(map[string]any, len(n.kids))
		for _, child := range n.kids {
			out[child.key] = child.out()
		}
		return out
	default:
		return n.value
	}
}

func (n *naiveInputClipNode) collect(arrays, objects *[]*naiveInputClipNode) {
	if n.kind == '[' {
		*arrays = append(*arrays, n)
	} else if n.kind == '{' {
		*objects = append(*objects, n)
	}
	for _, child := range n.kids {
		child.collect(arrays, objects)
	}
}

func naiveInputClipRemaining(n *naiveInputClipNode) int { return len(n.kids) - n.fixed }

func naiveInputClipBest(nodes []*naiveInputClipNode) *naiveInputClipNode {
	var best *naiveInputClipNode
	for _, n := range nodes {
		if naiveInputClipRemaining(n) == 0 {
			continue
		}
		if best == nil || naiveInputClipRemaining(n) > naiveInputClipRemaining(best) ||
			(naiveInputClipRemaining(n) == naiveInputClipRemaining(best) && n.order < best.order) {
			best = n
		}
	}
	return best
}

func naiveBoundInputJSON(candidate []byte, originalSize, budget int) []byte {
	value, err := decodeInputObject(candidate)
	if err != nil {
		return candidate
	}
	order := 0
	root := buildNaiveInputClip(value, &order)
	markerKey := inputClipMarker
	for suffix := 2; ; suffix++ {
		if _, exists := value[markerKey]; !exists {
			break
		}
		markerKey = fmt.Sprintf("%s_%d", inputClipMarker, suffix)
	}
	marker := &naiveInputClipNode{value: inputClipMarkerValue(originalSize, 0, 0), key: markerKey}
	root.kids = append(root.kids, marker)
	root.fixed = 1
	arrayItems, objectFields := 0, 0
	for steps := 0; steps < 1_000_000; steps++ {
		if len(marshalPlain(root.out())) <= budget {
			break
		}
		var arrays, objects []*naiveInputClipNode
		root.collect(&arrays, &objects)
		target := naiveInputClipBest(arrays)
		if target == nil {
			target = naiveInputClipBest(objects)
		}
		if target == nil {
			break
		}
		last := len(target.kids) - 1 - target.fixed
		target.kids = append(target.kids[:last], target.kids[last+1:]...)
		if target.kind == '[' {
			arrayItems++
		} else {
			objectFields++
		}
		marker.value = inputClipMarkerValue(originalSize, arrayItems, objectFields)
	}
	return marshalPlain(root.out())
}

func smallRandomClipValue(r *rand.Rand, depth int) any {
	if depth == 0 || r.IntN(3) == 0 {
		switch r.IntN(5) {
		case 0:
			return strings.Repeat("x", r.IntN(30))
		case 1:
			return "<>&\u2028\u2029"
		case 2:
			return r.IntN(100)
		case 3:
			return r.IntN(2) == 0
		default:
			return nil
		}
	}
	if r.IntN(2) == 0 {
		out := make([]any, r.IntN(5))
		for i := range out {
			out[i] = smallRandomClipValue(r, depth-1)
		}
		return out
	}
	out := map[string]any{}
	for i := 0; i < r.IntN(5); i++ {
		out[fmt.Sprintf("k%d", i)] = smallRandomClipValue(r, depth-1)
	}
	return out
}

func TestClipInputMatchesNaiveReference(t *testing.T) {
	r := rand.New(rand.NewPCG(2907, 5))
	for i := 0; i < 80; i++ {
		root := map[string]any{}
		for j := 0; j < 6; j++ {
			root[fmt.Sprintf("field_%d", j)] = smallRandomClipValue(r, 3)
		}
		root["values"] = smallRandomClipValue(r, 3)
		candidate := marshalPlain(root)
		for _, budget := range []int{256, 512, 1024} {
			if len(candidate) <= budget {
				continue
			}
			got := boundInputJSONBudget(candidate, len(candidate), budget)
			want := naiveBoundInputJSON(candidate, len(candidate), budget)
			if !bytes.Equal(got, want) {
				t.Fatalf("case %d budget %d:\n got %s\nwant %s", i, budget, got, want)
			}
			if len(got) > budget {
				t.Fatalf("case %d budget %d returned %d bytes", i, budget, len(got))
			}
		}
	}
}

func TestClipInputNaiveReferencePinsNestedOrder(t *testing.T) {
	candidate := []byte(`{"a":[0,0,[1,2,3]],"b":[4,5,6]}`)
	found := false
	for budget := 100; budget <= 400; budget++ {
		got := boundInputJSONBudget(candidate, len(candidate), budget)
		if want := naiveBoundInputJSON(candidate, len(candidate), budget); !bytes.Equal(got, want) {
			t.Fatalf("budget %d:\n got %s\nwant %s", budget, got, want)
		}
		decoded, err := decodeInputObject(got)
		if err != nil {
			t.Fatal(err)
		}
		arrayItems, _ := inputClipMarkerCounts(t, inputClipMarkerNote(t, decoded[inputClipMarker]))
		if arrayItems != 3 {
			continue
		}
		found = true
		a := inputClipTestArray(t, decoded["a"])
		b := inputClipTestArray(t, decoded["b"])
		if len(a) != 1 || a[0] != float64(0) || len(b) != 2 || b[0] != float64(4) || b[1] != float64(5) {
			t.Fatalf("budget %d retained a=%v b=%v after three direct removals", budget, a, b)
		}
	}
	if !found {
		t.Fatal("no budget exercised exactly three nested-array removals")
	}
}
