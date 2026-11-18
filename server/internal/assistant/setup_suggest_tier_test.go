package assistant

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The weekly block's line for a lower tier of the background supervisor's model (setup_suggest_tier.go). All models,
// launchers and ids are placeholders.

// tierLadder is a provider with three tier models; its own default is the largest of them.
func tierLadder() proto.ManagedLauncher {
	return proto.ManagedLauncher{Name: "gw", BaseURL: "https://models.example", Model: "big-model",
		HaikuModel: "small-model", SonnetModel: "mid-model", OpusModel: "big-model"}
}

func TestTierBelowNeedsATierThatIsNotTheSmallest(t *testing.T) {
	for name, c := range map[string]struct {
		l       proto.ManagedLauncher
		current string
		want    string
	}{
		"three tiers, on the largest":               {tierLadder(), "big-model", "mid-model"},
		"the 1M marker aside":                       {proto.ManagedLauncher{HaikuModel: "small-model", SonnetModel: "mid-model[1m]", OpusModel: "big-model"}, "big-model", "mid-model"},
		"four tiers, on the top":                    {proto.ManagedLauncher{HaikuModel: "small-model", SonnetModel: "mid-model", OpusModel: "big-model", FableModel: "top-model"}, "top-model", "big-model"},
		"a tier the launcher does not name":         {proto.ManagedLauncher{HaikuModel: "small-model", SonnetModel: "mid-model", FableModel: "top-model"}, "top-model", "mid-model"},
		"the light tier as the web's form keeps it": {proto.ManagedLauncher{SmallModel: "small-model", SonnetModel: "mid-model", OpusModel: "big-model"}, "big-model", "mid-model"},
		"the Haiku tier over the small model":       {proto.ManagedLauncher{HaikuModel: "mid-model", SmallModel: "small-model", SonnetModel: "mid-model", OpusModel: "big-model"}, "big-model", ""},
		"on the light tier the form keeps":          {proto.ManagedLauncher{SmallModel: "small-model", SonnetModel: "mid-model", OpusModel: "big-model"}, "small-model", ""},
		"on the middle: below is the smallest":      {tierLadder(), "mid-model", ""},
		"on the smallest":                           {tierLadder(), "small-model", ""},
		"two tiers only":                            {proto.ManagedLauncher{SonnetModel: "mid-model", OpusModel: "big-model"}, "big-model", ""},
		"two tiers under one model":                 {proto.ManagedLauncher{HaikuModel: "mid-model", SonnetModel: "mid-model", OpusModel: "big-model"}, "big-model", ""},
		"the model fills two slots":                 {proto.ManagedLauncher{HaikuModel: "small-model", SonnetModel: "big-model", OpusModel: "big-model"}, "big-model", ""},
		"the model fills the slots over a third":    {proto.ManagedLauncher{HaikuModel: "small-model", SonnetModel: "mid-model", OpusModel: "big-model", FableModel: "big-model"}, "big-model", "mid-model"},
		"a model that is no tier model":             {tierLadder(), "other-model", ""},
		"no tier models":                            {proto.ManagedLauncher{Model: "big-model"}, "big-model", ""},
		"no model known":                            {tierLadder(), "", ""},
	} {
		got, ok := tierBelow(c.l, c.current)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s: %q %v, want %q", name, got, ok, c.want)
		}
	}
}

// tierReviews are n acceptances of the background supervisor's on model, one every hour up to firstAgo before now, the
// first reopened of them reopened an hour after they were accepted. Oldest first, as the store returns them.
func tierReviews(now time.Time, prefix, role, model string, n, reopened int, firstAgo time.Duration) []db.WorkEvent {
	var out []db.WorkEvent
	for i := n - 1; i >= 0; i-- {
		at := now.Add(-firstAgo - time.Duration(i)*time.Hour)
		todo := fmt.Sprintf("%s%d", prefix, i)
		out = append(out, db.WorkEvent{At: at, Kind: taskReviewedEvent, Detail: map[string]any{"todo": todo, "verdict": reviewAccepted,
			"conversation": "bg", "role": role, "model": model, "effort": "medium"}})
		if i < reopened {
			out = append(out, db.WorkEvent{At: at.Add(30 * time.Minute), Kind: todoReopenedEvent, Detail: map[string]any{"todo": todo}})
		}
	}
	slices.SortStableFunc(out, func(a, b db.WorkEvent) int { return a.At.Compare(b.At) })
	return out
}

func tierEvents(lists ...[]db.WorkEvent) []db.WorkEvent {
	out := slices.Concat(lists...)
	slices.SortStableFunc(out, func(a, b db.WorkEvent) int { return a.At.Compare(b.At) })
	return out
}

// The share is reopened over the settled acceptances, never over all that were accepted, and only the background
// supervisor's own reviews on the model in question are read.
func TestSetupTierCountIsReopenedOverSettled(t *testing.T) {
	since := setupNow.Add(-setupLookback)
	settled := 2 * supervisionReopenWithin
	events := tierEvents(
		tierReviews(setupNow, "s", supervisorBackground, "big-model", 20, 2, settled),
		tierReviews(setupNow, "f", supervisorBackground, "big-model", 23, 0, time.Hour), // accepted within the last day: not settled
		tierReviews(setupNow, "m", supervisorMain, "big-model", 40, 0, settled),         // the main conversation's own reviews
		tierReviews(setupNow, "l", supervisorLeader, "big-model", 40, 0, settled),
		tierReviews(setupNow, "o", supervisorBackground, "mid-model", 40, 0, settled), // on another model
		tierReviews(setupNow, "x", supervisorBackground, "big-model", 5, 0, 40*24*time.Hour),
		[]db.WorkEvent{{At: setupNow.Add(-settled), Kind: taskReviewedEvent, Detail: map[string]any{"todo": "r", "verdict": reviewRework,
			"role": supervisorBackground, "model": "big-model"}}},
	)
	c := setupTierCount(events, "big-model", since, setupNow)
	if c.n != 20 || c.reopened != 2 || c.model != "big-model" {
		t.Fatalf("count: %+v", c)
	}
	c.lower = "mid-model"
	// 2 of 20 settled is 10 percent; over the 43 accepted it would be under 5 and pass.
	if (setupState{}).suggests(setupSupervisorTier, c) {
		t.Fatal("judged over the accepted, not the settled")
	}
	// A model written with its 1M marker is the same model.
	marked := tierReviews(setupNow, "s", supervisorBackground, "big-model[1m]", 3, 1, settled)
	if c := setupTierCount(marked, "big-model", since, setupNow); c.n != 3 || c.reopened != 1 {
		t.Fatalf("1M marker: %+v", c)
	}
}

func TestSetupTierThresholds(t *testing.T) {
	for name, c := range map[string]struct {
		n, reopened int
		lower       string
		want        bool
	}{
		"enough, none reopened":       {setupTierMin, 0, "mid-model", true},
		"enough, one in twenty":       {setupTierMin, 1, "mid-model", true},
		"enough, two in twenty":       {setupTierMin, 2, "mid-model", false},
		"two in forty":                {40, 2, "mid-model", true},
		"three in forty":              {40, 3, "mid-model", false},
		"one short of enough":         {setupTierMin - 1, 0, "mid-model", false},
		"nothing settled":             {0, 0, "mid-model", false},
		"no lower tier known":         {100, 0, "", false},
		"every acceptance was undone": {setupTierMin, setupTierMin, "mid-model", false},
	} {
		if got := (setupState{}).suggests(setupSupervisorTier, setupNumber{n: c.n, reopened: c.reopened, lower: c.lower}); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// newTierService is a manager whose background supervisor is on and there, on provider gw's own default model.
func newTierService(t *testing.T, l proto.ManagedLauncher) (*Service, *fakeRuleEvidence) {
	t.Helper()
	s, ev := newRuleService(t)
	s.noBackground = false
	if err := s.putSetting(context.Background(), launchersManagedKey, []storedLauncher{{ManagedLauncher: l}}); err != nil {
		t.Fatal(err)
	}
	setSettings(t, s, func(v *Settings) {
		v.Main, v.Launcher = "main1", l.Name
		v.BackgroundSupervisor, v.BackgroundSupervisorMachineID = "bg", v.MachineID
	})
	return s, ev
}

func tierHeld(ev *fakeRuleEvidence, now time.Time, model string, n, reopened int) {
	ev.events = tierEvents(ev.events, tierReviews(now, fmt.Sprintf("t%d_", now.Unix()), supervisorBackground, model, n, reopened, 2*supervisionReopenWithin))
}

func TestSetupBlockSuggestsALowerTierWhenTheReviewsHeld(t *testing.T) {
	ctx := context.Background()
	s, ev := newTierService(t, tierLadder())
	now, loc := time.Now(), s.location(ctx)

	tierHeld(ev, now, "big-model", setupTierMin-1, 0)
	if got := setupIDs(s.setupFor(ctx, now, loc, i18n.En, testWeb)); len(got) != 0 {
		t.Fatalf("one acceptance short: %v", got)
	}
	ev.events = nil
	tierHeld(ev, now, "big-model", setupTierMin, 2)
	if got := setupIDs(s.setupFor(ctx, now, loc, i18n.En, testWeb)); len(got) != 0 {
		t.Fatalf("two of twenty reopened: %v", got)
	}
	ev.events = nil
	tierHeld(ev, now, "big-model", setupTierMin, 1)
	b := s.setupFor(ctx, now, loc, i18n.En, testWeb)
	if got := setupIDs(b); !slices.Equal(got, []string{setupSupervisorTier}) {
		t.Fatalf("the reviews held: %v", got)
	}
	sg := b.Suggestions[0]
	if sg.Count != setupTierMin || sg.Setting != purposeSupervise || sg.Link != testWeb+"/settings?tab=models&purpose=assistant.supervise" {
		t.Fatalf("suggestion: %+v", sg)
	}
	for _, want := range []string{"20 acceptances", "reopened among them: 1", "big-model", "mid-model", "Cost and risk:", "Model settings"} {
		if !strings.Contains(sg.Text, want) {
			t.Errorf("no %q in %q", want, sg.Text)
		}
	}
	if strings.Contains(sg.Text, "small-model") {
		t.Errorf("the smallest tier is named: %q", sg.Text)
	}

	// The supervisor's own binding, once it has one, is what is judged: on the middle tier the tier below is the
	// smallest, and nothing is suggested whatever its reviews.
	if r := modelRequest(s, "PATCH", purposeSupervise, `{"revision":0,"model":"mid-model"}`); r.Code != 200 {
		t.Fatalf("saving the binding: %d %s", r.Code, r.Body.String())
	}
	tierHeld(ev, now.Add(-time.Minute), "mid-model", 2*setupTierMin, 0)
	if got := setupIDs(s.setupFor(ctx, now, loc, i18n.En, testWeb)); len(got) != 0 {
		t.Fatalf("on the tier over the smallest: %v", got)
	}
}

func TestSetupBlockSuggestsNoTierItCannotTell(t *testing.T) {
	ctx := context.Background()
	bare := tierLadder()
	bare.HaikuModel, bare.SonnetModel, bare.OpusModel = "", "", ""
	other := tierLadder()
	other.Model = "other-model"
	api := tierLadder()
	api.Kind = "api"
	for name, l := range map[string]proto.ManagedLauncher{
		"a provider with no tier models":        bare,
		"a model that is none of its tiers":     other,
		"a provider that starts no CLI":         api,
		"a launcher that was not added here":    {},
		"only two tiers: below is the smallest": {Name: "gw", BaseURL: "https://models.example", Model: "big-model", SonnetModel: "mid-model", OpusModel: "big-model"},
	} {
		s, ev := newTierService(t, l)
		if l.Name == "" { // the main chat runs on an rc-file launcher, of which nothing is stored
			setSettings(t, s, func(v *Settings) { v.Launcher = "work" })
		}
		now := time.Now()
		tierHeld(ev, now, cmp.Or(l.Model, "big-model"), 2*setupTierMin, 0)
		if b := s.setupFor(ctx, now, s.location(ctx), i18n.En, testWeb); b == nil || len(b.Suggestions) != 0 {
			t.Errorf("%s: %+v", name, b)
		}
	}
}

func TestSetupBlockSuggestsNoTierWithoutTheBackgroundSupervisor(t *testing.T) {
	ctx := context.Background()
	s, ev := newTierService(t, tierLadder())
	now, loc := time.Now(), s.location(ctx)
	tierHeld(ev, now, "big-model", 2*setupTierMin, 0)
	if got := setupIDs(s.setupFor(ctx, now, loc, i18n.En, testWeb)); !slices.Equal(got, []string{setupSupervisorTier}) {
		t.Fatalf("on: %v", got)
	}
	// Switched off: the main conversation follows up its own tasks, whatever the background supervisor reviewed before.
	off := true
	setSettings(t, s, func(v *Settings) { v.MainSupervisesOwnTasks = &off })
	if got := setupIDs(s.setupFor(ctx, now, loc, i18n.En, testWeb)); len(got) != 0 {
		t.Fatalf("off: %v", got)
	}
	// On, but none was ever made.
	setSettings(t, s, func(v *Settings) { v.MainSupervisesOwnTasks, v.BackgroundSupervisor = nil, "" })
	if got := setupIDs(s.setupFor(ctx, now, loc, i18n.En, testWeb)); len(got) != 0 {
		t.Fatalf("none made: %v", got)
	}
	// There, but the reviews were the main conversation's.
	setSettings(t, s, func(v *Settings) { v.BackgroundSupervisor = "bg" })
	ev.events = tierReviews(now, "m", supervisorMain, "big-model", 2*setupTierMin, 0, 2*supervisionReopenWithin)
	if got := setupIDs(s.setupFor(ctx, now, loc, i18n.En, testWeb)); len(got) != 0 {
		t.Fatalf("reviewed by the main conversation: %v", got)
	}
}

// The tier line obeys the block's month, and neither computing nor telling it changes a binding or any other setting.
func TestSetupTierOnceAMonthAndNothingSwitched(t *testing.T) {
	ctx := context.Background()
	s, ev := newTierService(t, tierLadder())
	loc := s.location(ctx)
	monday := time.Now()
	tierHeld(ev, monday, "big-model", setupTierMin, 0)

	store := s.store().(ruleStore)
	snapshot := func() map[string]string {
		store.mu.Lock()
		defer store.mu.Unlock()
		out := map[string]string{}
		for k, v := range store.vals {
			if k != setupKey {
				out[k] = string(v)
			}
		}
		return out
	}
	before, puts := snapshot(), store.puts
	bound, err := s.resolveModel(ctx, purposeSupervise)
	if err != nil {
		t.Fatal(err)
	}

	b := s.setupFor(ctx, monday, loc, i18n.En, testWeb)
	if got := setupIDs(b); !slices.Equal(got, []string{setupSupervisorTier}) {
		t.Fatalf("first briefing of the week: %v", got)
	}
	if store.puts != puts {
		t.Fatalf("computing the line wrote %d settings", store.puts-puts)
	}
	s.setupTold(b, monday)
	if store.puts != puts+1 {
		t.Fatalf("telling the line wrote %d settings, want the block's own book only", store.puts-puts)
	}
	after := snapshot()
	if len(after) != len(before) {
		t.Fatalf("settings changed: %v → %v", before, after)
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("setting %s changed: %s → %s", k, v, after[k])
		}
	}
	if now, err := s.resolveModel(ctx, purposeSupervise); err != nil || now != bound {
		t.Fatalf("the supervisor's binding changed: %+v → %+v (%v)", bound, now, err)
	}
	if book := setupBookOf(t, s); !book.Shown[setupSupervisorTier].Equal(monday.UTC()) {
		t.Fatalf("book: %+v", book)
	}

	for _, weeks := range []int{1, 2, 3, 4} {
		at := monday.Add(time.Duration(weeks) * 7 * 24 * time.Hour)
		tierHeld(ev, at, "big-model", setupTierMin, 0)
		next := s.setupFor(ctx, at, loc, i18n.En, testWeb)
		if next == nil || len(next.Suggestions) != 0 {
			t.Fatalf("week +%d: %+v", weeks, next)
		}
		s.setupTold(next, at)
	}
	later := monday.Add(5 * 7 * 24 * time.Hour)
	tierHeld(ev, later, "big-model", setupTierMin, 0)
	if got := setupIDs(s.setupFor(ctx, later, loc, i18n.En, testWeb)); !slices.Equal(got, []string{setupSupervisorTier}) {
		t.Fatalf("after a month: %v", got)
	}
}

// The line says both models, the two numbers, the cost with the risk and where the binding is, in the three languages.
func TestSetupTierLineIsLocalized(t *testing.T) {
	c := setupNumber{n: 37, reopened: 1, model: "big-model", lower: "mid-model"}
	for l, where := range map[i18n.Lang]string{i18n.En: "Model settings", i18n.ZhHans: "模型配置", i18n.Ja: "モデル設定"} {
		sg := setupWord(l, testWeb, setupSupervisorTier, c)
		for _, want := range []string{"37", "1", "30", "big-model", "mid-model", where, i18n.T(l, "assistant-models.name_assistant_supervise")} {
			if !strings.Contains(sg.Text, want) {
				t.Errorf("%s: no %q in %q", l, want, sg.Text)
			}
		}
		if strings.Contains(sg.Text, "{") || strings.Contains(sg.Text, purposeSupervise) {
			t.Errorf("%s: %q", l, sg.Text)
		}
	}
}

// A provider saved from the web's form keeps its light tier in small_model and no haiku_model; the node runs the
// Haiku tier on it all the same, so it is the smallest tier here too. The line names the two models under the names
// the provider's record has for them, which is how the model picker of Model Settings lists them.
func TestSetupTierReadsTheFormsLightTierAndNamesModelsAsTheProviderDoes(t *testing.T) {
	ctx := context.Background()
	l := proto.ManagedLauncher{Name: "gw", BaseURL: "https://models.example", Model: "big-model",
		SmallModel: "small-model", SonnetModel: "mid-model", OpusModel: "big-model",
		Models: []proto.ProviderModel{{ID: "big-model", Name: "Big"}, {ID: "mid-model[1m]", Name: "Mid"}, {ID: "small-model"}}}
	s, ev := newTierService(t, l)
	now := time.Now()
	tierHeld(ev, now, "big-model", setupTierMin, 0)
	b := s.setupFor(ctx, now, s.location(ctx), i18n.En, testWeb)
	if got := setupIDs(b); !slices.Equal(got, []string{setupSupervisorTier}) {
		t.Fatalf("a provider with its light tier in small_model: %v", got)
	}
	sg := b.Suggestions[0]
	if sg.Count != setupTierMin || !strings.Contains(sg.Text, " on Big ") || !strings.Contains(sg.Text, "also has Mid,") {
		t.Fatalf("suggestion: %+v", sg)
	}
	if strings.Contains(sg.Text, "big-model") || strings.Contains(sg.Text, "mid-model") {
		t.Errorf("a named model is called by its id: %q", sg.Text)
	}
	if got := tierModelName(l, "small-model"); got != "small-model" {
		t.Errorf("a model with no name: %q", got)
	}
}
