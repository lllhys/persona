package hub

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/db"
)

// sharedStore is what a test hands both the hub and the assistant it runs in-process: the hub's store and the
// assistant's in one (as the database they share).
type sharedStore interface {
	Store
	assistant.Store
}

// settingsStore keeps settings in memory like the settings table.
type settingsStore struct {
	sharedStore
	mu   sync.Mutex
	vals map[string][]byte
	puts int
}

func (s *settingsStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.vals[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (s *settingsStore) PutSetting(_ context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vals[key] = b
	s.puts++
	return nil
}

// HoldLiveActivityStart: no start is claimed here, so a phone registering lifts nothing.
func (s *settingsStore) HoldLiveActivityStart(context.Context, string, time.Time, time.Duration) (bool, error) {
	return false, nil
}

func (s *settingsStore) ListTodos(context.Context) ([]db.TodoRow, error) { return nil, nil }
func (s *settingsStore) ListAllTodoSpaces(context.Context) ([]db.TodoSpaceRow, error) {
	return nil, nil
}
func (s *settingsStore) ListAllTodoAgents(context.Context) ([]db.TodoAgentRow, error) {
	return nil, nil
}
