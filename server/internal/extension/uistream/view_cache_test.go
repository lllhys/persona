package uistream

import (
	"bytes"
	"testing"
)

// A checked tree serializes its view once; these pin what that cache must keep.

func TestViewJSONIsADetachedCopy(t *testing.T) {
	tree := treeFromFixture(t, "UIStreamSnapshot.keyed.json")
	want := tree.root.json()
	got := tree.ViewJSON()
	if !bytes.Equal(got, want) {
		t.Fatalf("cached view differs from the tree\n%s\n%s", got, want)
	}
	for i := range got {
		got[i] = 'x'
	}
	if again := tree.ViewJSON(); !bytes.Equal(again, want) {
		t.Fatalf("a caller's edit reached the tree: %s", again)
	}
	if frame := encoded(t, tree); !bytes.Contains(frame, []byte(`"title":"Example"`)) {
		t.Fatalf("a caller's edit reached the frame: %s", frame)
	}
}

func TestAppliedTreeCachesItsOwnView(t *testing.T) {
	a := treeFromFixture(t, "UIStreamSnapshot.keyed.json")
	before := a.ViewJSON()
	d, err := DecodeDelta(fixture(t, "UIStreamDelta.operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Apply(a, d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.ViewJSON(), b.root.json()) {
		t.Fatalf("applied tree serves a stale view\n%s\n%s", b.ViewJSON(), b.root.json())
	}
	want := treeFromFixture(t, "UIStreamSnapshot.after-operations.json")
	if !bytes.Equal(b.ViewJSON(), want.ViewJSON()) {
		t.Fatalf("applied view\n%s\nwant\n%s", b.ViewJSON(), want.ViewJSON())
	}
	if !bytes.Equal(a.ViewJSON(), before) || !bytes.Equal(a.ViewJSON(), a.root.json()) {
		t.Fatal("applying a delta changed the source tree's view")
	}
}

func TestDiffReadsBothCachedViews(t *testing.T) {
	from := snapshot(t, benchView(3, 4, ""), 1)
	to := snapshot(t, benchView(3, 4, "s2-i1"), 2)
	d, snap, err := Diff(from, to)
	if err != nil || d == nil || snap || len(d.ops) != 1 {
		t.Fatalf("diff = %v, snapshot %v, err %v", d, snap, err)
	}
	got, err := Apply(from, d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.ViewJSON(), to.ViewJSON()) {
		t.Fatalf("diff did not reproduce the target\n%s\n%s", got.ViewJSON(), to.ViewJSON())
	}
}
