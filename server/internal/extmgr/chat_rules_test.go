package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/proto"
)

// memRuleStore is a userRuleStore in memory: what any real storage must do (one rule to a chat, in the order added),
// and failures to order.
type memRuleStore struct {
	mu                         sync.Mutex
	rules                      []connector.UserRule
	listErr, addErr, removeErr error
	adds                       int
	// before, when set, is called first by each call ("list", "add", "remove") and can stand in for a store that hangs
	// (return when ctx ends) or look at what the manager has done by then; a non-nil error is the call's answer.
	before func(ctx context.Context, call string) error
}

func (s *memRuleStore) List(ctx context.Context) ([]connector.UserRule, error) {
	if s.before != nil {
		if err := s.before(ctx, "list"); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	return slices.Clone(s.rules), nil
}

func (s *memRuleStore) Add(ctx context.Context, r connector.UserRule) error {
	if s.before != nil {
		if err := s.before(ctx, "add"); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adds++
	if s.addErr != nil {
		return s.addErr
	}
	if !slices.ContainsFunc(s.rules, func(o connector.UserRule) bool { return o.ID() == r.ID() }) {
		s.rules = append(s.rules, r)
	}
	return nil
}

func (s *memRuleStore) Remove(ctx context.Context, id string) (bool, error) {
	if s.before != nil {
		if err := s.before(ctx, "remove"); err != nil {
			return false, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.removeErr != nil {
		return false, s.removeErr
	}
	n := len(s.rules)
	s.rules = slices.DeleteFunc(s.rules, func(o connector.UserRule) bool { return o.ID() == id })
	return len(s.rules) != n, nil
}

func (s *memRuleStore) ids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, r := range s.rules {
		out = append(out, r.ID())
	}
	return out
}

// chatGitHub is a connector with GitHub's id and write actions, whose writes are only counted.
type chatGitHub struct {
	mu  sync.Mutex
	did []string
}

func (*chatGitHub) ID() string   { return "github" }
func (*chatGitHub) Name() string { return "GitHub" }
func (*chatGitHub) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapActions}}
}
func (*chatGitHub) Status(context.Context) connector.Status {
	return connector.Status{State: connector.StateOK}
}
func (*chatGitHub) Actions() []proto.Tool {
	return []proto.Tool{{Name: "github_comment"}, {Name: "github_review"}}
}
func (*chatGitHub) Describe(_ context.Context, name string, args json.RawMessage) (connector.Action, string, error) {
	return connector.Action{Name: name, Visible: "everyone who can see the repository"}, name + " " + string(args), nil
}
func (c *chatGitHub) Do(_ context.Context, name string, args json.RawMessage) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.did = append(c.did, name+" "+string(args))
	return "done", nil
}
func (c *chatGitHub) done() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.did)
}

// chatLarkCommand is the command of the Lark gate the tests' managers have (withLarkGates), and the one their Lark rows name.
const chatLarkCommand = "feed"

const (
	chatIssue12 = `{"repo":"example-org/example-repo","number":12,"body":"ok"}`
	chatIssue13 = `{"repo":"example-org/example-repo","number":13,"body":"ok"}`
	chatReview  = `{"repo":"example-org/example-repo","number":12,"event":"APPROVE"}`
)

// githubRowOf is the ledger row a gated GitHub write makes.
func githubRowOf(action, args string) db.SentAsUser {
	return sentRow(connector.Sent{At: time.Now(), Connector: "github", Action: action, Args: json.RawMessage(args), Text: "ok",
		Through: connector.LetThrough{Kind: connector.ThroughCard}}, nil)
}

// withLarkGates gives h one extension for each of commands, with a command gate for connector "lark" (a gate file of its
// own, nothing started): the Lark gates chatRuleOfRow asks for, as gateCommands reads them.
func withLarkGates(t *testing.T, h *Manager, commands ...string) {
	t.Helper()
	if h.extensions == nil {
		h.extensions = newExtensions(t.TempDir(), nil, h.logf)
	}
	for i, c := range commands {
		dir := t.TempDir()
		gate := fmt.Sprintf(`{"command": %q, "script": "gate/run.sh", "title": "Gate", "connector": "lark", "env": ["GATE_BASE", "GATE_TOKEN"],
			"base_env": "GATE_BASE", "secret": ["GATE_TOKEN"], "rules": [{"method": "GET", "path": "**", "action": "allow"}],
			"tool": {"name": "gate_%d", "description": "Run it."}}`, c, i)
		if err := os.WriteFile(filepath.Join(dir, "gate.json"), []byte(gate), 0o644); err != nil {
			t.Fatal(err)
		}
		assistant, _ := json.Marshal([]map[string]any{{"id": "gate", "gate": "gate.json"}})
		m := &extension.Manifest{ManifestVersion: extension.ManifestVersion, ID: fmt.Sprintf("example-gate-%d", i), Name: "Example gate", Version: "1.0.0",
			Engines: extension.Engines{ExtensionProtocol: extension.Protocol}, ActivationEvents: []string{extension.OnStartup},
			Runtime: &extension.Runtime{Command: extension.Command{Path: "example-gate"}}, Contributes: map[string]json.RawMessage{"assistant": assistant}}
		if err := h.extensions.reg.AddBuiltin(m, dir, true); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.extensions.gateCommands(connector.Lark); !slices.Equal(got, commands) {
		t.Fatalf("the Lark gates are %v, want %v", got, commands)
	}
}

// chatRuleManager is a manager with its gate running (startConnectorGate: hand-written rules, then the store's), the
// fake GitHub connector, the ledger and a rule store in memory.
func chatRuleManager(t *testing.T, hand []connector.Rule, st *memRuleStore) (*Manager, *chatGitHub) {
	t.Helper()
	h := newTestManager(t)
	keepSessions(t, h) // the devices keep the computers' sessions (listSessions)
	h.store = newSentLedgerStore()
	box := newMachine(h, MachineConfig{ID: "box", Name: "box"}) // a computer whose node gate can tell of a write
	h.machines, h.byID["box"] = append(h.machines, box), box
	h.cfg.ConnectorGate = hand
	h.ruleStore = st
	withLarkGates(t, h, chatLarkCommand)
	h.startConnectorGate(context.Background())
	gh := &chatGitHub{}
	if err := h.addConnector(gh); err != nil {
		t.Fatal(err)
	}
	yes := true
	hubOf(h).answer = func(proto.Interaction) proto.Resolution { return proto.Resolution{Allow: &yes} }
	return h, gh
}

// chatWrite sends a GitHub write as the assistant does and says whether the user was asked.
func chatWrite(t *testing.T, h *Manager, gh *chatGitHub, tool, args string) (asked bool) {
	t.Helper()
	cards, did := len(hubOf(h).asked()), gh.done()
	if r := h.callConnector(context.Background(), asker{machine: "box", session: "conv1"}, tool, json.RawMessage(args)); r.IsError || gh.done() != did+1 {
		t.Fatalf("%s %s: %+v, %d writes after %d", tool, args, r, gh.done(), did)
	}
	return len(hubOf(h).asked()) > cards
}

// The rows the real write paths make give the rules, in the shapes the gates match: a gated GitHub comment, a Lark
// write the node's gate proxy told of. What cannot name one chat gives a reason.
func TestChatRuleOfTheLedgersRows(t *testing.T) {
	h, st := larkSentManager(t)
	withLarkGates(t, h, chatLarkCommand)
	ctx := context.Background()
	for _, s := range []proto.LarkSent{
		{Command: chatLarkCommand, Method: "POST", Path: "/conversations/oc_1/messages", Text: "hello", LetThrough: proto.LarkLetThrough{Kind: proto.LarkThroughAlways}},
		{Command: chatLarkCommand, Method: "POST", Path: "/conversations/oc_2/images", LetThrough: proto.LarkLetThrough{Kind: proto.LarkThroughAlways}},
		{Command: chatLarkCommand, Method: "POST", Path: "/messages/om_3/replies", Text: "re", LetThrough: proto.LarkLetThrough{Kind: proto.LarkThroughAlways}},
		{Command: chatLarkCommand, Method: "POST", Path: "/messages/om_3/reactions", LetThrough: proto.LarkLetThrough{Kind: proto.LarkThroughAlways}},
		{Command: chatLarkCommand, Method: "PUT", Path: "/conversations/oc_1/messages", LetThrough: proto.LarkLetThrough{Kind: proto.LarkThroughAlways}},
		{Command: chatLarkCommand, Method: "POST", Path: "/conversations/oc_*/messages", LetThrough: proto.LarkLetThrough{Kind: proto.LarkThroughAlways}},
	} {
		if _, err := h.RecordNodeSent(ctx, "box", larkSentParams(t, s, nil)); err != nil {
			t.Fatalf("%+v: %v", s, err)
		}
	}
	lark := st.all()
	larkRule := func(conv string) *connector.Rule {
		return &connector.Rule{Connector: "lark", Action: "POST /conversations/" + conv + "/messages", Decision: connector.Allow}
	}
	for i, want := range []struct {
		rule *connector.Rule
		code string
	}{
		{larkRule("oc_1"), ""}, {larkRule("oc_2"), ""}, {nil, connector.NoChatReply}, {nil, connector.NoChatRoute},
		{nil, connector.NoChatRoute}, {nil, connector.NoChatTarget},
	} {
		got, err := h.chatRuleOfRow(lark[i])
		var no *connector.NoChatRuleError
		if want.rule != nil && (err != nil || !reflect.DeepEqual(got.Rule, *want.rule)) ||
			want.rule == nil && (!errors.As(err, &no) || no.Code != want.code) {
			t.Errorf("Lark row %d (%s): %+v, %v; want %+v / %q", i, lark[i].Action, got, err, want.rule, want.code)
		}
	}

	ghRule := &connector.Rule{Connector: "github", Action: "github_comment", Decision: connector.Allow,
		Match: map[string]any{"repo": "example-org/example-repo", "number": float64(12)}}
	for _, c := range []struct {
		name string
		row  db.SentAsUser
		rule *connector.Rule
		code string
	}{
		{"a comment", githubRowOf("github_comment", chatIssue12), ghRule, ""},
		{"a comment in the thread page's form", githubRowOf("github_comment", `{"chat":"example-org/example-repo#12","text":"ok"}`), ghRule, ""},
		{"a review", githubRowOf("github_review", chatReview), nil, connector.NoChatReview},
		{"mail", sentRow(connector.Sent{Connector: "mail", Action: "mail_send", Args: json.RawMessage(`{"to":[{"email":"a@example.com"}],"subject":"hi"}`)}, nil), nil, connector.NoChatMail},
		{"another connector's write", sentRow(connector.Sent{Connector: "example", Action: "example_send", Args: json.RawMessage(`{"chat":"u1","text":"hi"}`)}, nil), nil, connector.NoChatUnknown},
		{"an extension's own record", db.SentAsUser{Connector: "example", Action: sentRecordAction, Target: json.RawMessage(`{"chat":"u1"}`)}, nil, connector.NoChatRecorded},
		{"an extension's record under Lark's id", db.SentAsUser{Connector: "lark", Action: sentRecordAction, Target: json.RawMessage(`{"conversation":"oc_1"}`)}, nil, connector.NoChatRecorded},
	} {
		got, err := h.chatRuleOfRow(c.row)
		var no *connector.NoChatRuleError
		if c.rule != nil && (err != nil || !reflect.DeepEqual(got.Rule, *c.rule)) || c.rule == nil && (!errors.As(err, &no) || no.Code != c.code) {
			t.Errorf("%s: %+v, %v; want %+v / %q", c.name, got, err, c.rule, c.code)
		}
	}
}

// A Lark row is the gated call of a command ("METHOD /path"), and the node names the command in every write it tells of.
// A row whose target does not say a command of a Lark gate (no target, null, {}, [], a number, a string, a command that is
// empty, null, not text, of another case, of another gate, or only a part of the gate's or more) is data that is wrong,
// and nothing is let through on it: reason `command`, from chatRuleOfRow and on the ledger page, before any other reason
// (a reply of such a row too). What is no gated call (an extension's own record, a name that is no "METHOD /path") keeps
// the reason it had.
func TestChatRuleOfALarkRowThatNamesNoLarkGateCommand(t *testing.T) {
	ctx := context.Background()
	h := newTestManager(t)
	withLarkGates(t, h, chatLarkCommand)
	const action = "POST /conversations/oc_1/messages"
	gated := func(target string) db.SentAsUser {
		return db.SentAsUser{Connector: connector.Lark, Action: action, Target: json.RawMessage(target)}
	}
	// What the reason says: the row says no command (data that cannot be read), or says one that is not the gate's.
	const saysNone, notTheGates = "does not say which command", "which is not the Lark gate's"
	for name, c := range map[string]struct{ target, reason string }{
		"no target":                      {``, saysNone},
		"null":                           {`null`, saysNone},
		"an empty object":                {`{}`, saysNone},
		"an empty list":                  {`[]`, saysNone},
		"a number":                       {`7`, saysNone},
		"a string":                       {`"feed"`, saysNone},
		"a conversation and no command":  {`{"conversation":"oc_1"}`, saysNone},
		"an empty command":               {`{"command":"","conversation":"oc_1"}`, saysNone},
		"a null command":                 {`{"command":null,"conversation":"oc_1"}`, saysNone},
		"a number for the command":       {`{"command":5,"conversation":"oc_1"}`, saysNone},
		"a list for the command":         {`{"command":["feed"],"conversation":"oc_1"}`, saysNone},
		"the command in another case":    {`{"command":"FEED","conversation":"oc_1"}`, notTheGates},
		"a command that is not a gate's": {`{"command":"othergate","conversation":"oc_1"}`, notTheGates},
		"the command and more":           {`{"command":"feed2","conversation":"oc_1"}`, notTheGates},
		"a part of the command":          {`{"command":"fee","conversation":"oc_1"}`, notTheGates},
	} {
		row := gated(c.target)
		_, err := h.chatRuleOfRow(row)
		var no *connector.NoChatRuleError
		if !errors.As(err, &no) || no.Code != connector.NoChatCommand || !strings.Contains(no.Reason, c.reason) {
			t.Errorf("%s: %v, want a refusal with reason %s saying %q", name, err, connector.NoChatCommand, c.reason)
		}
		if st := h.chatRuleStates(ctx, []db.SentAsUser{row})[0]; st.Can || st.Reason != connector.NoChatCommand {
			t.Errorf("%s: the ledger page says %+v", name, st)
		}
	}
	// The same row with the gate's command is the control: it gives the rule.
	if got, err := h.chatRuleOfRow(gated(`{"command":"feed","conversation":"oc_1"}`)); err != nil || got.ID() != "lark:oc_1" {
		t.Fatalf("a row that names the gate's command: %+v, %v", got, err)
	}
	// The check comes first: a reply or an image row that names no command says so, before it says what it is.
	for _, action := range []string{"POST /messages/om_1/replies", "POST /conversations/oc_1/reactions"} {
		if _, err := h.chatRuleOfRow(db.SentAsUser{Connector: connector.Lark, Action: action, Target: json.RawMessage(`{}`)}); !errors.As(err, new(*connector.NoChatRuleError)) ||
			err.(*connector.NoChatRuleError).Code != connector.NoChatCommand {
			t.Errorf("%s with no command: %v", action, err)
		}
	}
	// What is no gated call keeps its own reason, command or not.
	for name, c := range map[string]struct {
		row  db.SentAsUser
		code string
	}{
		"an extension's own record": {db.SentAsUser{Connector: connector.Lark, Action: sentRecordAction, Target: json.RawMessage(`{}`)}, connector.NoChatRecorded},
		"a name that is no call":    {db.SentAsUser{Connector: connector.Lark, Action: "lark_send", Target: json.RawMessage(`{}`)}, connector.NoChatRoute},
		"a method in lower case":    {db.SentAsUser{Connector: connector.Lark, Action: "post /conversations/oc_1/messages", Target: json.RawMessage(`{}`)}, connector.NoChatRoute},
	} {
		if _, err := h.chatRuleOfRow(c.row); !errors.As(err, new(*connector.NoChatRuleError)) || err.(*connector.NoChatRuleError).Code != c.code {
			t.Errorf("%s: %v, want reason %s", name, err, c.code)
		}
	}
}

// Making a rule from a row and dropping it change the next write at once, with no restart: the connectors' registry
// (GitHub's comments) and what the node's Lark gate is served (the node pulls it about every minute). The write into
// the chat of the rule goes without a card; the same issue's review, another issue and a Lark reply still ask.
func TestChatRulesApplyWithoutARestart(t *testing.T) {
	ctx := context.Background()
	st := &memRuleStore{}
	h, gh := chatRuleManager(t, nil, st)
	larkRules := func() []proto.LarkRule {
		t.Helper()
		lk, err := h.assistantLark(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return lk.Rules
	}

	if !chatWrite(t, h, gh, "github_comment", chatIssue12) {
		t.Fatal("asked nothing before there was a rule")
	}
	rule, _, err := h.addChatRule(ctx, githubRowOf("github_comment", chatIssue12))
	if err != nil || rule.ID() != "github:example-org/example-repo#12" || rule.At.IsZero() || !slices.Equal(st.ids(), []string{rule.ID()}) {
		t.Fatalf("add: %+v, %v, kept %v", rule, err, st.ids())
	}
	if chatWrite(t, h, gh, "github_comment", chatIssue12) {
		t.Error("the chat of the rule was asked about")
	}
	for _, c := range []struct{ name, tool, args string }{
		{"another issue", "github_comment", chatIssue13},
		{"a review of the same issue", "github_review", chatReview},
	} {
		if !chatWrite(t, h, gh, c.tool, c.args) {
			t.Errorf("%s was not asked about", c.name)
		}
	}
	if got := larkRules(); got != nil {
		t.Errorf("a GitHub rule reached the node's Lark gate: %+v", got)
	}

	// A Lark rule from a row: served to the node's gate on its next pull, in the form it reads.
	h.store.(*sentLedgerStore).rows = nil
	h.store = newSentLedgerStore()
	box := newMachine(h, MachineConfig{ID: "box", Name: "box"})
	h.machines, h.byID["box"] = append(h.machines, box), box
	if _, err := h.RecordNodeSent(ctx, "box", larkSentParams(t, proto.LarkSent{Command: chatLarkCommand, Method: "POST", Path: "/conversations/oc_1/messages", Text: "hi",
		LetThrough: proto.LarkLetThrough{Kind: proto.LarkThroughAlways}}, nil)); err != nil {
		t.Fatal(err)
	}
	larkRule, _, err := h.addChatRule(ctx, h.store.(*sentLedgerStore).all()[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := []proto.LarkRule{{Method: "POST", Path: "/conversations/oc_1/messages", Action: "allow"}}; !reflect.DeepEqual(larkRules(), want) {
		t.Fatalf("served %+v, want %+v", larkRules(), want)
	}
	if got := h.connectors.UserRules(); len(got) != 2 {
		t.Fatalf("the registry has %+v", got)
	}

	// Adding the same chat again is no change.
	if _, _, err := h.addChatRule(ctx, githubRowOf("github_comment", chatIssue12)); err != nil || len(st.ids()) != 2 || len(h.connectors.UserRules()) != 2 {
		t.Fatalf("added twice: %v, kept %v, %d rules", err, st.ids(), len(h.connectors.UserRules()))
	}

	// Dropped: asked again, and the node is served none.
	if had, err := h.removeChatRule(ctx, rule.ID()); err != nil || !had {
		t.Fatalf("remove: %v %v", had, err)
	}
	if !chatWrite(t, h, gh, "github_comment", chatIssue12) {
		t.Error("a dropped rule still let the chat through")
	}
	if had, err := h.removeChatRule(ctx, larkRule.ID()); err != nil || !had || larkRules() != nil || len(h.connectors.UserRules()) != 0 || len(st.ids()) != 0 {
		t.Fatalf("remove Lark: %v %v, served %+v, %d rules, kept %v", had, err, larkRules(), len(h.connectors.UserRules()), st.ids())
	}
	if had, err := h.removeChatRule(ctx, "lark:nowhere"); err != nil || had {
		t.Fatalf("remove what is not there: %v %v", had, err)
	}
}

// A rule that pins one issue (repo and number) is only as good as the gate's reading of the arguments being the
// connector's. A connector reads them with a decoder that ignores case and takes the last of two fields that differ only
// in capitals, so a write that says `repo` and `REPO` (or `number` and `NUMBER`) could satisfy the rule with one and act on
// the other. Whatever the gate does with such a write (ask, or refuse), the connector is not told to write: nobody here
// says yes. A write of the same issue with the plain fields still goes through without a card, so the rule is in force and
// the test is not passing for want of one.
func TestAWriteNamingAFieldTwiceDoesNotReachTheConnectorByAUserRule(t *testing.T) {
	ctx := context.Background()
	h, gh := chatRuleManager(t, nil, &memRuleStore{})
	if _, _, err := h.addChatRule(ctx, githubRowOf("github_comment", chatIssue12)); err != nil {
		t.Fatal(err)
	}
	if chatWrite(t, h, gh, "github_comment", chatIssue12) {
		t.Fatal("the rule is not in force: the plain write asked")
	}
	no := false
	hubOf(h).answer = func(proto.Interaction) proto.Resolution { return proto.Resolution{Allow: &no} }
	for name, args := range map[string]string{
		"repo, then REPO":     `{"repo":"example-org/example-repo","number":12,"REPO":"other-org/other-repo","body":"ok"}`,
		"REPO, then repo":     `{"REPO":"other-org/other-repo","repo":"example-org/example-repo","number":12,"body":"ok"}`,
		"repo, then Repo":     `{"repo":"example-org/example-repo","number":12,"Repo":"other-org/other-repo","body":"ok"}`,
		"number, then NUMBER": `{"repo":"example-org/example-repo","number":12,"NUMBER":13,"body":"ok"}`,
		"NUMBER, then number": `{"NUMBER":13,"repo":"example-org/example-repo","number":12,"body":"ok"}`,
		"number, then Number": `{"repo":"example-org/example-repo","number":12,"Number":13,"body":"ok"}`,
	} {
		did := gh.done()
		r := h.callConnector(ctx, asker{machine: "box", session: "conv1"}, "github_comment", json.RawMessage(args))
		if gh.done() != did {
			t.Errorf("%s: the connector was told to write %s (the call answered %+v)", name, args, r)
		}
	}
}

// What the node's gate is served: the hand-written rules first (connector_gate's Lark rules, then lark_gate's), the
// user's after, so a hand-written deny or confirm still wins in the node's first-match order; and the same rules, nothing
// added, when the user has made none.
func TestServedLarkRulesKeepTheHandWrittenOnesFirst(t *testing.T) {
	ctx := context.Background()
	hand := []connector.Rule{
		{Connector: "lark", Action: "POST /conversations/oc_1/messages", Decision: connector.Deny},
		{Connector: "github", Action: "github_review", Decision: connector.Deny},
	}
	st := &memRuleStore{}
	h, _ := chatRuleManager(t, hand, st)
	h.cfg.LarkGate = []proto.LarkRule{{Method: "POST", Path: "/conversations/oc_2/messages", Action: "confirm"}}
	served := func() proto.AssistantLark {
		lk, err := h.assistantLark(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return lk
	}
	before, _ := json.Marshal(served())
	handWritten := []proto.LarkRule{
		{Method: "POST", Path: "/conversations/oc_1/messages", Action: "deny"},
		{Method: "POST", Path: "/conversations/oc_2/messages", Action: "confirm"},
	}
	if !reflect.DeepEqual(served().Rules, handWritten) {
		t.Fatalf("with no user rule: %+v", served().Rules)
	}

	larkRow := func(conv string) db.SentAsUser {
		return db.SentAsUser{Connector: "lark", Action: "POST /conversations/" + conv + "/messages", Target: json.RawMessage(`{"command":"` + chatLarkCommand + `","conversation":"` + conv + `"}`)}
	}
	for _, conv := range []string{"oc_1", "oc_3"} {
		if _, _, err := h.addChatRule(ctx, larkRow(conv)); err != nil {
			t.Fatal(err)
		}
	}
	want := append(slices.Clone(handWritten),
		proto.LarkRule{Method: "POST", Path: "/conversations/oc_1/messages", Action: "allow"},
		proto.LarkRule{Method: "POST", Path: "/conversations/oc_3/messages", Action: "allow"})
	if got := served().Rules; !reflect.DeepEqual(got, want) {
		t.Fatalf("with user rules: %+v\nwant %+v", got, want)
	}

	// Everything else that is served is as it was, and with the rules dropped so is all of it.
	for _, conv := range []string{"oc_1", "oc_3"} {
		if _, err := h.removeChatRule(ctx, "lark:"+conv); err != nil {
			t.Fatal(err)
		}
	}
	if after, _ := json.Marshal(served()); string(after) != string(before) {
		t.Fatalf("after dropping them: %s\nbefore %s", after, before)
	}
}

// Without a store, or with an empty one, nothing about the gate changes: the registry and what the node is served are
// what the hand-written rules make them.
func TestNoStoreNoUserRules(t *testing.T) {
	ctx := context.Background()
	hand := []connector.Rule{{Connector: "lark", Action: "POST /conversations/*/read-position", Decision: connector.Allow}}
	for name, st := range map[string]userRuleStore{"no store": nil, "an empty store": &memRuleStore{}} {
		h := newTestManager(t)
		h.cfg.ConnectorGate = hand
		if st != nil {
			h.ruleStore = st
		}
		h.startConnectorGate(ctx)
		lk, err := h.assistantLark(ctx)
		if err != nil || !reflect.DeepEqual(lk.Rules, connector.LarkRules(hand, nil)) || h.connectors.UserRules() != nil {
			t.Errorf("%s: served %+v, %v, user rules %+v", name, lk.Rules, err, h.connectors.UserRules())
		}
	}
	// With no database to keep them in, a change says so and changes nothing; so does a manager with no store at all.
	h := newTestManager(t)
	h.startConnectorGate(ctx)
	if _, _, err := h.addChatRule(ctx, githubRowOf("github_comment", chatIssue12)); !errors.Is(err, errNoDatabase) {
		t.Errorf("add without a database: %v", err)
	}
	if _, err := h.removeChatRule(ctx, "github:x/y#1"); !errors.Is(err, errNoDatabase) {
		t.Errorf("remove without a database: %v", err)
	}
	h.ruleStore = nil
	if _, _, err := h.addChatRule(ctx, githubRowOf("github_comment", chatIssue12)); !errors.Is(err, errNoRuleStore) {
		t.Errorf("add without a store: %v", err)
	}
}

// At start the manager reads what the store has and applies it: a gate that asks nothing for those chats from the first
// write, and the node served their Lark rules. A rule the store has that is not a one-chat allow (edited by hand,
// say) is left out and said, the rest still apply; a store that cannot be read leaves the gate as the hand-written rules
// make it, says so, and is read again by the next change, which keeps what was there.
func TestChatRulesAreLoadedAtStart(t *testing.T) {
	ctx := context.Background()
	gh12, _ := connector.ChatRuleFor("github", "github_comment", json.RawMessage(`{"ref":"example-org/example-repo#12"}`))
	lark1, _ := connector.ChatRuleFor("lark", "POST /conversations/oc_1/messages", json.RawMessage(`{"conversation":"oc_1"}`))
	st := &memRuleStore{rules: []connector.UserRule{
		gh12,
		{Rule: connector.Rule{Connector: "*", Action: "*", Decision: connector.Allow}},
		{Rule: connector.Rule{Connector: "lark", Action: "POST /conversations/*/messages", Decision: connector.Allow}},
		lark1,
		{Rule: connector.Rule{Connector: "github", Action: "github_comment", Decision: connector.Deny,
			Match: map[string]any{"repo": "example-org/example-repo", "number": float64(13)}}},
	}}
	h, gh := chatRuleManager(t, nil, st)
	if got := h.connectors.UserRules(); len(got) != 2 {
		t.Fatalf("applied %+v", got)
	}
	if chatWrite(t, h, gh, "github_comment", chatIssue12) || !chatWrite(t, h, gh, "github_comment", chatIssue13) {
		t.Error("the gate does not follow the rules that were loaded")
	}
	if lk, _ := h.assistantLark(ctx); !reflect.DeepEqual(lk.Rules, []proto.LarkRule{{Method: "POST", Path: "/conversations/oc_1/messages", Action: "allow"}}) {
		t.Errorf("served %+v", lk.Rules)
	}

	// A store that cannot be read at start.
	bad := &memRuleStore{rules: []connector.UserRule{gh12}, listErr: errors.New("the settings are not readable")}
	h2, gh2 := chatRuleManager(t, nil, bad)
	if len(h2.connectors.UserRules()) != 0 || !chatWrite(t, h2, gh2, "github_comment", chatIssue12) {
		t.Error("a store that cannot be read let a chat through")
	}
	if _, _, err := h2.addChatRule(ctx, githubRowOf("github_comment", chatIssue13)); err == nil || bad.adds != 0 {
		t.Fatalf("a change on a store that cannot be read: %v, %d adds", err, bad.adds)
	}
	bad.listErr = nil
	if _, _, err := h2.addChatRule(ctx, githubRowOf("github_comment", chatIssue13)); err != nil {
		t.Fatal(err)
	}
	if got := h2.connectors.UserRules(); len(got) != 2 { // the rule that was already there is not lost
		t.Fatalf("after the first change that could read it: %+v", got)
	}
	if chatWrite(t, h2, gh2, "github_comment", chatIssue12) || chatWrite(t, h2, gh2, "github_comment", chatIssue13) {
		t.Error("the rules of the store were not applied once it could be read")
	}
}

// A change that does not happen leaves the gate as it was: a row that offers no rule, a store that refuses the rule. A
// removal the store refuses is the other way round: the user asked for the chat to be asked about again, so it is, and the
// caller is told the rule is still stored (asking again, or the next start, settles it).
func TestChatRuleChangesThatFailChangeNothing(t *testing.T) {
	ctx := context.Background()
	st := &memRuleStore{}
	h, gh := chatRuleManager(t, nil, st)
	var no *connector.NoChatRuleError
	if _, _, err := h.addChatRule(ctx, githubRowOf("github_review", chatReview)); !errors.As(err, &no) || no.Code != connector.NoChatReview || st.adds != 0 {
		t.Fatalf("a review: %v, %d adds", err, st.adds)
	}
	st.addErr = errors.New("disk full")
	if _, _, err := h.addChatRule(ctx, githubRowOf("github_comment", chatIssue12)); err == nil || len(h.connectors.UserRules()) != 0 || !chatWrite(t, h, gh, "github_comment", chatIssue12) {
		t.Fatalf("a store that refuses: %v, rules %+v", err, h.connectors.UserRules())
	}
	st.addErr = nil
	if _, _, err := h.addChatRule(ctx, githubRowOf("github_comment", chatIssue12)); err != nil {
		t.Fatal(err)
	}
	if chatWrite(t, h, gh, "github_comment", chatIssue12) {
		t.Fatal("the rule is not in force")
	}
	st.removeErr = errors.New("disk full")
	if had, err := h.removeChatRule(ctx, "github:example-org/example-repo#12"); err == nil || !had || !chatWrite(t, h, gh, "github_comment", chatIssue12) {
		t.Fatalf("a removal the store refuses: %v %v: the chat must be asked about again", had, err)
	}
	if got := st.ids(); len(got) != 1 {
		t.Fatalf("the store lost the rule it refused to drop: %v", got)
	}
	st.removeErr = nil
	if had, err := h.removeChatRule(ctx, "github:example-org/example-repo#12"); err != nil || !had || len(st.ids()) != 0 || !chatWrite(t, h, gh, "github_comment", chatIssue12) {
		t.Fatalf("the removal, asked again: %v %v, kept %v", had, err, st.ids())
	}
}

// hangUntil is a store call that hangs: it returns when ctx ends, or after three seconds, which the error calls "no
// limit": a test that is told that took longer than its limit.
func hangUntil(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(3 * time.Second):
		return errors.New("still hanging after 3 seconds: the call had no time limit")
	}
}

// shortStoreTime makes a change's time limit short for the test.
func shortStoreTime(t *testing.T) {
	t.Helper()
	was := chatRuleStoreTime
	chatRuleStoreTime = 50 * time.Millisecond
	t.Cleanup(func() { chatRuleStoreTime = was })
}

// A store that hangs holds a change for chatRuleStoreTime, from taking the lock to the answer, and no longer, in whichever
// call it hangs: adding, removing, and reading the store for a rule that is not in force any more. A removal closes the
// gate before it asks the store, so a store that never answers still leaves the chat asked about, and the next change
// is not held up for ever by this one.
func TestAStoreThatHangsHoldsAChangeOnlyForTheTimeLimit(t *testing.T) {
	shortStoreTime(t)
	ctx := context.Background()
	st := &memRuleStore{}
	h, gh := chatRuleManager(t, nil, st)
	hang, gateAtRemove := "", -1
	st.before = func(ctx context.Context, call string) error {
		if call == "remove" {
			gateAtRemove = len(h.connectors.UserRules())
		}
		if call == hang {
			return hangUntil(ctx)
		}
		return nil
	}
	within := func(what string, f func() error) error {
		t.Helper()
		start := time.Now()
		err := f()
		if d := time.Since(start); d > time.Second {
			t.Errorf("%s took %v: the limit is %v", what, d, chatRuleStoreTime)
		}
		return err
	}
	add := func() (connector.UserRule, error) {
		t.Helper()
		var rule connector.UserRule
		err := within("add", func() (err error) {
			rule, _, err = h.addChatRule(ctx, githubRowOf("github_comment", chatIssue12))
			return
		})
		return rule, err
	}

	hang = "add"
	if _, err := add(); !errors.Is(err, context.DeadlineExceeded) || len(h.connectors.UserRules()) != 0 || len(st.ids()) != 0 {
		t.Fatalf("an add that hangs: %v, rules %+v, kept %v", err, h.connectors.UserRules(), st.ids())
	}
	hang = ""
	rule, err := add()
	if err != nil || chatWrite(t, h, gh, "github_comment", chatIssue12) {
		t.Fatalf("the next add, after the one that hung: %v", err)
	}

	// A removal by the name the app has: the gate is closed by the time the store is asked, and the store's hang ends it.
	hang = "remove"
	var had bool
	err = within("remove by key", func() (err error) { _, had, err = h.removeChatRuleKey(ctx, chatRuleKey(rule)); return })
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, errRuleStillStored) || !had || gateAtRemove != 0 {
		t.Fatalf("a removal that hangs: %v, had %v, the gate held %d rules when the store was asked", err, had, gateAtRemove)
	}
	if !chatWrite(t, h, gh, "github_comment", chatIssue12) || len(st.ids()) != 1 {
		t.Fatal("the chat was not asked about after the removal was asked for, or the store lost the rule it never dropped")
	}

	// The rule is stored and no longer in force: finding it means reading the store, which hangs as well.
	hang = "list"
	err = within("remove of a rule that is not in force", func() (err error) { _, had, err = h.removeChatRuleKey(ctx, chatRuleKey(rule)); return })
	if !errors.Is(err, context.DeadlineExceeded) || had {
		t.Fatalf("a read that hangs: %v, had %v", err, had)
	}

	// The same through the id the rule has in the store.
	hang = ""
	if _, err := add(); err != nil {
		t.Fatal(err)
	}
	hang = "remove"
	err = within("remove by id", func() (err error) { had, err = h.removeChatRule(ctx, rule.ID()); return })
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, errRuleStillStored) || !had {
		t.Fatalf("a removal by id that hangs: %v, had %v", err, had)
	}

	// Nothing is held: the removal, once the store answers, finishes.
	hang = ""
	if _, had, err := h.removeChatRuleKey(ctx, chatRuleKey(rule)); err != nil || !had || len(st.ids()) != 0 {
		t.Fatalf("the removal, asked again: %v %v, kept %v", had, err, st.ids())
	}
}

// Changes made at once, from several requests, end with the gate holding exactly what the store holds.
func TestChatRuleChangesAtOnce(t *testing.T) {
	ctx := context.Background()
	st := &memRuleStore{}
	h, _ := chatRuleManager(t, nil, st)
	var wg sync.WaitGroup
	for n := range 12 {
		wg.Add(2)
		args := fmt.Sprintf(`{"repo":"example-org/example-repo","number":%d,"body":"ok"}`, n+1)
		go func() {
			defer wg.Done()
			if _, _, err := h.addChatRule(ctx, githubRowOf("github_comment", args)); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			_, _ = h.removeChatRule(ctx, fmt.Sprintf("github:example-org/example-repo#%d", n+1))
		}()
	}
	wg.Wait()
	var gate []string
	for _, r := range h.connectors.UserRules() {
		gate = append(gate, fmt.Sprintf("github:%s#%d", r.Match["repo"], int(r.Match["number"].(float64))))
	}
	kept := st.ids()
	slices.Sort(gate)
	slices.Sort(kept)
	if !slices.Equal(gate, kept) {
		t.Fatalf("the gate has %v, the store %v", gate, kept)
	}
	if len(h.userRules.list) != len(kept) {
		t.Fatalf("the manager's list has %d, the store %d", len(h.userRules.list), len(kept))
	}
}

// Every route the ledger knows to create a message in a conversation (the node's lark.sent rows of it) offers the
// conversation's rule: the two lists of routes, the ledger's and the rule maker's, are one.
func TestChatRuleOfEveryRouteTheLedgerKnows(t *testing.T) {
	h := newTestManager(t)
	withLarkGates(t, h, chatLarkCommand)
	for _, route := range nodeCreationRoutes {
		row := db.SentAsUser{Connector: connector.Lark, Action: "POST /conversations/oc_1/" + route, Target: json.RawMessage(`{"command":"` + chatLarkCommand + `","conversation":"oc_1"}`)}
		if got, err := h.chatRuleOfRow(row); err != nil || got.ID() != "lark:oc_1" {
			t.Errorf("%s: %+v, %v", route, got, err)
		}
	}
}
