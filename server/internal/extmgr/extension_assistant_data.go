package extmgr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/lazyre"
)

// Assistant content from an extension's data directory (docs/extensions/extensions.md "Assistant content from the data
// directory"): instructions and skills its program writes at run time, where upgrades keep them, read by the hub when it
// renders the assistant's CLAUDE.md and skills. A change of the instructions makes the main conversation resume in a
// fresh process (node assistantProc.instructionsStale) and lose its prompt cache, so a new version of a data
// instructions file is adopted at most once per dataAdoptEvery, when the hub's own instructions change anyway, or when
// the user asks for it (apply); until then the adopted text stays. Skills restart nothing and are read as they are.

const (
	// dataInstructionsMost is the most one data instructions file may weigh; a larger one is not adopted.
	dataInstructionsMost = 6 << 10
	// dataSkillsMost is the most one extension's data skills may weigh, all files together; dataSkillsCount how many.
	dataSkillsMost  = 256 << 10
	dataSkillsCount = 32
	// dataAdoptEvery is how often a new version of a data instructions file may be adopted.
	dataAdoptEvery = 24 * time.Hour
	// assistantDataKey is the setting that keeps what is adopted, so a restart does not open the window again.
	assistantDataKey = "extensions.assistant_data"
)

// Why a data contribution's content as it is now is not what the assistant reads (AssistantDataView.Reason): the
// file's problems, then the hub's.
const (
	dataTooLarge    = "too_large"    // over its limit
	dataInvalid     = "invalid"      // not UTF-8 text
	dataNotRegular  = "not_regular"  // a directory, a device, a link out of the data directory
	dataUnreadable  = "unreadable"   // anything else the system said
	dataRateLimited = "rate_limited" // a new version of instructions waits for its turn (next_adopt_at)
	dataDisabled    = "disabled"     // the extension is switched off (or cannot run): nothing of it is read
	dataSwitchedOff = "switched_off" // this contribution is switched off in the extension's details
)

// What the assistant reads of a data contribution (AssistantDataView.State).
const (
	dataStateAdopted = "adopted" // the content as it is now (instructions: adopted; skills: sent)
	dataStatePending = "pending" // instructions: an older version, the new one waits (rate_limited)
	dataStateKept    = "kept"    // instructions: an older version, the new one cannot be used (Reason says why)
	dataStateNone    = "none"    // nothing: none written yet, or none usable, or the extension or contribution is off
)

// dataSkillName is what a data skill's directory may be called (it becomes the skill's name on the assistant's machine).
var dataSkillName = lazyre.New(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// dataFileError is why a data file could not be used: one of the data* codes, and the system's words when it said any.
type dataFileError struct {
	code string
	err  error
}

func (e *dataFileError) Error() string {
	if e.err != nil {
		return e.code + ": " + e.err.Error()
	}
	return e.code
}

// readDataFile is rel (a slash path) inside dir, the extension's data directory, at most most bytes. Opened through an
// os.Root, so neither ".." nor a symbolic link leads out of it. Nil and no error when there is no such file.
func readDataFile(dir, rel string, most int64) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, &dataFileError{dataUnreadable, err}
	}
	defer root.Close()
	return readRootFile(root.FS(), rel, most)
}

// readRootFile is readDataFile within fsys (an os.Root's).
func readRootFile(fsys fs.FS, rel string, most int64) ([]byte, error) {
	f, err := fsys.Open(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, &dataFileError{dataNotRegular, err}
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, &dataFileError{dataUnreadable, err}
	}
	if !fi.Mode().IsRegular() {
		return nil, &dataFileError{code: dataNotRegular}
	}
	if fi.Size() > most {
		return nil, &dataFileError{dataTooLarge, &dataSize{size: fi.Size(), most: most}}
	}
	b, err := io.ReadAll(io.LimitReader(f, most+1))
	if err != nil {
		return nil, &dataFileError{dataUnreadable, err}
	}
	if int64(len(b)) > most { // it grew since its size was read: what it has now, at least what was read
		size := int64(len(b))
		if fi, err := f.Stat(); err == nil {
			size = max(size, fi.Size())
		}
		return nil, &dataFileError{dataTooLarge, &dataSize{size: size, most: most}}
	}
	return b, nil
}

// dataSize is why a file is too large (dataTooLarge): what it weighs and the most it may.
type dataSize struct{ size, most int64 }

func (e *dataSize) Error() string { return fmt.Sprintf("%d bytes, at most %d", e.size, e.most) }

// tooLargeSize is what a file refused as too large weighs (err from readRootFile); false for any other error.
func tooLargeSize(err error) (int64, bool) {
	var de *dataFileError
	var ds *dataSize
	if errors.As(err, &de) && de.code == dataTooLarge && errors.As(de.err, &ds) {
		return ds.size, true
	}
	return 0, false
}

// invisibleMark: a byte order mark, an embedding or isolate mark (they reorder text on screen), a line or paragraph
// separator.
func invisibleMark(r rune) bool {
	return r == 0xFEFF || r >= 0x202A && r <= 0x202E || r >= 0x2066 && r <= 0x2069 || r == 0x2028 || r == 0x2029
}

// cleanDataText is text an extension wrote for a prompt, made safe to put there: UTF-8 only (else dataInvalid), line
// ends as "\n", without control characters but tab and newline, without invisibleMark characters, and without HTML
// comment openers, so no part of it can pose as the header of another extension's part ("<!-- plugin <id> -->").
// Trimmed.
func cleanDataText(b []byte) (string, error) {
	if !utf8.Valid(b) {
		return "", &dataFileError{code: dataInvalid}
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && (unicode.IsControl(r) || invisibleMark(r)) {
			return -1
		}
		return r
	}, s)
	for strings.Contains(s, "<!--") {
		s = strings.ReplaceAll(s, "<!--", "")
	}
	return strings.TrimSpace(s), nil
}

// dataErrorCode is the data* code of err ("" for none).
func dataErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var de *dataFileError
	if errors.As(err, &de) {
		return de.code
	}
	return dataUnreadable
}

// textDigest names a text the way the status shows it: the first 16 hex digits of its sha256, "" for no text.
func textDigest(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

// dataLog logs line about key once, until the line for key changes: the assistant renders its instructions every
// minute, and a file that stays too large would say so every time.
func (ps *extensions) dataLog(key, line string) {
	if old, ok := ps.dataLogged.Swap(key, line); ok && old == line {
		return
	}
	ps.logf("%s", line)
}

// dataAdoption is the text of one data instructions contribution the assistant reads now, and when it was adopted.
type dataAdoption struct {
	Hash      string    `json:"hash,omitempty"`
	Text      string    `json:"text,omitempty"`
	AdoptedAt time.Time `json:"adopted_at"`
}

// assistantDataState is what assistantDataKey keeps: the adopted texts by "<extension>/<contribution>", and the
// digest of the hub's own instructions the last time the assistant said it (Base), to see them change.
type assistantDataState struct {
	Base    string                  `json:"base,omitempty"`
	Adopted map[string]dataAdoption `json:"adopted,omitempty"`
}

// AssistantDataView is one data contribution as the extension and the apps see it (assistant.data_status,
// GET /api/v1/extensions/{id}/-/assistant-data): State one of dataState*, Reason one of the data* reasons.
type AssistantDataView = assistantwire.AssistantDataItem

// dataKey is a contribution's key in assistantDataState.Adopted.
func dataKey(ext, contribution string) string { return ext + "/" + contribution }

// loadDataState is the kept state (empty when none or unreadable: everything is adopted afresh). Under ps.dataMu.
func (ps *extensions) loadDataState(ctx context.Context) *assistantDataState {
	if ps.dataState != nil {
		return ps.dataState
	}
	st := &assistantDataState{}
	if ps.h != nil && ps.h.store != nil {
		if _, err := ps.h.store.Setting(ctx, assistantDataKey, st); err != nil {
			ps.logf("assistant data: reading what is adopted: %v", err)
		}
	}
	if st.Adopted == nil {
		st.Adopted = map[string]dataAdoption{}
	}
	ps.dataState = st
	return st
}

// saveDataState keeps st. Under ps.dataMu.
func (ps *extensions) saveDataState(ctx context.Context, st *assistantDataState) {
	if ps.h == nil || ps.h.store == nil {
		return
	}
	if err := ps.h.store.PutSetting(ctx, assistantDataKey, st); err != nil {
		ps.logf("assistant data: keeping what is adopted: %v", err)
	}
}

// adoptRule is when a new version of data instructions is adopted now: ride, the hub's own instructions changed (the
// conversation restarts anyway); force, the user asked for it.
type adoptRule struct {
	now         time.Time
	ride, force bool
}

// adoptDecision is what to keep for one contribution: was is what is adopted (found says there is a record), cur the
// text the file has now (curErr when it could not be used: the adopted text stays). It answers the record to keep
// and whether it changed. A first version is adopted at once; after that at most once per dataAdoptEvery, unless
// the rule rides along or is forced.
func adoptDecision(was dataAdoption, found bool, cur string, curErr error, r adoptRule) (dataAdoption, bool) {
	if curErr != nil {
		return was, false
	}
	hash := textDigest(cur)
	if found && hash == was.Hash {
		return was, false
	}
	if !found && cur == "" {
		return was, false // nothing written yet, nothing adopted: no record
	}
	if !found || r.force || r.ride || r.now.Sub(was.AdoptedAt) >= dataAdoptEvery {
		return dataAdoption{Hash: hash, Text: cur, AdoptedAt: r.now}, true
	}
	return was, false
}

// dataInstructionsOf brings p's data instructions up to date under rule r and answers each one's view, the adopted
// text in adopted by contribution id. Under ps.dataMu; dirty says st changed.
func (ps *extensions) dataInstructionsOf(p *extension.Extension, st *assistantDataState, r adoptRule) (views []AssistantDataView, adopted map[string]string, dirty bool) {
	adopted = map[string]string{}
	dir := ps.reg.DataDir(p.Record.ID)
	for _, it := range p.Manifest.DataPrompts(extension.PromptInstructions) {
		key := dataKey(p.Record.ID, it.ID)
		v := AssistantDataView{Contribution: it.ID, Kind: extension.PromptInstructions, Path: it.File, Limit: dataInstructionsMost}
		raw, err := readDataFile(dir, it.File, dataInstructionsMost)
		v.Bytes = len(raw)
		if size, ok := tooLargeSize(err); ok { // refused unread: what the file weighs, not the 0 bytes read
			v.Bytes = int(size)
		}
		var cur string
		if err == nil {
			cur, err = cleanDataText(raw)
		}
		if err != nil {
			v.Reason, v.Detail = dataErrorCode(err), err.Error()
			ps.dataLog(key, fmt.Sprintf("plugin %s: assistant instructions %s: not adopted, %v", p.Record.ID, it.File, err))
		} else {
			v.Hash = textDigest(cur)
			ps.dataLogged.Delete(key)
		}
		was, found := st.Adopted[key]
		next, changed := adoptDecision(was, found, cur, err, r)
		if changed {
			st.Adopted[key], dirty = next, true
			how := "after the wait"
			switch {
			case !found:
				how = "first version"
			case r.force:
				how = "applied by the user"
			case r.ride:
				how = "with the hub's own instructions"
			}
			ps.logf("plugin %s: assistant instructions %s adopted (%s, %s, %d bytes)", p.Record.ID, it.File, how, adoptedName(next.Hash), len(next.Text))
		}
		v.State = dataStateNone
		if found || changed {
			v.AdoptedHash, v.AdoptedAt = next.Hash, assistantwire.FormatDataTime(next.AdoptedAt)
			adopted[it.ID] = next.Text
			switch {
			case err != nil && next.Text != "":
				v.State = dataStateKept
			case err == nil && v.Hash != next.Hash:
				due := assistantwire.FormatDataTime(next.AdoptedAt.Add(dataAdoptEvery))
				v.State, v.Reason, v.NextAdoptAt = dataStatePending, dataRateLimited, due
			case next.Text != "":
				v.State = dataStateAdopted
			}
		}
		views = append(views, v)
	}
	return views, adopted, dirty
}

// adoptedName is a digest for the log, "empty" for no text.
func adoptedName(hash string) string {
	if hash == "" {
		return "empty"
	}
	return hash
}

// dataInstructions are the data instructions parts of the assistant's CLAUDE.md of every extension switched on, each
// under "<!-- plugin <id> (data) -->", by extension; within one, in the manifest's order. base is the digest of the
// hub's own part of the CLAUDE.md they go with ("" from a caller that renders none: the health space).
func (ps *extensions) dataInstructions(ctx context.Context, base string) map[string]string {
	out := map[string]string{}
	if ps == nil {
		return out
	}
	ps.dataMu.Lock()
	defer ps.dataMu.Unlock()
	st := ps.loadDataState(ctx)
	r := adoptRule{now: time.Now()}
	dirty := false
	if base != "" && base != st.Base {
		r.ride = st.Base != "" // the first time there is nothing to compare with
		st.Base, dirty = base, true
	}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" {
			continue
		}
		_, adopted, d := ps.dataInstructionsOf(p, st, r)
		dirty = dirty || d
		var b strings.Builder
		for _, it := range p.Manifest.DataPrompts(extension.PromptInstructions) {
			if text := adopted[it.ID]; text != "" {
				b.WriteString("\n\n<!-- plugin " + p.Record.ID + " (data) -->\n")
				b.WriteString(text)
				b.WriteString("\n")
			}
		}
		if b.Len() > 0 {
			out[p.Record.ID] = b.String()
		}
	}
	if dirty {
		ps.saveDataState(ctx, st)
	}
	return out
}

// assistantDataStatus is the status of extension id's data contributions, all its manifest declares (what is due is
// adopted on the way); force adopts its instructions as they are now (the user's apply). Those of an extension
// switched off, or switched off themselves, say so (state none, reason disabled or switched_off) with what was
// adopted last. Nil when it has none.
func (ps *extensions) assistantDataStatus(ctx context.Context, id string, force bool) []AssistantDataView {
	if ps == nil {
		return nil
	}
	p := ps.reg.Get(id)
	if p == nil || p.Full == nil {
		return nil
	}
	on := p.Record.Enabled && p.Problem == ""
	off := func(v AssistantDataView) AssistantDataView {
		v.State, v.Reason = dataStateNone, dataSwitchedOff
		if !on {
			v.Reason = dataDisabled
		}
		return v
	}
	var instr, skills []AssistantDataView
	ps.dataMu.Lock()
	st := ps.loadDataState(ctx)
	if on {
		views, _, dirty := ps.dataInstructionsOf(p, st, adoptRule{now: time.Now(), force: force})
		if dirty {
			ps.saveDataState(ctx, st)
		}
		instr = views
	}
	for _, it := range p.Full.DataPrompts(extension.PromptInstructions) {
		if on && slices.ContainsFunc(instr, func(v AssistantDataView) bool { return v.Contribution == it.ID }) {
			continue
		}
		v := off(AssistantDataView{Contribution: it.ID, Kind: extension.PromptInstructions, Path: it.File, Limit: dataInstructionsMost})
		if a, ok := st.Adopted[dataKey(p.Record.ID, it.ID)]; ok {
			v.AdoptedHash, v.AdoptedAt = a.Hash, assistantwire.FormatDataTime(a.AdoptedAt)
		}
		instr = append(instr, v)
	}
	ps.dataMu.Unlock()
	if on {
		_, skills = ps.dataSkillsOf(p)
	}
	for _, it := range p.Full.DataSkills() {
		if on && slices.ContainsFunc(skills, func(v AssistantDataView) bool { return v.Contribution == it.ID }) {
			continue
		}
		skills = append(skills, off(AssistantDataView{Contribution: it.ID, Kind: "skills", Path: it.Dir, Limit: dataSkillsMost}))
	}
	// Project and Inbox context (extension_prompt_context.go): read when a prompt is built, nothing to adopt.
	var contexts []AssistantDataView
	if on {
		contexts = ps.contextViews(p)
	}
	for _, kind := range []string{extension.PromptProjectContext, extension.PromptInboxContext} {
		for _, it := range p.Full.DataPrompts(kind) {
			if on && slices.ContainsFunc(contexts, func(v AssistantDataView) bool { return v.Contribution == it.ID }) {
				continue
			}
			v := AssistantDataView{Contribution: it.ID, Kind: kind, Path: it.File, Limit: inboxContextMost}
			if kind == extension.PromptProjectContext {
				v.Path, v.Limit = it.Dir, projectContextMost
			}
			contexts = append(contexts, off(v))
		}
	}
	return append(append(instr, skills...), contexts...)
}

// assistantDataRoutes: GET /api/v1/extensions/{id}/-/assistant-data is the status of its data contributions;
// POST …/assistant-data/apply adopts its data instructions as they are now (the main conversation reads them on its
// next message, in a fresh process). Full credentials only, as full is.
func (h *Manager) assistantDataRoutes(api *http.ServeMux, full func(f func(w http.ResponseWriter, r *http.Request, p *extension.Extension)) http.HandlerFunc) {
	status := func(force bool) http.HandlerFunc {
		return full(func(w http.ResponseWriter, r *http.Request, p *extension.Extension) {
			items := h.extensions.assistantDataStatus(r.Context(), p.Record.ID, force)
			if items == nil {
				items = []AssistantDataView{}
			}
			writeExtensionJSON(w, r, map[string]any{"items": items})
		})
	}
	api.HandleFunc("GET /api/v1/extensions/{id}/-/assistant-data", status(false))
	api.HandleFunc("POST /api/v1/extensions/{id}/-/assistant-data/apply", status(true))
}

// forgetAssistantData drops what was adopted of extension id (it was removed: installed again, it starts afresh).
func (ps *extensions) forgetAssistantData(ctx context.Context, id string) {
	if ps == nil {
		return
	}
	ps.dataMu.Lock()
	defer ps.dataMu.Unlock()
	st := ps.loadDataState(ctx)
	dirty := false
	for k := range st.Adopted {
		if strings.HasPrefix(k, id+"/") {
			delete(st.Adopted, k)
			dirty = true
		}
	}
	if dirty {
		ps.saveDataState(ctx, st)
	}
}

// dataSkillsOf are p's data skills, each subdirectory with a SKILL.md of a skills contribution's dir one skill of
// Markdown files (others are left out), all of them together at most dataSkillsMost bytes and dataSkillsCount skills:
// past that, a contribution sends none. With each contribution's view.
func (ps *extensions) dataSkillsOf(p *extension.Extension) ([]ExtensionSkill, []AssistantDataView) {
	items := p.Manifest.DataSkills()
	if len(items) == 0 {
		return nil, nil
	}
	var out []ExtensionSkill
	var views []AssistantDataView
	root, err := os.OpenRoot(ps.reg.DataDir(p.Record.ID))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		ps.dataLog(p.Record.ID+"/skills", fmt.Sprintf("plugin %s: data skills: %v", p.Record.ID, err))
	}
	if root != nil {
		defer root.Close()
	}
	total, count := 0, 0
	for _, it := range items {
		key := dataKey(p.Record.ID, "skills/"+it.ID)
		v := AssistantDataView{Contribution: it.ID, Kind: "skills", Path: it.Dir, Limit: dataSkillsMost, State: dataStateNone}
		if root == nil {
			views = append(views, v)
			continue
		}
		skills, size, err := readDataSkills(root.FS(), it.Dir, dataSkillsMost-total)
		v.Bytes = size
		switch {
		case err != nil:
			v.Reason, v.Detail = dataErrorCode(err), err.Error()
		case count+len(skills) > dataSkillsCount:
			v.Reason, v.Detail = dataTooLarge, fmt.Sprintf("more than %d skills", dataSkillsCount)
		case len(skills) > 0:
			total, count = total+size, count+len(skills)
			digests := make([]string, 0, len(skills))
			for _, s := range skills {
				s.Extension = p.Record.ID
				out = append(out, s)
				v.Skills = append(v.Skills, s.Name)
				digests = append(digests, s.Name+" "+s.Digest)
			}
			v.State, v.Hash = dataStateAdopted, textDigest(strings.Join(digests, "\n"))
		}
		if v.Reason != "" {
			ps.dataLog(key, fmt.Sprintf("plugin %s: data skills %s: not sent, %s", p.Record.ID, it.Dir, v.Detail))
		} else {
			ps.dataLogged.Delete(key)
		}
		views = append(views, v)
	}
	return out, views
}

// readDataSkills are the skills in dir of fsys: each subdirectory named as a skill may be, with a SKILL.md, its
// Markdown files (in any of its subdirectories) the skill's files. most is how many bytes they may weigh together.
func readDataSkills(fsys fs.FS, dir string, most int) ([]ExtensionSkill, int, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, &dataFileError{dataNotRegular, err}
	}
	var out []ExtensionSkill
	size := 0
	for _, e := range entries {
		if !e.IsDir() || !dataSkillName.MatchString(e.Name()) {
			continue
		}
		skillDir := path.Join(dir, e.Name())
		if fi, err := fs.Stat(fsys, path.Join(skillDir, "SKILL.md")); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		files := map[string][]byte{}
		err := fs.WalkDir(fsys, skillDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(strings.ToLower(p), ".md") {
				return nil // links and anything but Markdown stay out
			}
			b, err := readRootFile(fsys, p, int64(most-size))
			if err != nil {
				if n, ok := tooLargeSize(err); ok {
					size += int(n) // counted as it weighs, so the status says what is there
				}
				return err
			}
			if !utf8.Valid(b) {
				return &dataFileError{dataInvalid, fmt.Errorf("%s is not UTF-8", p)}
			}
			size += len(b)
			files[strings.TrimPrefix(p, skillDir+"/")] = b
			return nil
		})
		if err != nil {
			var de *dataFileError
			if !errors.As(err, &de) {
				err = &dataFileError{dataUnreadable, err}
			}
			return nil, size, err
		}
		out = append(out, ExtensionSkill{Name: e.Name(), Digest: skillDigest(files), Files: files})
	}
	return out, size, nil
}

// skillDigest is a skill's digest: its files' names and contents, in name order.
func skillDigest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write(files[n])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
