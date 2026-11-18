package assistant

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// mutedAsBeforeEmptyCheck is the original evaluation, including its settings read.
func mutedAsBeforeEmptyCheck(s *Service, machineID, sessionID, space string) bool {
	p := s.loadMutes(context.Background())
	key := machineID + "/" + sessionID
	if slices.Contains(p.Sessions, key) {
		return true
	}
	if slices.Contains(p.Allowed, key) {
		return false
	}
	return space != "" && slices.Contains(p.Spaces, machineID+"/"+space)
}

func TestMutedAsMatchesOriginal(t *testing.T) {
	for _, ids := range []struct{ name, machine, session, space string }{
		{"short", "mac", "s1", "app"},
		{"long", strings.Repeat("m", 128), strings.Repeat("s", 256), strings.Repeat("p", 64)},
		{"unicode", "m\u00e4chine", "s\u00e9ance", "\u00e9quipe"},
		{"slashes", "machine/one", "/session/two/", "space/three/"},
		{"empty", "", "", ""},
	} {
		t.Run(ids.name, func(t *testing.T) {
			s := &Service{}
			key, spaceKey := ids.machine+"/"+ids.session, ids.machine+"/"+ids.space
			for _, tc := range []struct {
				name  string
				prefs *Pins
				space string
				want  bool
			}{
				{"unset", nil, ids.space, false},
				{"nil_lists", &Pins{}, ids.space, false},
				{"empty_lists", &Pins{Sessions: []string{}, Spaces: []string{}}, ids.space, false},
				{"allowed_only", &Pins{Allowed: []string{key}}, ids.space, false},
				{"session", &Pins{Sessions: []string{key}}, ids.space, true},
				{"session_before_allowed", &Pins{Sessions: []string{key}, Allowed: []string{key}}, ids.space, true},
				{"space", &Pins{Spaces: []string{spaceKey}}, ids.space, ids.space != ""},
				{"space_allowed", &Pins{Spaces: []string{spaceKey}, Allowed: []string{key}}, ids.space, false},
				{"empty_space", &Pins{Spaces: []string{ids.machine + "/"}}, "", false},
				{"duplicates", &Pins{Sessions: []string{key, key}, Spaces: []string{spaceKey, spaceKey}, Allowed: []string{key, key}}, ids.space, true},
				{"no_match", &Pins{Sessions: []string{"other/agent"}, Spaces: []string{"other/space"}, Allowed: []string{"other/agent"}}, ids.space, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if tc.prefs != nil {
						setMutes(s, *tc.prefs)
					}
					got := s.mutedAs(ids.machine, ids.session, tc.space)
					old := mutedAsBeforeEmptyCheck(s, ids.machine, ids.session, tc.space)
					if got != tc.want || old != tc.want {
						t.Fatalf("mutedAs %v, original %v; want %v", got, old, tc.want)
					}
				})
			}
		})
	}
}

func TestMutedAsConcurrentSettings(t *testing.T) {
	s := &Service{}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		<-start
		for range 1000 {
			s.setMutes(assistantwire.Mutes{})
			s.setMutes(assistantwire.Mutes{Allowed: []string{"mac/s1"}})
			s.setMutes(assistantwire.Mutes{Sessions: []string{"mac/other"}, Spaces: []string{"mac/app"}, Allowed: []string{"mac/s1"}})
		}
	})
	for range 4 {
		wg.Go(func() {
			<-start
			for range 3000 {
				if s.mutedAs("mac", "s1", "app") {
					t.Error("an allowed session became muted during settings updates")
					return
				}
			}
		})
	}
	close(start)
	wg.Wait()
}

var mutedAsBenchmarkResult bool

func BenchmarkMutedAs(b *testing.B) {
	for _, ids := range []struct{ name, machine, session string }{
		{"short", "mac", "s1"},
		{"long", strings.Repeat("m", 64), strings.Repeat("s", 64)},
	} {
		key := ids.machine + "/" + ids.session
		for _, prefs := range []struct {
			name string
			pins Pins
		}{
			{"empty", Pins{}},
			{"allowed_only", Pins{Allowed: []string{key}}},
			{"session", Pins{Sessions: []string{key}}},
			{"space", Pins{Spaces: []string{ids.machine + "/app"}}},
		} {
			b.Run(ids.name+"/"+prefs.name, func(b *testing.B) {
				s := &Service{}
				setMutes(s, prefs.pins)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					mutedAsBenchmarkResult = s.mutedAs(ids.machine, ids.session, "app")
				}
			})
		}
	}
}
