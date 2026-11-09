package hub

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// A manager's card push replaces only that manager's push of the same card: a card id that is another push's collapse
// id (the hub's update notice, a decision's, a notify API alert's, an extension's) gets an apns-collapse-id of its own,
// as does the same id from another manager, a 70-byte one fits APNs' 64 bytes, and the payload still names the card.
// The assistant the hub starts pushes its cards under their ids, as before.
func TestCardCollapseIDIsTheManagers(t *testing.T) {
	h, f, attempts := localizedHub(t)
	if err := h.push.setPrefs("phone", NotifyPrefs{Input: true}); err != nil {
		t.Fatal(err)
	}
	const other = "m-0b0b0b"
	ids := []string{"hub_update", "td_" + strings.Repeat("0", 24), notifyCollapse + "budget", extensionCollapse + strings.Repeat("1", 32),
		"ext:" + strings.Repeat("2", 32), "p_" + strings.Repeat("3", 16), "codex-" + strings.Repeat("ab", 32), "ix_mine"}
	var cards []proto.Interaction
	for _, id := range ids {
		cards = append(cards, proto.Interaction{ID: id, Kind: proto.IxPermission, Summary: "x"})
	}
	h.takeFleetInteractionsFrom(other, cards)
	h.takeFleetInteractions([]proto.Interaction{{ID: "ix_yours", Kind: proto.IxPermission, Summary: "x"}})
	sent := 0
	push := func(from, id string) apnsCall {
		t.Helper()
		if err := h.deliverPushFrom(from, assistantwire.Notification{Kind: assistantwire.NotifyInput, Interaction: &proto.Interaction{ID: id}}); err != nil {
			t.Fatalf("%s's card %q: %v", from, id, err)
		}
		sent++
		return sentPushes(t, f, attempts, sent)[sent-1]
	}
	if got := push(assistantwire.AssistantID, "ix_yours"); got.Collapse != "ix_yours" {
		t.Fatalf("the assistant's card: apns-collapse-id %q, want its id", got.Collapse)
	}
	seen := map[string]string{}
	for _, id := range ids {
		got := push(other, id)
		if !strings.HasPrefix(got.Collapse, "card:") || len(got.Collapse) > 64 || got.Collapse != cardCollapseID(other, id) {
			t.Fatalf("%s's card %q: apns-collapse-id %q", other, id, got.Collapse)
		}
		if prev, ok := seen[got.Collapse]; ok {
			t.Fatalf("cards %q and %q share apns-collapse-id %q", prev, id, got.Collapse)
		}
		seen[got.Collapse] = id
		var p map[string]any
		if err := json.Unmarshal(got.Body, &p); err != nil || p["interaction_id"] != id {
			t.Fatalf("%s's card %q: payload names %v (%v)", other, id, p["interaction_id"], err)
		}
	}
	if again := push(other, "hub_update"); again.Collapse != cardCollapseID(other, "hub_update") {
		t.Fatalf("the same card again: apns-collapse-id %q", again.Collapse)
	}
	if cardCollapseID("m-0c0c0c", "ix_mine") == cardCollapseID(other, "ix_mine") || cardCollapseID(other, "ix_mine") == "ix_mine" {
		t.Fatal("two managers' card ix_mine share an apns-collapse-id")
	}
	if cardCollapseID("ab", "c") == cardCollapseID("a", "bc") {
		t.Fatal("managers ab and a share an id: the manager's end is not kept apart from the id")
	}
	if cardCollapseID("", "ix_x") != "ix_x" {
		t.Fatal("a card of no manager changed its apns-collapse-id")
	}
}

// A manager's card pushed under its own collapse id is still taken back by its card id: the push the app opens names
// the card, and so does the withdrawal push once it is over.
func TestAManagersCardIsWithdrawnByItsID(t *testing.T) {
	h, f, open := withdrawHub(t, "withdraw")
	const other, id = "m-0b0b0b", "hub_update"
	h.notifyInteraction(other, card(id), "笔记本", false)
	r := f.wait(t, 1)[0]
	if r.CollapseID != relayCollapseID(cardCollapseID(other, id)) {
		t.Fatalf("relay collapse id %q, want the hash of %q", r.CollapseID, cardCollapseID(other, id))
	}
	if p := open(r); p["interaction_id"] != id {
		t.Fatalf("opened %v", p)
	}
	h.push.withdraw(id)
	w := f.wait(t, 2)[1]
	if ids, _ := open(w)["resolved_interaction_ids"].([]any); w.PushType != "background" || len(ids) != 1 || ids[0] != id {
		t.Fatalf("withdrawal %+v names %v", w, ids)
	}
}
