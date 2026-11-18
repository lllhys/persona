package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// profileProbeLauncher writes a ~/.zshrc whose launcher work runs claude in profile, counting every time the shell
// runs it (the probe does, in a subshell with claude stubbed; a start does too) in the file it returns.
func profileProbeLauncher(t *testing.T, profile string) string {
	t.Helper()
	home := os.Getenv("HOME")
	runs := filepath.Join(home, "work-runs")
	writeFile(t, filepath.Join(home, ".zshrc"), `function work { print -r -- run >> `+shellQuote(runs)+`; CLAUDE_CONFIG_DIR=`+
		shellQuote(profile)+` command claude "$@"; }`+"\n")
	return runs
}

// settleFile dates path back past profileProbeSettle, as a ~/.zshrc nobody is editing.
func settleFile(t *testing.T, path string) { settleFileBy(t, path, time.Hour) }

// settleFileBy dates path back by ago: two edits settled by different amounts keep different times on a file system
// that keeps only whole seconds.
func settleFileBy(t *testing.T, path string, ago time.Duration) {
	t.Helper()
	old := time.Now().Add(-ago)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func launcherRuns(t *testing.T, runs string) int {
	t.Helper()
	b, _ := os.ReadFile(runs)
	return strings.Count(string(b), "run\n")
}

// A message to a running assistant conversation does not run its shell launcher again: the profile check reuses the
// probe of the message before it (#8947). Through the RPC boundary, as the hub sends.
func TestAssistantMessageDoesNotProbeTheLauncherEachTime(t *testing.T) {
	f := cliTest(t)
	n := f.n
	n.home = os.Getenv("HOME")
	profile := t.TempDir()
	runs := profileProbeLauncher(t, profile)
	settleFile(t, filepath.Join(n.home, ".zshrc"))
	cwd := filepath.Join(n.dir, "assistant")
	answer, err := nodeCall(t, n, "assistant.send", map[string]any{"launcher": "work", "cwd": cwd, "text": "hello", "model_fingerprint": "probe-test"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(answer, &res); err != nil || res.SessionID == "" {
		t.Fatalf("send: %s (%v)", answer, err)
	}
	f.idle(t, res.SessionID)
	if st, _ := n.state.get(res.SessionID); st.ConfigDir != profile {
		t.Fatalf("the conversation did not start in its launcher's profile: %q, want %q", st.ConfigDir, profile)
	}
	before := launcherRuns(t, runs)
	for _, text := range []string{"again", "and again"} {
		if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": res.SessionID, "text": text}); err != nil {
			t.Fatal(err)
		}
		f.idle(t, res.SessionID)
	}
	if got := launcherRuns(t, runs) - before; got != 0 {
		t.Fatalf("two messages to a running conversation ran its launcher %d more times; want none (one login shell each is a second per message)", got)
	}
}

// The check still holds: a launcher edited to set another config directory is refused on the very next message, not
// after the cached probe ages out, and a launcher that sets the conversation's own directory again is let through.
// Each edit is settled before the message, as one made seconds earlier: the file's stamp must tell, not its age.
func TestAssistantMessageSeesAnEditedLauncherAtOnce(t *testing.T) {
	f := cliTest(t)
	n := f.n
	n.home = os.Getenv("HOME")
	profile := t.TempDir()
	profileProbeLauncher(t, profile)
	settleFile(t, filepath.Join(n.home, ".zshrc"))
	cwd := filepath.Join(n.dir, "assistant")
	answer, err := nodeCall(t, n, "assistant.send", map[string]any{"launcher": "work", "cwd": cwd, "text": "hello", "model_fingerprint": "probe-test"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(answer, &res); err != nil || res.SessionID == "" {
		t.Fatalf("send: %s (%v)", answer, err)
	}
	f.idle(t, res.SessionID)
	profileProbeLauncher(t, t.TempDir()) // the user points the launcher at another profile, the same length of text
	settleFileBy(t, filepath.Join(n.home, ".zshrc"), 2*time.Hour)
	_, err = nodeCall(t, n, "session.send", map[string]any{"session_id": res.SessionID, "text": "after the edit"})
	if err == nil || !strings.Contains(err.Error(), "conflicts with the launcher's own profile") {
		t.Fatalf("a launcher moved to another profile was not refused on the next message: %v", err)
	}
	profileProbeLauncher(t, profile) // and back
	settleFileBy(t, filepath.Join(n.home, ".zshrc"), 3*time.Hour)
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": res.SessionID, "text": "back"}); err != nil {
		t.Fatalf("the launcher's own profile again was refused: %v", err)
	}
}

// What the stamp cannot see (a file the startup files source) is seen once the probe is profileProbeFresh old, and a
// failed probe is tried again after profileProbeRetry.
func TestAssistantLauncherProfileProbeAgesOut(t *testing.T) {
	f := cliTest(t)
	n := f.n
	n.home = os.Getenv("HOME")
	sourced := filepath.Join(n.home, "work.zsh")
	first, second := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(n.home, ".zshrc"), "source "+shellQuote(sourced)+"\n")
	settleFile(t, filepath.Join(n.home, ".zshrc"))
	define := func(profile string) {
		writeFile(t, sourced, `function work { CLAUDE_CONFIG_DIR=`+shellQuote(profile)+` command claude "$@"; }`+"\n")
	}
	age := func(by time.Duration) {
		profileProbes.Lock()
		defer profileProbes.Unlock()
		c := profileProbes.got[n.home+"\x00work"]
		c.at = c.at.Add(-by)
		profileProbes.got[n.home+"\x00work"] = c
	}
	ctx := context.Background()
	if got := n.assistantLauncherProfile(ctx, "work"); got != "" {
		t.Fatalf("a launcher that is not defined yet has profile %q", got)
	}
	define(first)
	if got := n.assistantLauncherProfile(ctx, "work"); got != "" {
		t.Fatalf("a failed probe was tried again before profileProbeRetry: %q", got)
	}
	age(profileProbeRetry)
	if got := n.assistantLauncherProfile(ctx, "work"); got != first {
		t.Fatalf("a failed probe was not tried again after profileProbeRetry: %q, want %q", got, first)
	}
	define(second)
	if got := n.assistantLauncherProfile(ctx, "work"); got != first {
		t.Fatalf("a probe younger than profileProbeFresh was taken again for an unwatched file: %q", got)
	}
	age(profileProbeFresh)
	if got := n.assistantLauncherProfile(ctx, "work"); got != second {
		t.Fatalf("a probe older than profileProbeFresh was reused: %q, want %q", got, second)
	}
}

// sendFirst starts an assistant conversation on launcher work through the RPC boundary, as the hub does, and waits for
// the end of its turn.
func sendFirst(t *testing.T, f *cliFixture, params map[string]any) string {
	t.Helper()
	answer, err := nodeCall(t, f.n, "assistant.send", params)
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(answer, &res); err != nil || res.SessionID == "" {
		t.Fatalf("send: %s (%v)", answer, err)
	}
	f.idle(t, res.SessionID)
	return res.SessionID
}

// ageProbes moves every kept probe of home back by d, as if d had passed since each was taken and last used, and the
// agent-kind and model-check results kept from them (launcherProbeCached, launcherModelCheck) with them.
func ageProbes(home string, d time.Duration) {
	profileProbes.Lock()
	for k, c := range profileProbes.got {
		if c.home == home {
			c.at, c.used = c.at.Add(-d), c.used.Add(-d)
			profileProbes.got[k] = c
		}
	}
	profileProbes.Unlock()
	probeCache.Lock()
	for k, at := range probeCache.at {
		probeCache.at[k] = at.Add(-d)
	}
	probeCache.Unlock()
	modelProbeCache.Lock()
	for k, r := range modelProbeCache.results {
		if strings.HasPrefix(k, home+"\x00") {
			r.at = r.at.Add(-d)
			modelProbeCache.results[k] = r
		}
	}
	modelProbeCache.Unlock()
}

// A start of an assistant conversation's process runs its launcher once, to start it: the probe of its compaction
// settings is the one the message's checks just read, and the first message of a new conversation takes one probe for
// the agent, profile and compaction checks together, plus one for the model it passes on (#9097). Each took its own
// login shell before, one after another.
func TestAssistantStartRunsTheLauncherOnlyToStartIt(t *testing.T) {
	f := cliTest(t)
	n := f.n
	n.home = os.Getenv("HOME")
	for _, k := range claudeModelVars { // a model the developer's own shell exports is not the launcher's
		t.Setenv(k, "")
	}
	profile := t.TempDir()
	runs := profileProbeLauncher(t, profile)
	settleFile(t, filepath.Join(n.home, ".zshrc"))
	cwd := filepath.Join(n.dir, "assistant")
	send := map[string]any{"launcher": "work", "cwd": cwd, "text": "hello", "model": "claude-opus-4-1", "model_fingerprint": "probe-test"}
	id := sendFirst(t, f, send)
	if got := launcherRuns(t, runs); got != 3 {
		t.Fatalf("a new conversation's first message ran its launcher %d times; want 3: one probe, one with --model, one start", got)
	}
	n.stopAssistantProcess(id) // its process ended (the reaper, an idle stop): the next message starts it again
	before := launcherRuns(t, runs)
	send["session_id"], send["text"] = id, "after a while"
	sendFirst(t, f, send)
	if got := launcherRuns(t, runs) - before; got != 1 {
		t.Fatalf("a cold start of the conversation ran its launcher %d times; want 1, the start itself", got)
	}
	if st, _ := n.state.get(id); st.ConfigDir != profile {
		t.Fatalf("the restarted conversation is not in its launcher's profile: %q, want %q", st.ConfigDir, profile)
	}
	// A resume with no model configured asks what the launcher would run on (resumeModel): the same probe again.
	before = launcherRuns(t, runs)
	if got := n.resumeModel("work", cwd, "", profile); got != "default" {
		t.Fatalf("resume model: %q, want default", got)
	}
	if got := launcherRuns(t, runs) - before; got != 0 {
		t.Fatalf("the resume model ran the launcher %d times; want none, the message's probe is kept", got)
	}
}

// A kept probe in use is taken again before it ages out, so a message after a quiet spell longer than
// profileProbeFresh does not wait for a login shell; an edit is still seen on the very next message; a probe nobody
// used in profileProbeKeep is left to age out (#9097).
func TestKeptProbeIsRenewedBeforeItAgesOut(t *testing.T) {
	f := cliTest(t)
	n := f.n
	n.home = os.Getenv("HOME")
	profile := t.TempDir()
	runs := profileProbeLauncher(t, profile)
	rc := filepath.Join(n.home, ".zshrc")
	settleFile(t, rc)
	cwd := filepath.Join(n.dir, "assistant")
	id := sendFirst(t, f, map[string]any{"launcher": "work", "cwd": cwd, "text": "hello", "model": "claude-opus-4-1", "model_fingerprint": "probe-test"})
	ctx := context.Background()

	if got := n.renewKeptProbes(ctx); got != 0 {
		t.Fatalf("a probe taken just now was renewed (%d)", got)
	}
	ageProbes(n.home, profileProbeFresh-profileProbeRenew/2) // a quiet spell
	key := keptProbeKey(n.home, "work", nil)
	profileProbes.Lock()
	used := profileProbes.got[key].used
	profileProbes.Unlock()
	before := launcherRuns(t, runs)
	if got := n.renewKeptProbes(ctx); got != 2 {
		t.Fatalf("renewed %d probes about to age out; want the launcher's two (plain, and passing the model)", got)
	}
	if got := launcherRuns(t, runs) - before; got != 2 {
		t.Fatalf("the renewal ran the launcher %d times; want 2", got)
	}
	profileProbes.Lock()
	c := profileProbes.got[key]
	profileProbes.Unlock()
	if time.Since(c.at) > time.Minute || !c.used.Equal(used) {
		t.Fatalf("after the renewal the probe is from %v and last used %v; want taken now and still last used %v (a renewal is no use: it would keep itself going forever)", c.at, c.used, used)
	}
	ageProbes(n.home, profileProbeRenew) // past when the first probes would have aged out
	before = launcherRuns(t, runs)
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": id, "text": "after the quiet spell"}); err != nil {
		t.Fatal(err)
	}
	f.idle(t, id)
	if got := launcherRuns(t, runs) - before; got != 0 {
		t.Fatalf("a message after the quiet spell ran the launcher %d times; want none, its probe was renewed", got)
	}

	profileProbeLauncher(t, t.TempDir()) // renewed or not, an edit is seen at once
	settleFileBy(t, rc, 2*time.Hour)
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": id, "text": "after the edit"}); err == nil ||
		!strings.Contains(err.Error(), "conflicts with the launcher's own profile") {
		t.Fatalf("a launcher moved to another profile after a renewal was not refused on the next message: %v", err)
	}

	profileProbes.Lock() // about to age out again, but nobody used it in profileProbeKeep
	for k, c := range profileProbes.got {
		if c.home == n.home {
			c.at, c.used = time.Now().Add(-(profileProbeFresh - profileProbeRenew/2)), time.Now().Add(-profileProbeKeep)
			profileProbes.got[k] = c
		}
	}
	profileProbes.Unlock()
	if got := n.renewKeptProbes(ctx); got != 0 {
		t.Fatalf("renewed %d probes nobody used in profileProbeKeep", got)
	}
}

// Callers asking for the same probe at the same time share one login shell, and one whose context ends first does not
// stop it: the next caller gets what it found.
func TestKeptProbeSharesOneShell(t *testing.T) {
	fakeClaudeHome(t, fakeCLI)
	n := codexTestNode(t)
	home := os.Getenv("HOME")
	n.home = home
	runs := filepath.Join(home, "slow-runs")
	writeFile(t, filepath.Join(home, ".zshrc"), `function slow { print -r -- run >> `+shellQuote(runs)+`; sleep 1; command claude "$@"; }`+"\n")
	settleFile(t, filepath.Join(home, ".zshrc"))
	short, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := n.keptProbe(short, "slow"); err == nil {
		t.Fatal("a caller that gave up after 100ms got a probe of a launcher that takes a second")
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p, err := n.keptProbe(context.Background(), "slow"); err != nil || p.Agent != "claude" {
				t.Errorf("probe: %+v (%v)", p, err)
			}
		}()
	}
	wg.Wait()
	if got := launcherRuns(t, runs); got != 1 {
		t.Fatalf("four callers ran the launcher %d times; want one shell for all of them", got)
	}
}
