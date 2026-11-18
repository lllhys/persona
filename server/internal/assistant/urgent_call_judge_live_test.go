package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Behaviour checks for urgent calls (#8734): testdata/judge/urgent_call.json holds written-out matters, what the owner
// said, and whether the assistant should ring the owner's phone, the built-in rules and the owner's own ones included.
// TestUrgentScenariosAreWellFormed runs everywhere and keeps the cases consistent with the manager's gates;
// TestUrgentJudgeLive asks a real model and runs only when PERSONA_LIVE_AI is set:
//
//	PERSONA_LIVE_AI='{"base_url":"…","api_key_file":"…","model":"…"}' go test -run UrgentJudgeLive -v ./internal/assistant
//
// docs/assistant/prompts.md ("Behaviour checks") says when to run it.

type urgentScenario struct {
	Name      string   `json:"name"`
	OwnerSaid []string `json:"owner_said"`
	Situation string   `json:"situation"`
	DropRules []string `json:"drop_rules"`
	Want      string   `json:"want"` // call | no_call
	Why       string   `json:"why"`
	Rule      string   `json:"rule"`
	Known     string   `json:"known"`
}

type urgentScenarios struct {
	System    string           `json:"system"`
	Rules     []urgentRule     `json:"rules"`
	Scenarios []urgentScenario `json:"scenarios"`
}

func loadUrgentScenarios(t *testing.T) urgentScenarios {
	t.Helper()
	raw, err := os.ReadFile("testdata/judge/urgent_call.json")
	if err != nil {
		t.Fatal(err)
	}
	var out urgentScenarios
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// rules are the scenario's "what counts as urgent": the built-in ones less those it drops, and the owner's.
func (sc urgentScenario) rules(own []urgentRule) []urgentRule {
	set := defaultUrgentCallSettings()
	rules := slices.DeleteFunc(set.Rules, func(r urgentRule) bool { return slices.Contains(sc.DropRules, r.ID) })
	return append(rules, own...)
}

// prompt is what the model reads: the instructions' urgent-call section with the scenario's rules, the tool's
// description, then the matter.
func (sc urgentScenario) prompt(own []urgentRule) string {
	p := &prompts.UrgentCall{}
	for _, r := range sc.rules(own) {
		p.Rules = append(p.Rules, prompts.UrgentCallRule{ID: r.ID, Text: urgentRuleText(r, "")})
	}
	md := prompts.RenderAssistantClaudeMD(prompts.AssistantClaudeMD{UrgentCall: p})
	section := md[strings.Index(md, "## 加急来电"):]
	var b strings.Builder
	b.WriteString(section)
	b.WriteString("\n\n## urgent_call\n\n")
	b.WriteString(prompts.Tool("urgent_call").Description)
	b.WriteString("\n\n## 刚发生的事\n\n")
	b.WriteString(sc.Situation)
	if len(sc.OwnerSaid) > 0 {
		b.WriteString("\n\n## 用户最近在这个对话里说过\n\n")
		for _, s := range sc.OwnerSaid {
			b.WriteString("- " + s + "\n")
		}
	}
	return b.String()
}

// urgentJudgement is the model's answer.
type urgentJudgement struct {
	Call       bool   `json:"call"`
	Why        string `json:"why"`
	Rule       string `json:"rule"`
	OwnerQuote string `json:"owner_quote"`
	Happening  string `json:"happening"`
	Reason     string `json:"reason"`
}

// mismatch is what is wrong with j for sc ("" when it passes): a call must also pass the manager's gates, as
// urgent_call would check them.
func (sc urgentScenario) mismatch(j urgentJudgement, own []urgentRule, now time.Time) string {
	if sc.Want == "no_call" {
		if j.Call {
			return "called (" + j.Why + " " + j.Rule + ")"
		}
		return ""
	}
	switch {
	case !j.Call:
		return "did not call"
	case j.Why != sc.Why:
		return "why " + j.Why + ", want " + sc.Why
	case sc.Why == urgentWhyRule && j.Rule != sc.Rule:
		return "rule " + j.Rule + ", want " + sc.Rule
	case sc.Why == urgentWhyRule && !slices.ContainsFunc(sc.rules(own), func(r urgentRule) bool { return r.ID == j.Rule }):
		return "no such rule " + j.Rule
	}
	if sc.Why == urgentWhyOwner && !ownerWrote(sc.ownerItems(now), strings.Join(strings.Fields(j.OwnerQuote), " "), now) {
		return fmt.Sprintf("owner_quote %q is not the owner's words", j.OwnerQuote)
	}
	if n := len([]rune(j.Happening)); sc.Why == urgentWhyRule && (n < urgentHappeningMin || n > urgentHappeningMax) {
		return fmt.Sprintf("happening of %d characters", n)
	}
	return ""
}

// ownerItems are the owner's messages as the conversation's transcript has them.
func (sc urgentScenario) ownerItems(now time.Time) []proto.Item {
	var out []proto.Item
	for _, s := range sc.OwnerSaid {
		out = append(out, proto.Item{Role: "user", Kind: proto.KindText, Text: s, TS: now.Add(-time.Hour)})
	}
	return out
}

// TestUrgentScenariosAreWellFormed: every case names a matter and an outcome, a call case gives what the gates need
// (a rule in its list, or the owner's words), and the system prompt asks for the answer's fields.
func TestUrgentScenariosAreWellFormed(t *testing.T) {
	all := loadUrgentScenarios(t)
	if !strings.Contains(all.System, `"call"`) || len(all.Scenarios) < 10 {
		t.Fatalf("%d scenarios, system %q", len(all.Scenarios), all.System)
	}
	seen := map[string]bool{}
	calls, custom := 0, 0
	for _, sc := range all.Scenarios {
		if sc.Name == "" || seen[sc.Name] || sc.Situation == "" {
			t.Errorf("scenario %q: needs a unique name and a situation", sc.Name)
		}
		seen[sc.Name] = true
		switch sc.Want {
		case "no_call":
			if sc.Why != "" || sc.Rule != "" {
				t.Errorf("%s: no_call gives no why or rule", sc.Name)
			}
		case "call":
			calls++
			switch sc.Why {
			case urgentWhyRule:
				if !slices.ContainsFunc(sc.rules(all.Rules), func(r urgentRule) bool { return r.ID == sc.Rule }) {
					t.Errorf("%s: rule %q is not in its list", sc.Name, sc.Rule)
				}
				if strings.HasPrefix(sc.Rule, "r_") {
					custom++
				}
			case urgentWhyOwner:
				if len(sc.OwnerSaid) == 0 {
					t.Errorf("%s: owner_asked needs what the owner said", sc.Name)
				}
			default:
				t.Errorf("%s: why %q", sc.Name, sc.Why)
			}
		default:
			t.Errorf("%s: want %q", sc.Name, sc.Want)
		}
		if !strings.Contains(sc.prompt(all.Rules), sc.Situation) {
			t.Errorf("%s: the prompt leaves out the situation", sc.Name)
		}
	}
	if calls == 0 || calls == len(all.Scenarios) || custom < 2 {
		t.Fatalf("%d of %d cases call, %d on the owner's own rules: keep both kinds, and the owner's rules", calls, len(all.Scenarios), custom)
	}
	// The mismatch check agrees with the gates on a right answer and on wrong ones.
	now := time.Now()
	for _, sc := range all.Scenarios {
		right := urgentJudgement{Call: sc.Want == "call", Why: sc.Why, Rule: sc.Rule, Happening: "这件事正在发生，数字在变"}
		if sc.Why == urgentWhyOwner {
			right.OwnerQuote = sc.OwnerSaid[0]
		}
		if miss := sc.mismatch(right, all.Rules, now); miss != "" {
			t.Errorf("%s: the right answer fails: %s", sc.Name, miss)
		}
		if miss := sc.mismatch(urgentJudgement{Call: sc.Want != "call"}, all.Rules, now); miss == "" {
			t.Errorf("%s: the wrong answer passes", sc.Name)
		}
	}
}

// TestUrgentJudgeLive asks a real model (PERSONA_LIVE_AI) about every case and prints what it decided; a case it gets
// wrong fails, unless the case records it as a known miss. Run it before and after changing the instructions' 加急来电
// section, the urgent_call tool's description or the built-in rules, and put both tables in the PR.
func TestUrgentJudgeLive(t *testing.T) {
	raw := os.Getenv("PERSONA_LIVE_AI")
	if raw == "" {
		t.Skip("PERSONA_LIVE_AI not set")
	}
	var cfg ai.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	all := loadUrgentScenarios(t)
	m := ai.New(cfg)
	var rows []string
	failed := 0
	for _, sc := range all.Scenarios {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		start := time.Now()
		var j urgentJudgement
		err := m.JSON(ctx, all.System, sc.prompt(all.Rules), &j)
		cancel()
		miss := sc.mismatch(j, all.Rules, time.Now())
		got := "no_call"
		if j.Call {
			got = "call " + j.Why + " " + j.Rule
		}
		if err != nil {
			got, miss = "error", "error: "+err.Error()
		}
		mark := liveMark(t, sc.Name, miss, sc.Known, j.Reason)
		if mark == "MISS" {
			failed++
		}
		rows = append(rows, fmt.Sprintf("%s %-28s %-26s want %-22s %5.1fs %s", mark, sc.Name, got, strings.TrimSpace(sc.Want+" "+sc.Why+" "+sc.Rule),
			time.Since(start).Seconds(), j.Reason))
	}
	t.Logf("urgent calls on %s: %d of %d as expected or known\n%s", cfg.Model, len(rows)-failed, len(rows), strings.Join(rows, "\n"))
}
