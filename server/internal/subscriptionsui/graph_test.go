package subscriptionsui

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// The relationship view (#5741): every source the list has, under what brings it and how it is read, with where it
// goes, whether it reads now, and the list's words, badges and possible duplicates; sources that are off are left out.
func TestGraph(t *testing.T) {
	f, b := withDuplicate(newFake()), newBuiltins()
	g, err := DrawGraph(context.Background(), f, sourcesEnv(i18n.En, true, b))
	if err != nil {
		t.Fatal(err)
	}
	var exts []string
	for _, e := range g.Extensions {
		exts = append(exts, e.ID+"="+e.Name+" ("+e.Line+")")
	}
	if want := []string{"persona=Built into Persona (3 sources)", "apps=Connected apps (3 sources)", "lark=lark (1 source)", "telegram=telegram (1 source)"}; !slices.Equal(exts, want) {
		t.Errorf("extensions, Persona's own first: %q", exts)
	}
	reads := map[string]GraphRead{}
	first := map[string]int{}
	for _, r := range g.Reads {
		reads[r.ID] = r
	}
	var order []string
	for i, s := range g.Sources {
		r, ok := reads[s.Read]
		if !ok {
			t.Fatalf("%s reads with %q, which is not among the reads", s.ID, s.Read)
		}
		if _, seen := first[s.Read]; !seen {
			first[s.Read] = i
		}
		order = append(order, s.ID+"<"+r.Title)
	}
	// Grouped by extension and, inside one, by read: the columns read top to bottom without a link crossing another.
	if want := []string{"b.github<Read directly", "b.mail<Read directly", "s.sub_github<notifications", "s.sub_outlook<Search messages",
		"s.sub_slack<list_messages", "s.sub_broker<positions", "b.lark<Read directly", "b.telegram<Read directly"}; !slices.Equal(order, want) {
		t.Errorf("sources:\n%q\nwant\n%q", order, want)
	}
	for i := 1; i < len(g.Reads); i++ {
		if first[g.Reads[i-1].ID] > first[g.Reads[i].ID] {
			t.Errorf("reads out of their sources' order: %+v", g.Reads)
		}
	}
	src := map[string]GraphSource{}
	for _, s := range g.Sources {
		src[s.ID] = s
	}
	outlook, mail := src["s.sub_outlook"], src["b.mail"]
	if !outlook.Reading || !slices.Equal(outlook.To, []string{"inbox", "kb"}) || !slices.Equal(outlook.Duplicates, []string{"b.mail"}) ||
		outlook.Badge == nil || outlook.Badge.Text != "Possible duplicate" || outlook.State != `{"p":"source","id":"sub_outlook"}` ||
		!strings.HasPrefix(outlook.Ask, AskURL+"?text=") || outlook.Kind != "subscription" {
		t.Errorf("a subscription that reads: %+v", outlook)
	}
	if mail.Reading || !slices.Equal(mail.Duplicates, []string{"s.sub_outlook"}) || mail.Badge == nil || mail.State != `{"p":"builtin","c":"mail"}` ||
		mail.Ask != "" || mail.Kind != "builtin" || !reads[mail.Read].Direct {
		t.Errorf("a built-in source one of whose accounts failed: %+v", mail)
	}
	for _, id := range []string{"s.sub_slack", "s.sub_github", "b.telegram"} {
		if s := src[id]; s.Reading || s.Badge == nil {
			t.Errorf("%s can't read now and says so: %+v", id, s)
		}
	}
	if s := src["s.sub_broker"]; !s.Reading || !slices.Equal(s.To, []string{"kb"}) {
		t.Errorf("a knowledge-base-only subscription: %+v", s)
	}
	var dests []string
	for _, d := range g.Destinations {
		dests = append(dests, d.ID+"="+d.Name+" ("+d.Line+")")
	}
	if want := []string{"inbox=Inbox (7 sources · 3 reading)", "kb=Knowledge base (3 sources · 2 reading)"}; !slices.Equal(dests, want) {
		t.Errorf("destinations: %q", dests)
	}

	// Off is left out: a subscription with neither destination, and a built-in source turned off as a whole.
	s := f.subs["sub_broker"]
	s.ToKB, s.State = false, subscriptions.StateOff
	f.subs["sub_broker"] = s
	b.list[2].Status = "disabled" // Lark
	g, _ = DrawGraph(context.Background(), f, sourcesEnv(i18n.ZhHans, true, b))
	for _, s := range g.Sources {
		if s.ID == "s.sub_broker" || s.ID == "b.lark" {
			t.Errorf("an off source is drawn: %+v", s)
		}
	}
	for _, e := range g.Extensions {
		if e.ID == "lark" {
			t.Error("an extension with nothing drawn is drawn")
		}
	}
	if g.Extensions[0].Name != "Persona 自带" || g.Destinations[0].Line != "6 个来源 · 2 个在读" || g.Destinations[1].Name != "知识库" {
		t.Errorf("in Chinese: %+v %+v", g.Extensions[0], g.Destinations)
	}
}
