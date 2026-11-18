package transcript

import (
	containerheap "container/heap"
	"fmt"
	"sort"
)

// inputClipMarker is an object, not a string, so no client's "first string argument" subject fallback can turn the
// structural clipping note into the call's title. Generic JSON views still show it, and recursive cut detectors find note.
const inputClipMarker = "zz_persona_clipped"

// inputClipNode indexes one value in the JSON produced by the string passes. size is the encoded size of a scalar (or of
// the fixed marker object). Object children retain the candidate's sorted-key order; the root marker is appended last and
// is the one fixed child. Container sizes are summed only when their subtree is detached, never along every ancestor.
type inputClipNode struct {
	kind    byte // '[', '{', or 0 for a scalar (the marker object is scalar to the index)
	value   any  // scalar value; containers rebuild their value from children
	key     string
	keySize int // encoded key size when the parent is an object
	kids    []*inputClipNode
	fixed   int
	size    int
	order   int
	active  bool
	index   int // position in inputClipHeap
}

type inputClipTree struct {
	arrays  []*inputClipNode
	objects []*inputClipNode
	order   int
	work    int // node visits, used by the deep-nesting test rather than a wall-clock assertion
}

func newInputClipTree(v any) *inputClipTree {
	t := &inputClipTree{}
	t.build(v)
	return t
}

func (t *inputClipTree) build(v any) *inputClipNode {
	t.work++
	n := &inputClipNode{value: v, active: true, index: -1}
	switch v := v.(type) {
	case []any:
		n.kind = '['
		n.order = t.order
		t.order++
		t.arrays = append(t.arrays, n)
		for _, e := range v {
			n.kids = append(n.kids, t.build(e))
		}
	case map[string]any:
		n.kind = '{'
		n.order = t.order
		t.order++
		t.objects = append(t.objects, n)
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys) // the same order as encoding/json, which produced the candidate
		for _, k := range keys {
			child := t.build(v[k])
			child.key, child.keySize = k, jsonLen(k)
			n.kids = append(n.kids, child)
		}
	default:
		n.size = len(marshalPlain(v))
	}
	return n
}

// size sums the current encoded size of a subtree. It is called only for the initial root and for a child being
// detached, so after the initial walk each indexed node is visited here at most once.
func (t *inputClipTree) size(n *inputClipNode) int {
	t.work++
	if n.kind == 0 {
		return n.size
	}
	size := 2
	for i, child := range n.kids {
		if i > 0 {
			size++
		}
		if n.kind == '{' {
			size += child.keySize + 1
		}
		size += t.size(child)
	}
	return size
}

// inputClipHeap chooses the longest array or widest object, with the first container in document order breaking ties.
type inputClipHeap []*inputClipNode

func (h inputClipHeap) Len() int { return len(h) }
func (h inputClipHeap) Less(i, j int) bool {
	a, b := h[i].remaining(), h[j].remaining()
	if a != b {
		return a > b
	}
	return h[i].order < h[j].order
}
func (h inputClipHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}
func (h *inputClipHeap) Push(v any) {
	n := v.(*inputClipNode)
	n.index = len(*h)
	*h = append(*h, n)
}
func (h *inputClipHeap) Pop() any {
	old := *h
	n := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	n.index = -1
	return n
}

func (n *inputClipNode) remaining() int { return len(n.kids) - n.fixed }

// nextInputClipNode lazily discards containers removed with an ancestor. A zero at the top means no attached container
// has anything left to drop: remaining count, not activity, determines heap order, so inactive positive counts pop first.
func nextInputClipNode(h *inputClipHeap) *inputClipNode {
	for h.Len() > 0 {
		n := (*h)[0]
		if !n.active {
			containerheap.Pop(h)
			continue
		}
		if n.remaining() == 0 {
			return nil
		}
		return n
	}
	return nil
}

func (t *inputClipTree) deactivate(n *inputClipNode) {
	t.work++
	n.active = false
	for _, child := range n.kids {
		t.deactivate(child)
	}
}

func (t *inputClipTree) dropTail(h *inputClipHeap, n *inputClipNode) int {
	last := len(n.kids) - 1 - n.fixed // fixed suffix children (the root marker) are never removed
	child := n.kids[last]
	saved := t.size(child)
	if n.kind == '{' {
		saved += child.keySize + 1
	}
	if len(n.kids) > 1 {
		saved++ // the comma before the removed member, including the root marker's comma
	}
	n.kids = append(n.kids[:last], n.kids[last+1:]...)
	t.deactivate(child)
	containerheap.Fix(h, n.index)
	return saved
}

func (n *inputClipNode) decodedValue() any {
	switch n.kind {
	case '[':
		out := make([]any, len(n.kids))
		for i, child := range n.kids {
			out[i] = child.decodedValue()
		}
		return out
	case '{':
		out := make(map[string]any, len(n.kids))
		for _, child := range n.kids {
			out[child.key] = child.decodedValue()
		}
		return out
	default:
		return n.value
	}
}

func inputClipNote(originalSize, arrayItems, objectFields int) string {
	return fmt.Sprintf("Omitted %d array items and %d object fields."+clipNoteFormat, arrayItems, objectFields, originalSize)
}

func inputClipMarkerValue(originalSize, arrayItems, objectFields int) map[string]any {
	return map[string]any{"note": inputClipNote(originalSize, arrayItems, objectFields)}
}

// boundInputJSON applies the third and final clip pass to an over-budget candidate from the string passes. It removes
// array tails, longest by current item count first, then object trailing fields, widest by current field count first.
// Every surviving value keeps its type and place. The root marker object names the direct removals and ends with the
// existing cut note; its encoded bytes are included throughout, so the compact JSON is at most maxInputLen bytes.
func boundInputJSON(candidate []byte, originalSize int) []byte {
	return boundInputJSONBudget(candidate, originalSize, maxInputLen)
}

func boundInputJSONBudget(candidate []byte, originalSize, budget int) []byte {
	out, _ := boundInputJSONBudgetWork(candidate, originalSize, budget)
	return out
}

func boundInputJSONBudgetWork(candidate []byte, originalSize, budget int) ([]byte, int) {
	value, err := decodeInputObject(candidate)
	if err != nil {
		return candidate, 0 // candidate came from marshalPlain; keep the old behavior if that ever changes
	}
	tree := newInputClipTree(value)
	rootNode := tree.objects[0] // the document root is the first object in preorder
	arrays := inputClipHeap(tree.arrays)
	objects := inputClipHeap(tree.objects)
	for i, n := range arrays {
		n.index = i
	}
	for i, n := range objects {
		n.index = i
	}
	containerheap.Init(&arrays)
	containerheap.Init(&objects)

	markerKey := inputClipMarker
	for suffix := 2; ; suffix++ {
		if _, exists := value[markerKey]; !exists {
			break
		}
		markerKey = fmt.Sprintf("%s_%d", inputClipMarker, suffix)
	}
	marker := &inputClipNode{value: inputClipMarkerValue(originalSize, 0, 0), key: markerKey, keySize: jsonLen(markerKey), active: true, index: -1}
	marker.size = len(marshalPlain(marker.value))
	size := tree.size(rootNode) + marker.keySize + 1 + marker.size
	if len(rootNode.kids) > 0 {
		size++
	}
	rootNode.kids = append(rootNode.kids, marker)
	rootNode.fixed = 1
	containerheap.Fix(&objects, rootNode.index)

	arrayItems, objectFields := 0, 0
	for size > budget {
		if n := nextInputClipNode(&arrays); n != nil {
			size -= tree.dropTail(&arrays, n)
			arrayItems++
		} else if n := nextInputClipNode(&objects); n != nil {
			size -= tree.dropTail(&objects, n)
			objectFields++
		} else {
			break // only the small root marker remains, which always fits
		}
		marker.value = inputClipMarkerValue(originalSize, arrayItems, objectFields)
		nextSize := len(marshalPlain(marker.value))
		size += nextSize - marker.size
		marker.size = nextSize
	}
	return marshalPlain(rootNode.decodedValue()), tree.work
}
