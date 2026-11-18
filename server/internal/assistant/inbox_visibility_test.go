package assistant

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestInboxHandlingVisibilitySnapshot(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.Main = "main" })
	for id, origin := range map[string]string{
		"main": OriginInbox, "inbox": OriginInbox, "user": OriginUser,
		"channel": OriginChannel, "task": OriginTask, "spawned": OriginSpawned,
		"extension": OriginExtension, "lark-agent": OriginLarkAgent,
	} {
		s.originSet(ctx, "server", id, origin, "ref", "")
	}
	for _, v := range s.snapshot(ctx).Sessions {
		if want := v.SessionID == "inbox"; v.Archived != want {
			t.Errorf("%s: archived=%v, want %v", v.SessionID, v.Archived, want)
		}
	}
	// The default is derived from saved origins, including conversations from before the upgrade.
	s.origins.mu.Lock()
	s.origins.loaded = false
	s.origins.mu.Unlock()
	s.UserSpoke(ctx, SessionRef{MachineID: "server", SessionID: "inbox"})
	for _, v := range s.snapshot(ctx).Sessions {
		if v.SessionID == "inbox" && (!v.Archived || v.OriginRef != "ref") {
			t.Fatalf("continuing a recovered handling chat exposed it: %+v", v)
		}
	}
}

func TestInboxHandlingVisibilityListsAndLookup(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub()
	f.hello.Assistant.MachineID = "box"
	f.hello.Assistant.Machines = []string{"box"}
	f.devices = []Device{{ID: "box", Home: "/home/u"}}
	d := newFleetDevs()
	d.add("box", "Computer")
	st := newFleetMem()
	s := New(Options{Hub: f, Devices: d, Store: func() Store { return st }})
	setSettings(t, s, func(v *Settings) { v.MachineID, v.Main = "box", "main" })
	d.setSessions("box", []proto.Session{
		{SessionID: "main", Cwd: "/home/u/.persona/assistant", Status: proto.StatusDone},
		{SessionID: "inbox", Cwd: "/home/u/.persona/assistant", Status: proto.StatusDone},
		{SessionID: "user", Cwd: "/home/u/.persona/assistant", Status: proto.StatusDone},
	})
	s.originSet(ctx, "box", "inbox", OriginInbox, "example:item", "")
	for _, list := range [][]proto.Session{
		s.view(ctx, false).Conversations,
		func() []proto.Session { list, _ := s.conversations(ctx, false, ""); return list }(),
	} {
		if len(list) != 2 {
			t.Fatalf("default list: %+v", list)
		}
		for _, c := range list {
			if c.SessionID == "inbox" {
				t.Fatal("the default list contains an Inbox handling chat")
			}
		}
	}
	if got := s.view(ctx, true).Conversations; len(got) != 3 {
		t.Fatalf("archived history lost the handling chat: %+v", got)
	}
	c, err := s.assistantConversation(ctx, "inbox")
	if err != nil || !c.Archived || c.OriginRef != "example:item" {
		t.Fatalf("the Inbox link cannot resolve its processing chat: %+v, %v", c, err)
	}
}

func TestInboxHandlingVisibilityKeepsInstructions(t *testing.T) {
	ctx := context.Background()
	h, _, it := handledHub(t)
	h.originSet(ctx, "server", "s1", OriginInbox, it.ID, "")
	if _, err := suggestTool(t, h, `{"kind":"conclusion","text":"Check the schedule","outcome":"fyi"}`); err != nil {
		t.Fatal(err)
	}
	code, _ := suggestionCall(t, h, it.ID, `{"version":1,"action":"instruct","text":"Check the deadline first"}`)
	if code != 200 {
		t.Fatalf("instruction returned %d", code)
	}
	toldConv(t, h, "Check the deadline first")
	h.inbox.mu.Lock()
	listed := it.listed()
	h.inbox.mu.Unlock()
	if listed.Agent.SessionID != "s1" || listed.Handling.State != HandlingLooking {
		t.Fatalf("the instruction did not continue the same chat: %+v", listed)
	}
	linked := false
	for _, e := range listed.Chain {
		if e.Link != nil && e.Link.SessionID == "s1" {
			linked = true
		}
	}
	if !linked {
		t.Fatal("the Inbox chain lost the processing-chat link")
	}
	h.UserSpoke(ctx, SessionRef{MachineID: "server", SessionID: "s1"})
	for _, v := range h.snapshot(ctx).Sessions {
		if v.SessionID == "s1" && !v.Archived {
			t.Fatal("an instruction brought the processing chat into the default list")
		}
	}
	// The item's API still carries its processing conversation and handling chain.
	code, detail := inboxCall(t, h.routes(), "GET", "/api/v1/inbox/"+it.ID, "")
	agent, ok := detail["agent"].(map[string]any)
	if code != 200 || !ok || agent["session_id"] != "s1" {
		t.Fatalf("item detail: %d %v", code, detail)
	}
}
