package assistant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func seedReadinessFailure(t *testing.T, s *Service, at time.Time) {
	t.Helper()
	if err := s.readinessUpdate(context.Background(), func(r *readinessRecord) {
		r.Probe = &readinessProbe{Machine: "server", Launcher: "claude", At: at,
			Check: proto.LauncherCheck{Error: "Invalid API key"}}
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReadinessRecoverySurvivesOfflineAndReload(t *testing.T) {
	for _, observe := range []string{"readiness", "completed turn"} {
		t.Run(observe, func(t *testing.T) {
			at := time.Now().Add(-24 * time.Hour).UTC()
			main := proto.Session{SessionID: "m1", Assistant: true, Launcher: "claude",
				Status: proto.StatusDone, State: proto.StateIdle, LastText: "Recovered answer", UpdatedAt: at}
			f, s, st := readyMachine(t, main)
			seedReadinessFailure(t, s, at.Add(-time.Hour))
			if observe == "readiness" {
				if r := getReadiness(t, s, "", "en"); !r.Ready {
					t.Fatalf("successful answer did not recover: %+v", r)
				}
			} else {
				// Completion must be remembered even if the client closes before polling readiness.
				s.assistantTurnSettled(context.Background(), "server", "m1")
			}
			main.Status, main.State = proto.StatusOffline, proto.StateOffline
			f.setSessions("server", main)
			for _, service := range []*Service{s, newServiceOn(t, f, st)} {
				r := getReadiness(t, service, "", "en")
				if !r.Ready || r.Mode != ReadinessFull || readinessStep(t, r, StepModel).State != StepOK {
					t.Fatalf("offline/reload resurrected the older failure: %+v", r)
				}
			}
			var kept readinessRecord
			if _, err := st.Setting(context.Background(), readinessKey, &kept); err != nil || kept.Probe != nil {
				t.Fatalf("superseded failed probe was retained: %+v, %v", kept.Probe, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.checked) != 0 {
				t.Fatalf("recovery unexpectedly ran model probes: %v", f.checked)
			}
		})
	}
}

func TestReadinessRecoveryDoesNotValidateChangedConfiguration(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	main := proto.Session{SessionID: "m1", Assistant: true, Launcher: "claude", Model: "example-model",
		Status: proto.StatusDone, State: proto.StateIdle, LastText: "Recovered answer", UpdatedAt: at}
	f, s, st := readyMachine(t, main)
	seedReadinessFailure(t, s, at.Add(-time.Minute))
	getReadiness(t, s, "", "en")
	// Even picking the same concrete model changes the binding from the launcher's default.
	r := modelRequest(s, "PATCH", "assistant", `{"revision":0,"provider":"launcher.claude","model":"example-model"}`)
	if r.Code != 200 {
		t.Fatalf("change model binding: %d %s", r.Code, r.Body.String())
	}
	if m := readinessStep(t, getReadiness(t, s, "", "en"), StepModel); m.State == StepOK {
		t.Fatalf("old live reply validated changed configuration: %+v", m)
	}
	main.Status, main.State = proto.StatusOffline, proto.StateOffline
	f.setSessions("server", main)
	s = newServiceOn(t, f, st)
	if m := readinessStep(t, getReadiness(t, s, "", "en"), StepModel); m.State == StepOK || m.Code == proto.TurnErrorAuthFailed {
		t.Fatalf("old success or superseded failure crossed configuration change: %+v", m)
	}
	main.Status, main.State, main.UpdatedAt = proto.StatusDone, proto.StateIdle, at.Add(time.Second)
	f.setSessions("server", main)
	s.assistantTurnSettled(context.Background(), "server", "m1")
	main.Status, main.State = proto.StatusOffline, proto.StateOffline
	f.setSessions("server", main)
	if m := readinessStep(t, getReadiness(t, newServiceOn(t, f, st), "", "en"), StepModel); m.State != StepOK {
		t.Fatalf("new reply did not validate current configuration: %+v", m)
	}
}

func TestReadinessRecoveryRejectsAnotherExecutionModel(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "launcher default"
		if explicit {
			name = "configured model"
		}
		t.Run(name, func(t *testing.T) {
			at := time.Now().Add(-time.Minute).UTC()
			main := proto.Session{SessionID: "m1", Assistant: true, Launcher: "claude", Model: "different-model",
				Status: proto.StatusDone, State: proto.StateIdle, LastText: "Another model answered", UpdatedAt: at}
			f, s, st := readyMachine(t, main)
			f.agentModels = map[string][]proto.AgentModel{"claude": {{ID: "configured-model", Default: true}, {ID: "different-model"}}}
			if explicit {
				if r := modelRequest(s, "PATCH", "assistant", `{"revision":0,"provider":"launcher.claude","model":"configured-model"}`); r.Code != 200 {
					t.Fatalf("configure model: %d %s", r.Code, r.Body.String())
				}
			}
			seedReadinessFailure(t, s, at.Add(-time.Minute))
			s.assistantTurnSettled(context.Background(), "server", "m1")
			if kept := s.readinessRecordNow(context.Background()); kept.Success != nil || kept.Probe == nil {
				t.Fatalf("another model's reply changed the configured binding's evidence: %+v", kept)
			}
			main.Status, main.State = proto.StatusOffline, proto.StateOffline
			f.setSessions("server", main)
			if step := readinessStep(t, getReadiness(t, newServiceOn(t, f, st), "", "en"), StepModel); step.State == StepOK {
				t.Fatalf("another model validated the configured binding after release/reload: %+v", step)
			}
		})
	}
}

func TestReadinessRecoveryAcceptsLauncherDefaultModel(t *testing.T) {
	at := time.Now().Add(-time.Minute).UTC()
	main := proto.Session{SessionID: "m1", Assistant: true, Launcher: "claude", Model: "configured-model",
		Status: proto.StatusDone, State: proto.StateIdle, LastText: "The default model answered", UpdatedAt: at}
	f, s, st := readyMachine(t, main)
	f.agentModels = map[string][]proto.AgentModel{"claude": {{ID: "configured-model", Default: true}, {ID: "different-model"}}}
	seedReadinessFailure(t, s, at.Add(-time.Minute))
	s.assistantTurnSettled(context.Background(), "server", "m1")
	main.Status, main.State = proto.StatusOffline, proto.StateOffline
	f.setSessions("server", main)
	if r := getReadiness(t, newServiceOn(t, f, st), "", "en"); !r.Ready {
		t.Fatalf("the launcher's actual default model lost successful reply evidence: %+v", r)
	}
}

func TestReadinessRecoveryAcceptsResolvedModelAlias(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	main := proto.Session{SessionID: "m1", Assistant: true, Launcher: "claude", Model: "example-canonical-model",
		Status: proto.StatusDone, State: proto.StateIdle, LastText: "Recovered answer", UpdatedAt: at}
	f, s, st := readyMachine(t, main)
	f.agentModels = map[string][]proto.AgentModel{"claude": {{ID: "example-alias", Resolved: "example-canonical-model"}, {ID: "different-model"}}}
	r := modelRequest(s, "PATCH", "assistant", `{"revision":0,"provider":"launcher.claude","model":"example-alias"}`)
	if r.Code != 200 {
		t.Fatalf("configure model alias: %d %s", r.Code, r.Body.String())
	}
	seedReadinessFailure(t, s, at.Add(-time.Minute))
	s.assistantTurnSettled(context.Background(), "server", "m1")
	main.Status, main.State = proto.StatusOffline, proto.StateOffline
	f.setSessions("server", main)
	if r := getReadiness(t, newServiceOn(t, f, st), "", "en"); !r.Ready {
		t.Fatalf("resolved model alias lost successful reply: %+v", r)
	}
}

func TestReadinessRecoveryRetriesFailedPersistence(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	main := proto.Session{SessionID: "m1", Assistant: true, Launcher: "claude",
		Status: proto.StatusDone, State: proto.StateIdle, LastText: "Recovered answer", UpdatedAt: at}
	f, s, st := readyMachine(t, main)
	seedReadinessFailure(t, s, at.Add(-time.Minute))
	st.mu.Lock()
	st.failPut = errors.New("temporary storage failure")
	st.mu.Unlock()
	s.assistantTurnSettled(context.Background(), "server", "m1")
	if s.readinessRecordNow(context.Background()).Success != nil {
		t.Fatal("failed write was treated as persisted success")
	}
	st.mu.Lock()
	st.failPut = nil
	st.mu.Unlock()
	getReadiness(t, s, "", "en")
	main.Status, main.State = proto.StatusOffline, proto.StateOffline
	f.setSessions("server", main)
	if r := getReadiness(t, newServiceOn(t, f, st), "", "en"); !r.Ready {
		t.Fatalf("recovery was not persisted after storage recovered: %+v", r)
	}
}

func TestReadinessRecoveryRepeatedReadsDoNotRewriteEvidence(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	_, s, st := readyMachine(t, proto.Session{SessionID: "m1", Assistant: true, Launcher: "claude",
		Status: proto.StatusDone, State: proto.StateIdle, LastText: "Recovered answer", UpdatedAt: at})
	seedReadinessFailure(t, s, at.Add(-time.Minute))
	getReadiness(t, s, "", "en")
	st.mu.Lock()
	before := st.puts
	st.mu.Unlock()
	getReadiness(t, s, "", "en")
	getReadiness(t, s, "", "en")
	st.mu.Lock()
	after := st.puts
	st.mu.Unlock()
	if before != after {
		t.Fatalf("unchanged readiness rewrote evidence: %d -> %d", before, after)
	}
	if got := s.readinessRecordNow(context.Background()).Success.At; !got.Equal(at) {
		t.Fatalf("evidence timestamp changed: got %s, want %s", got, at)
	}
}

func TestReadinessRecoveryDoesNotInventSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, status, state, text, launcher string
		starting                            bool
	}{
		{"running", proto.StatusWorking, proto.StateWorking, "Partial reply", "claude", false},
		{"interrupted", proto.StatusInterrupted, proto.StateOffline, "Partial reply", "claude", false},
		{"offline without evidence", proto.StatusOffline, proto.StateOffline, "Unverified text", "claude", false},
		{"empty", proto.StatusDone, proto.StateIdle, " ", "claude", false},
		{"starting", proto.StatusDone, proto.StateIdle, "Old reply", "claude", true},
		{"error", proto.StatusDone, proto.StateIdle, "Invalid API key", "claude", false},
		{"other launcher", proto.StatusDone, proto.StateIdle, "Other reply", "other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Now().Add(-time.Minute)
			main := proto.Session{SessionID: "m1", Assistant: true, Launcher: tc.launcher,
				Status: tc.status, State: tc.state, LastText: tc.text, UpdatedAt: at, Starting: tc.starting}
			f, s, st := readyMachine(t, main)
			seedReadinessFailure(t, s, at.Add(-time.Minute))
			s.assistantTurnSettled(context.Background(), "server", "m1")
			main.Status, main.State = proto.StatusOffline, proto.StateOffline
			f.setSessions("server", main)
			r := getReadiness(t, newServiceOn(t, f, st), "", "en")
			if r.Ready || readinessStep(t, r, StepModel).Code != proto.TurnErrorAuthFailed {
				t.Fatalf("unproven answer cleared the failure: %+v", r)
			}
		})
	}
}

func TestReadinessRecoveryKeepsNewerFailures(t *testing.T) {
	for _, tc := range []struct{ name, text, code string }{
		{"authentication", "Invalid API key", proto.TurnErrorAuthFailed},
		{"routing", "Unknown model", proto.TurnErrorModelRoute},
		{"rate limit", "API Error: 429 Too Many Requests", proto.TurnErrorRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Now().Add(-time.Hour)
			main := proto.Session{SessionID: "m1", Assistant: true, Launcher: "claude",
				Status: proto.StatusDone, State: proto.StateIdle, LastText: "Recovered answer", UpdatedAt: at}
			f, s, st := readyMachine(t, main)
			seedReadinessFailure(t, s, at.Add(-time.Hour))
			getReadiness(t, s, "", "en")
			if err := s.readinessUpdate(context.Background(), func(r *readinessRecord) {
				r.Probe = &readinessProbe{Machine: "server", Launcher: "claude", At: at.Add(time.Minute),
					Check: proto.LauncherCheck{Error: tc.text}}
			}); err != nil {
				t.Fatal(err)
			}
			// A delayed callback for the earlier success cannot retire this newer failure.
			s.assistantTurnSettled(context.Background(), "server", "m1")
			main.Status, main.State = proto.StatusOffline, proto.StateOffline
			f.setSessions("server", main)
			r := getReadiness(t, newServiceOn(t, f, st), "", "en")
			m := readinessStep(t, r, StepModel)
			if m.State != StepProblem || m.Code != tc.code {
				t.Fatalf("newer failure hidden by saved success: %+v", r)
			}
		})
	}
}

func TestReadinessRecoveryFailureWinsTimestampTie(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	main := proto.Session{SessionID: "m1", Assistant: true, Launcher: "claude",
		Status: proto.StatusDone, State: proto.StateIdle, LastText: "Recovered answer", UpdatedAt: at}
	f, s, st := readyMachine(t, main)
	getReadiness(t, s, "", "en")
	seedReadinessFailure(t, s, at)
	main.Status, main.State = proto.StatusOffline, proto.StateOffline
	f.setSessions("server", main)
	r := getReadiness(t, newServiceOn(t, f, st), "", "en")
	if r.Ready || readinessStep(t, r, StepModel).Code != proto.TurnErrorAuthFailed {
		t.Fatalf("saved success hid a same-time failure: %+v", r)
	}
}
