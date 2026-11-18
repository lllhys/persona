package saved

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/i18n"
)

// Saved prompts (#7329, docs/web/web.md "Saved prompts"): the user's own "/" prompts, the instructions they send
// again and again. The user keeps them in Settings › Saved Prompts (or saves one of their messages as one); every chat's
// "/" list offers them after its built-in commands, and picking one fills the composer with its text, never sends it.
// They are this assistant's settings, as saved places and repository bindings are, so every device shows the same list.
// The assistant itself has no tools for them: its fixed payload stays as it was.

// promptStore is the part of the assistant's store the prompts use.
type promptStore interface {
	Setting(ctx context.Context, key string, out any) (bool, error)
	PutSetting(ctx context.Context, key string, value any) error
}

// PromptsKey is where the prompts are kept, in the order the "/" list shows them.
const PromptsKey = "assistant.saved_prompts"

// The limits of a prompt: its name is the word after "/" (one word, so the composer's "/word" finds it), its text what
// it types.
const (
	PromptNameMax = 32
	PromptTextMax = 8000
	PromptsMax    = 100
)

// Prompt is one of the user's saved prompts.
type Prompt struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// savedPromptInput is a prompt as the apps send it.
type savedPromptInput struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// promptBook serializes the changes: each reads the list, changes it and writes it back.
type promptBook struct {
	h  Host
	mu sync.Mutex
}

var (
	errSavedPromptsStore = i18n.Err("assistant-saved-prompts.err_store")
	errSavedPromptRead   = i18n.Err("assistant-saved-prompts.err_read_only")
	errSavedPromptGone   = i18n.Err("assistant-saved-prompts.err_not_found")
	errSavedPromptName   = i18n.Err("assistant-saved-prompts.err_name", i18n.Args{"max": PromptNameMax})
	errSavedPromptText   = i18n.Err("assistant-saved-prompts.err_text", i18n.Args{"max": PromptTextMax})
	errSavedPromptsFull  = i18n.Err("assistant-saved-prompts.err_too_many", i18n.Args{"max": PromptsMax})
	errSavedPromptOrder  = i18n.Err("assistant-saved-prompts.err_order")
)

// savedPromptNameTaken is the error for a name another prompt has.
func savedPromptNameTaken(name string) error {
	return i18n.Err("assistant-saved-prompts.err_name_taken", i18n.Args{"name": name})
}

// savedPromptNameRune: a name is letters (any script), digits and "-", "_", "." — one word the composer's "/word" can
// hold, with no "/" or "@" that would start another command or a mention.
func savedPromptNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || r == '-' || r == '_' || r == '.'
}

// savedPromptTextRune: the text may run over several lines and hold tabs, nothing else the composer can't show.
func savedPromptTextRune(r rune) bool {
	return r == '\n' || r == '\t' || !unicode.IsControl(r)
}

// prompt checks in and makes it a prompt (no id or times yet): the name trimmed and without a "/" typed before it, the
// text's line ends made "\n" and its surrounding blank space dropped.
func (in savedPromptInput) prompt() (Prompt, error) {
	name := strings.TrimPrefix(strings.TrimSpace(in.Name), "/")
	if name == "" || utf8.RuneCountInString(name) > PromptNameMax || strings.ContainsFunc(name, func(r rune) bool { return !savedPromptNameRune(r) }) {
		return Prompt{}, errSavedPromptName
	}
	text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(in.Text, "\r\n", "\n"), "\r", "\n"))
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > PromptTextMax ||
		strings.ContainsFunc(text, func(r rune) bool { return !savedPromptTextRune(r) }) {
		return Prompt{}, errSavedPromptText
	}
	return Prompt{Name: name, Text: text}, nil
}

// sameSavedPromptName: two names are the same prompt's, case aside (the "/" list matches without it).
func sameSavedPromptName(a, b string) bool { return strings.EqualFold(a, b) }

// savedPrompts are the saved prompts, in the "/" list's order.
func (b *promptBook) prompts(ctx context.Context) ([]Prompt, error) {
	st, ok := b.h.Store().(promptStore)
	if !ok {
		return nil, errSavedPromptsStore
	}
	var prompts []Prompt
	if _, err := st.Setting(ctx, PromptsKey, &prompts); err != nil {
		return nil, err
	}
	return prompts, nil
}

// changeSavedPrompts runs change on the saved prompts and keeps what it returns, one change at a time; the apps are told
// (event assistant_saved_prompts), so another open page reads the list again.
func (b *promptBook) change(ctx context.Context, change func([]Prompt) ([]Prompt, error)) ([]Prompt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	prompts, err := b.prompts(ctx)
	if err != nil {
		return nil, err
	}
	next, err := change(slices.Clone(prompts))
	if err != nil {
		return nil, err
	}
	st, ok := b.h.Store().(promptStore)
	if !ok {
		return nil, errSavedPromptsStore
	}
	if err := st.PutSetting(ctx, PromptsKey, next); err != nil {
		return nil, err
	}
	b.h.Event(ctx, map[string]any{"type": "assistant_saved_prompts"})
	return next, nil
}

// addSavedPrompt adds p at the end of the list; a prompt of the same name is an error.
func (b *promptBook) add(ctx context.Context, p Prompt) (Prompt, error) {
	_, err := b.change(ctx, func(prompts []Prompt) ([]Prompt, error) {
		if slices.ContainsFunc(prompts, func(x Prompt) bool { return sameSavedPromptName(x.Name, p.Name) }) {
			return nil, savedPromptNameTaken(p.Name)
		}
		if len(prompts) >= PromptsMax {
			return nil, errSavedPromptsFull
		}
		now := time.Now().UTC()
		p.ID, p.CreatedAt, p.UpdatedAt = newSavedPromptID(prompts, now), now, now
		return append(prompts, p), nil
	})
	return p, err
}

// newSavedPromptID is an id no prompt in prompts has: the time, moved on past one taken (a clock as coarse as a
// microsecond gives two prompts added in a row the same time).
func newSavedPromptID(prompts []Prompt, now time.Time) string {
	for n := now.UnixNano(); ; n++ {
		id := fmt.Sprintf("sp%d", n)
		if !slices.ContainsFunc(prompts, func(x Prompt) bool { return x.ID == id }) {
			return id
		}
	}
}

// updateSavedPrompt changes prompt id's name and text, keeping its place in the list; a name another prompt has is an
// error.
func (b *promptBook) update(ctx context.Context, id string, p Prompt) (Prompt, error) {
	_, err := b.change(ctx, func(prompts []Prompt) ([]Prompt, error) {
		i := slices.IndexFunc(prompts, func(x Prompt) bool { return x.ID == id })
		if i < 0 {
			return nil, errSavedPromptGone
		}
		if slices.ContainsFunc(prompts, func(x Prompt) bool { return x.ID != id && sameSavedPromptName(x.Name, p.Name) }) {
			return nil, savedPromptNameTaken(p.Name)
		}
		p.ID, p.CreatedAt, p.UpdatedAt = id, prompts[i].CreatedAt, time.Now().UTC()
		prompts[i] = p
		return prompts, nil
	})
	return p, err
}

// deleteSavedPrompt removes prompt id; one that is not there is not an error (deleting twice is fine).
func (b *promptBook) delete(ctx context.Context, id string) error {
	_, err := b.change(ctx, func(prompts []Prompt) ([]Prompt, error) {
		return slices.DeleteFunc(prompts, func(x Prompt) bool { return x.ID == id }), nil
	})
	return err
}

// orderSavedPrompts puts the prompts in the order of ids, which must name each saved prompt once: a list that changed
// since the page read it (one added, deleted, or named twice) is refused, and the page reads it again.
func (b *promptBook) order(ctx context.Context, ids []string) ([]Prompt, error) {
	return b.change(ctx, func(prompts []Prompt) ([]Prompt, error) {
		if len(ids) != len(prompts) {
			return nil, errSavedPromptOrder
		}
		at := make(map[string]int, len(prompts))
		for i, p := range prompts {
			at[p.ID] = i
		}
		out := make([]Prompt, 0, len(prompts))
		for _, id := range ids {
			i, ok := at[id]
			if !ok {
				return nil, errSavedPromptOrder
			}
			delete(at, id) // named twice: the second is not found
			out = append(out, prompts[i])
		}
		return out, nil
	})
}

// writeSavedPromptError answers a failed read or change of the prompts with its status.
func writeSavedPromptError(w http.ResponseWriter, err error) {
	var msg *i18n.Message
	switch {
	case errors.Is(err, errSavedPromptsStore):
		apierr.Write(w, http.StatusServiceUnavailable, err)
	case errors.Is(err, errSavedPromptGone):
		apierr.Write(w, http.StatusNotFound, err)
	case errors.Is(err, errSavedPromptOrder), errors.As(err, &msg) && msg.Key == "assistant-saved-prompts.err_name_taken":
		apierr.Write(w, http.StatusConflict, err)
	case errors.As(err, &msg):
		apierr.Write(w, http.StatusBadRequest, err)
	default:
		apierr.Write(w, http.StatusInternalServerError, err)
	}
}

// listedSavedPrompts is the list as the routes answer it: never null.
func listedSavedPrompts(prompts []Prompt) map[string]any {
	if prompts == nil {
		prompts = []Prompt{}
	}
	return map[string]any{"prompts": prompts}
}

func (b *promptBook) routes() {
	b.h.Handle("GET /api/v1/assistant/saved-prompts", func(w http.ResponseWriter, r *http.Request) {
		prompts, err := b.prompts(r.Context())
		if err != nil {
			writeSavedPromptError(w, err)
			return
		}
		apierr.WriteJSON(w, listedSavedPrompts(prompts))
	})
	// writable: a device that may change the prompts; false when it answered 403.
	writable := func(w http.ResponseWriter, r *http.Request) bool {
		if !b.h.Full(r) {
			apierr.Write(w, http.StatusForbidden, errSavedPromptRead)
			return false
		}
		return true
	}
	// readPrompt is the prompt in the request's body; false when it answered an error.
	readPrompt := func(w http.ResponseWriter, r *http.Request) (Prompt, bool) {
		if !writable(w, r) {
			return Prompt{}, false
		}
		var in savedPromptInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return Prompt{}, false
		}
		p, err := in.prompt()
		if err != nil {
			writeSavedPromptError(w, err)
			return Prompt{}, false
		}
		return p, true
	}
	b.h.Handle("POST /api/v1/assistant/saved-prompts", func(w http.ResponseWriter, r *http.Request) {
		p, ok := readPrompt(w, r)
		if !ok {
			return
		}
		p, err := b.add(r.Context(), p)
		if err != nil {
			writeSavedPromptError(w, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"prompt": p})
	})
	b.h.Handle("PUT /api/v1/assistant/saved-prompts/order", func(w http.ResponseWriter, r *http.Request) {
		if !writable(w, r) {
			return
		}
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		prompts, err := b.order(r.Context(), in.IDs)
		if err != nil {
			writeSavedPromptError(w, err)
			return
		}
		apierr.WriteJSON(w, listedSavedPrompts(prompts))
	})
	b.h.Handle("PUT /api/v1/assistant/saved-prompts/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := readPrompt(w, r)
		if !ok {
			return
		}
		p, err := b.update(r.Context(), r.PathValue("id"), p)
		if err != nil {
			writeSavedPromptError(w, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"prompt": p})
	})
	b.h.Handle("DELETE /api/v1/assistant/saved-prompts/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !writable(w, r) {
			return
		}
		if err := b.delete(r.Context(), r.PathValue("id")); err != nil {
			writeSavedPromptError(w, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"ok": true})
	})
}
