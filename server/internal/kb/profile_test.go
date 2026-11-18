package kb

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// Person profiles (profile.go): each writer's own, in four labelled parts, replaced as a whole, never touching anyone
// else's or the person's pinned facts and note.

func sampleProfile(fact string) *PersonProfile {
	return &PersonProfile{
		Facts:       []ProfileItem{{Text: fact, Sources: []string{"chats/dm-a/messages/2026-11-01.md"}}},
		Views:       []ProfileItem{{Text: "explains things clearly", Sources: []string{"user"}}},
		Inferences:  []ProfileItem{{Text: "prefers mornings", Basis: "replies before 10am on 8 of 10 days", Sources: []string{"chats/dm-a"}}},
		Commitments: []ProfileCommitment{{Text: "send the review notes", Owes: "user", Due: "2026-11-20", Sources: []string{"chats/dm-a/messages/2026-11-02.md"}}},
	}
}

func TestPersonProfileIsTheWritersOwn(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutPerson(&Person{ID: "p1", Name: "Example Person"})
	if _, err := e.Apply(KindPerson, "p1", Update{Pinned: str("works on the example team"), Note: "met at the kickoff"}); err != nil {
		t.Fatal(err)
	}
	res := extApply(t, e, "people", KindPerson, "p1", Update{Profile: sampleProfile("leads the example project")})
	if string(res.Before["profile"]) != "null" || len(res.Ignored) != 0 {
		t.Fatalf("first write: before %s ignored %v", res.Before, res.Ignored)
	}
	sum := e.Store.Summary(KindPerson, "p1")
	for _, want := range []string{"## 已确认的事实\nworks on the example team", "met at the kickoff", "## 画像（扩展 people整理",
		"### 事实\n- [事实] leads the example project（出处：chats/dm-a/messages/2026-11-01.md）",
		"### 用户说过的评价（原话）\n- [用户原话] “explains things clearly”（出处：user）",
		"### 推断（仅供参考，未经确认）\n- [推断·仅供参考] prefers mornings（依据：replies before 10am on 8 of 10 days）",
		"### 承诺和待办\n- [承诺·用户答应对方，截止 2026-11-20] send the review notes"} {
		if !strings.Contains(sum, want) {
			t.Fatalf("summary.md lacks %q:\n%s", want, sum)
		}
	}

	// Another extension's profile sits beside it; the user's own too. Neither touches the other.
	extApply(t, e, "other", KindPerson, "p1", Update{Profile: &PersonProfile{Facts: []ProfileItem{{Text: "based in the north office", Sources: []string{"docs/d1"}}}}})
	if _, err := e.Apply(KindPerson, "p1", Update{Profile: &PersonProfile{Views: []ProfileItem{{Text: "very reliable", Sources: []string{"user"}}}}}); err != nil {
		t.Fatal(err)
	}
	// The first extension replaces its own as a whole: the old one comes back in before.
	res = extApply(t, e, "people", KindPerson, "p1", Update{Profile: sampleProfile("leads two projects now")})
	var old PersonProfile
	if json.Unmarshal(res.Before["profile"], &old) != nil || old.Facts[0].Text != "leads the example project" {
		t.Fatalf("before: %s", res.Before["profile"])
	}
	d, err := e.Get(KindPerson, "p1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Profiles) != 3 || d.Profiles["ext:people"].Profile.Facts[0].Text != "leads two projects now" ||
		d.Profiles["ext:other"].Profile.Facts[0].Text != "based in the north office" || d.Profiles[UserWriter].Profile.Views[0].Text != "very reliable" {
		t.Fatalf("profiles: %+v", d.Profiles)
	}
	sum = d.Summary
	user, other, mine := strings.Index(sum, "## 画像（你自己整理"), strings.Index(sum, "## 画像（扩展 other整理"), strings.Index(sum, "## 画像（扩展 people整理")
	if user < 0 || other < 0 || mine < 0 || !(user < other && other < mine) || strings.Contains(sum, "leads the example project") {
		t.Fatalf("summary.md order or stale text:\n%s", sum)
	}
	p, _ := e.Store.Person("p1")
	if p.Pinned != "works on the example team" || p.Note != "met at the kickoff" || slices.Contains(p.LockedFields, "profile") || p.Provenance != nil {
		t.Fatalf("the person's own fields changed: %+v", p)
	}

	// An empty profile removes the writer's own, and only it.
	res = extApply(t, e, "people", KindPerson, "p1", Update{Profile: &PersonProfile{}})
	if string(res.Before["profile"]) == "null" {
		t.Fatal("removing gave no before")
	}
	d, _ = e.Get(KindPerson, "p1", 0)
	if _, ok := d.Profiles["ext:people"]; ok || len(d.Profiles) != 2 || strings.Contains(d.Summary, "扩展 people") {
		t.Fatalf("after removing: %+v\n%s", d.Profiles, d.Summary)
	}
}

// A profile that breaks a rule is refused whole, saying which; nothing is written.
func TestPersonProfileChecks(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutPerson(&Person{ID: "p1", Name: "Example Person"})
	long := strings.Repeat("x", profileTextMost+1)
	for name, c := range map[string]struct {
		p    *PersonProfile
		want string
	}{
		"inference without basis": {&PersonProfile{Inferences: []ProfileItem{{Text: "t", Sources: []string{"s"}}}}, "basis"},
		"no source":               {&PersonProfile{Facts: []ProfileItem{{Text: "t"}}}, "sources"},
		"empty text":              {&PersonProfile{Views: []ProfileItem{{Text: " ", Sources: []string{"s"}}}}, "text is empty"},
		"long text":               {&PersonProfile{Facts: []ProfileItem{{Text: long, Sources: []string{"s"}}}}, "at most 500"},
		"basis on a fact":         {&PersonProfile{Facts: []ProfileItem{{Text: "t", Basis: "b", Sources: []string{"s"}}}}, "basis is for inferences"},
		"bad owes":                {&PersonProfile{Commitments: []ProfileCommitment{{Text: "t", Owes: "me", Sources: []string{"s"}}}}, "owes"},
		"bad due":                 {&PersonProfile{Commitments: []ProfileCommitment{{Text: "t", Owes: "them", Due: "soon", Sources: []string{"s"}}}}, "YYYY-MM-DD"},
		"too many":                {&PersonProfile{Facts: slices.Repeat([]ProfileItem{{Text: "t", Sources: []string{"s"}}}, profileItemsMost+1)}, "at most 30"},
		"too long shown": {&PersonProfile{Facts: slices.Repeat([]ProfileItem{{Text: strings.Repeat("y", 400), Sources: []string{"s"}}}, 25)},
			"bytes as shown"},
	} {
		_, err := e.ApplyExtension(KindPerson, "p1", Update{Profile: c.p, Note: "should not land"}, ExtensionWrite{Extension: "people"})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	if p, _ := e.Store.Person("p1"); p.Note != "" {
		t.Fatalf("a refused write changed the note: %q", p.Note)
	}
	if _, err := os.Stat(e.Store.path(KindPerson, "p1", profilesFile)); !os.IsNotExist(err) {
		t.Fatalf("profiles.json written: %v", err)
	}
}

// Fields a person does not use are reported, not applied, not locked: summary.md stays the hub's.
func TestPersonUpdateReportsWhatItDoesNotUse(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutPerson(&Person{ID: "p1", Name: "Example Person"})
	e.WritePersonSummary("p1")
	res := extApply(t, e, "people", KindPerson, "p1", Update{Summary: "a hand-written summary", Log: "a log line", Goal: "g", Note: "a note"})
	var fields []string
	for _, f := range res.Ignored {
		fields = append(fields, f.Field)
		if f.Reason == "" {
			t.Errorf("%s: no reason", f.Field)
		}
	}
	if strings.Join(fields, ",") != "goal,log,summary" {
		t.Fatalf("ignored %v", res.Ignored)
	}
	if _, ok := res.Before["summary"]; ok {
		t.Fatalf("before names a field not written: %s", res.Before)
	}
	p, _ := e.Store.Person("p1")
	if p.Note != "a note" || slices.Contains(p.LockedFields, "summary") || slices.Contains(p.LockedFields, "goal") {
		t.Fatalf("person %+v", p)
	}
	if strings.Contains(e.Store.Summary(KindPerson, "p1"), "hand-written") {
		t.Fatal("summary.md was replaced")
	}
	// The user's path drops them the same way.
	if _, err := e.Apply(KindPerson, "p1", Update{Summary: "x"}); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.Store.Person("p1"); slices.Contains(p.LockedFields, "summary") {
		t.Fatal("the user's write locked a summary it did not write")
	}
}

// Every field a kind uses passes; the rest are named.
func TestUnsupportedFieldsByKind(t *testing.T) {
	full := Update{Pinned: str("p"), Lock: new(bool), RemovePeople: []string{"x"}, RemoveChats: []string{"x"}, RemoveAliases: []string{"x"},
		RemoveProjects: []string{"x"}, Name: "n", Kind: "short", Status: "active", Goal: "g", MyRole: "r", Note: "n", Importance: "high",
		Relevance: new(int), Tags: []string{}, Summary: "s", Log: "l", Source: "s", Aliases: []string{"a"}, Chats: []string{"c"},
		People: []string{"p"}, Dirs: []string{"d"}, Projects: []string{"p"}, MergeInto: "m", Profile: &PersonProfile{}}
	for kind, want := range map[string]string{
		KindProject:   "profile,projects,remove_projects",
		KindChat:      "aliases,chats,dirs,goal,log,merge_into,my_role,note,people,profile,project_kind,remove_aliases,remove_chats,source,status",
		KindPerson:    "aliases,chats,dirs,goal,log,merge_into,my_role,name,people,project_kind,remove_aliases,remove_people,source,summary",
		KindDoc:       "aliases,chats,dirs,goal,log,merge_into,my_role,name,people,profile,project_kind,remove_aliases,remove_chats,remove_people,source,status,summary",
		KindTopic:     "chats,dirs,goal,log,merge_into,my_role,people,profile,project_kind,projects,remove_chats,remove_people,remove_projects,source,status",
		KindRecording: "aliases,chats,dirs,goal,log,merge_into,my_role,name,note,people,profile,project_kind,remove_aliases,remove_chats,remove_people,source,status,summary",
	} {
		var got []string
		for _, f := range Unsupported(kind, full) {
			got = append(got, f.Field)
		}
		if strings.Join(got, ",") != want {
			t.Errorf("%s: %v\nwant %s", kind, got, want)
		}
	}
	if got := Unsupported(KindProject, Update{Source: "x"}); len(got) != 1 || got[0].Field != "source" {
		t.Errorf("a project's source without a log: %v", got)
	}
	if got := Unsupported("unknown", Update{Note: "x"}); got != nil {
		t.Errorf("unknown kind: %v", got)
	}
}

// A search finds a profile's lines in summary.md, each saying which part it is; profiles.json itself is not searched.
func TestPersonProfileSearch(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutPerson(&Person{ID: "p1", Name: "Example Person"})
	extApply(t, e, "people", KindPerson, "p1", Update{Profile: sampleProfile("leads the example project")})
	hits := e.Search("prefers mornings", false, 10)
	if len(hits) == 0 {
		t.Fatal("no hit")
	}
	for _, h := range hits {
		if !strings.HasSuffix(h.Path, "people/p1/summary.md") || !strings.Contains(h.Line, "[推断·仅供参考]") {
			t.Errorf("hit %+v", h)
		}
	}
	if searchable("people/p1/" + profilesFile) {
		t.Error("profiles.json is searchable")
	}
}
