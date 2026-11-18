package proto

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestSessionDeletedRareFieldWireAndCopies(t *testing.T) {
	base := Session{SessionID: "ordinary", BypassPending: true}
	base.SetParentAgentID("parent")
	base.SetAliases([]string{"earlier"})
	base.SetCompletedTodo(&TodoRef{ID: "completed"})
	copy := base
	copy.SetDeleted(true)
	if base.IsDeleted() || !copy.IsDeleted() || copy.ParentAgentID() != "parent" || !copy.BypassPending {
		t.Fatal("tombstone changed a shared row or lost main's pending mode")
	}
	copy.SetParentAgentID("other")
	copy.SetAliases([]string{"copy-alias"})
	if base.ParentAgentID() != "parent" || !copy.IsDeleted() {
		t.Fatal("ancestry mutation lost or shared the tombstone")
	}
	if base.Aliases()[0] != "earlier" || copy.Aliases()[0] != "copy-alias" || copy.CompletedTodo().ID != "completed" {
		t.Fatal("tombstone mutation lost or shared sparse metadata")
	}
	raw, err := json.Marshal(copy)
	if err != nil || !strings.Contains(string(raw), `"deleted":true`) || !strings.Contains(string(raw), `"bypass_pending":true`) || strings.Contains(string(raw), `"Hierarchy"`) {
		t.Fatalf("top-level wire: %s %v", raw, err)
	}
	var roundTrip Session
	if err := json.Unmarshal(raw, &roundTrip); err != nil || !roundTrip.IsDeleted() || roundTrip.ParentAgentID() != "other" || !roundTrip.BypassPending {
		t.Fatalf("roundtrip: %+v %v", roundTrip, err)
	}
	var only Session
	only.SetDeleted(true)
	before := only
	only.SetDeleted(false)
	if only.Hierarchy != nil || !before.IsDeleted() {
		t.Fatal("clearing a copy retained empty storage or changed its source")
	}
}

func TestSessionDeletedCopyOnWriteConcurrent(t *testing.T) {
	var base Session
	base.SetParentAgentID("parent")
	base.SetFold("parent", "", "", &SessionChildren{Total: 1})
	var work sync.WaitGroup
	for range 12 {
		work.Go(func() {
			copy := base
			copy.SetDeleted(true)
			copy.SetParentAgentID("copy")
			if !copy.IsDeleted() || copy.ParentAgentID() != "copy" {
				t.Error("copy lost its metadata")
			}
		})
	}
	work.Wait()
	if base.IsDeleted() || base.ParentAgentID() != "parent" || base.Hierarchy.Children.Total != 1 {
		t.Fatal("shared metadata mutated")
	}
}
