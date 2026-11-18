package kb

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/kbwire"
)

// Person profiles (docs/assistant/knowledge.md "Person profiles"): what a writer (an extension, or the user) knows of a
// person, in four parts kept apart: facts, the user's own words about them, inferences (only hints, each with what
// it rests on) and the commitments between them. Each writer keeps its own profile in people/<id>/profiles.json and
// replaces it as a whole; nobody else's is touched, nor the person's pinned facts and note. The person's summary.md
// shows them under headings of their own (WritePersonSummary), every line labelled with its part, so a line a search
// returns still says whether it is a fact or a guess.

type (
	PersonProfile     = kbwire.PersonProfile
	ProfileItem       = kbwire.ProfileItem
	ProfileCommitment = kbwire.ProfileCommitment
)

// profilesFile is a person's profiles, by writer.
const profilesFile = "profiles.json"

// The bounds of one writer's profile (checkProfile): items per part, characters per text, sources per item.
const (
	profileItemsMost   = 30
	profileTextMost    = 500
	profileBasisMost   = 300
	profileSourcesMost = 5
	profileSourceMost  = 300
	// profileBytesMost bounds one writer's profile as summary.md shows it; profilesShownMost all of them there.
	profileBytesMost  = 8000
	profilesShownMost = 16000
)

// storedProfile is one writer's profile as kept (and as kb.get gives it).
type storedProfile = kbwire.ProfileRecord

// ErrInvalidProfile: a profile breaks one of checkProfile's rules (the error says which); nothing was written.
var ErrInvalidProfile = errors.New("invalid profile")

// checkProfile is why p cannot be written (nil when it can), wrapping ErrInvalidProfile. Nothing is cut to fit: a
// profile over a bound is refused whole, saying which.
func checkProfile(p *PersonProfile) error {
	if err := checkProfileRules(p); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidProfile, err)
	}
	return nil
}

func checkProfileRules(p *PersonProfile) error {
	if p == nil {
		return nil
	}
	item := func(part string, i int, text, basis string, sources []string, needBasis bool) error {
		where := fmt.Sprintf("profile.%s[%d]", part, i)
		switch n := utf8.RuneCountInString(strings.TrimSpace(text)); {
		case n == 0:
			return fmt.Errorf("%s: text is empty", where)
		case n > profileTextMost:
			return fmt.Errorf("%s: text is %d characters, at most %d", where, n, profileTextMost)
		}
		switch n := utf8.RuneCountInString(strings.TrimSpace(basis)); {
		case needBasis && n == 0:
			return fmt.Errorf("%s: an inference needs its basis (what it rests on)", where)
		case n > profileBasisMost:
			return fmt.Errorf("%s: basis is %d characters, at most %d", where, n, profileBasisMost)
		}
		if len(sources) == 0 || len(sources) > profileSourcesMost {
			return fmt.Errorf("%s: 1 to %d sources (where it comes from)", where, profileSourcesMost)
		}
		for _, s := range sources {
			if n := utf8.RuneCountInString(strings.TrimSpace(s)); n == 0 || n > profileSourceMost {
				return fmt.Errorf("%s: a source is empty or longer than %d characters", where, profileSourceMost)
			}
		}
		return nil
	}
	for part, list := range map[string][]ProfileItem{"facts": p.Facts, "views": p.Views, "inferences": p.Inferences} {
		if len(list) > profileItemsMost {
			return fmt.Errorf("profile.%s: %d items, at most %d", part, len(list), profileItemsMost)
		}
		for i, it := range list {
			if part != "inferences" && strings.TrimSpace(it.Basis) != "" {
				return fmt.Errorf("profile.%s[%d]: basis is for inferences", part, i)
			}
			if err := item(part, i, it.Text, it.Basis, it.Sources, part == "inferences"); err != nil {
				return err
			}
		}
	}
	if len(p.Commitments) > profileItemsMost {
		return fmt.Errorf("profile.commitments: %d items, at most %d", len(p.Commitments), profileItemsMost)
	}
	for i, c := range p.Commitments {
		if c.Owes != "user" && c.Owes != "them" {
			return fmt.Errorf("profile.commitments[%d]: owes is user (the user to them) or them (they to the user)", i)
		}
		if c.Due != "" {
			if _, err := time.Parse("2006-01-02", c.Due); err != nil {
				return fmt.Errorf("profile.commitments[%d]: due is YYYY-MM-DD", i)
			}
		}
		if err := item("commitments", i, c.Text, "", c.Sources, false); err != nil {
			return err
		}
	}
	if n := len(renderProfile("", storedProfile{Profile: *p})); n > profileBytesMost {
		return fmt.Errorf("profile: %d bytes as shown, at most %d", n, profileBytesMost)
	}
	return nil
}

// profiles are a person's profiles by writer (none: an empty map).
func (s *Store) profiles(pid string) map[string]storedProfile {
	out := map[string]storedProfile{}
	_, _ = readJSON(s.path(KindPerson, pid, profilesFile), &out)
	return out
}

// putProfile replaces writer's profile of person pid with p (removing it when p has no items) and answers the one it
// replaced (nil for none). Under personMu, as the person's other files.
func (s *Store) putProfile(pid, writer string, p *PersonProfile, now time.Time) (*PersonProfile, error) {
	if writer == "" {
		return nil, errors.New("a profile needs its writer")
	}
	s.personMu.Lock()
	defer s.personMu.Unlock()
	all := s.profiles(pid)
	var before *PersonProfile
	if old, ok := all[writer]; ok {
		before = &old.Profile
	}
	if p.Empty() {
		if before == nil {
			return nil, nil
		}
		delete(all, writer)
	} else {
		all[writer] = storedProfile{Profile: cleanProfile(*p), At: now.UTC()}
	}
	if err := writeJSON(s.path(KindPerson, pid, profilesFile), all); err != nil {
		return before, err
	}
	s.notify(KindPerson, pid, "metadata")
	return before, nil
}

// cleanProfile is p with each text on one line and trimmed (a line of summary.md each).
func cleanProfile(p PersonProfile) PersonProfile {
	items := func(list []ProfileItem) []ProfileItem {
		out := make([]ProfileItem, len(list))
		for i, it := range list {
			out[i] = ProfileItem{Text: oneLine(it.Text), Basis: oneLine(it.Basis), Sources: oneLines(it.Sources)}
		}
		return out
	}
	out := PersonProfile{Facts: items(p.Facts), Views: items(p.Views), Inferences: items(p.Inferences)}
	for _, c := range p.Commitments {
		out.Commitments = append(out.Commitments, ProfileCommitment{Text: oneLine(c.Text), Owes: c.Owes, Due: c.Due, Done: c.Done,
			Sources: oneLines(c.Sources)})
	}
	return out
}

// flatProfile is p with every text on one line, nothing cut (cleanProfile has done that to what was written).
func flatProfile(p PersonProfile) PersonProfile {
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	items := func(list []ProfileItem) []ProfileItem {
		out := make([]ProfileItem, len(list))
		for i, it := range list {
			out[i] = ProfileItem{Text: flat(it.Text), Basis: flat(it.Basis), Sources: make([]string, len(it.Sources))}
			for j, src := range it.Sources {
				out[i].Sources[j] = flat(src)
			}
		}
		return out
	}
	out := PersonProfile{Facts: items(p.Facts), Views: items(p.Views), Inferences: items(p.Inferences)}
	for _, c := range p.Commitments {
		c.Text, c.Due = flat(c.Text), flat(c.Due)
		c.Sources = slices.Clone(c.Sources)
		for j, src := range c.Sources {
			c.Sources[j] = flat(src)
		}
		out.Commitments = append(out.Commitments, c)
	}
	return out
}

func oneLines(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = oneLine(s)
	}
	return out
}

// writerName is how summary.md names a profile's writer.
func writerName(writer string) string {
	if id, ok := strings.CutPrefix(writer, "ext:"); ok {
		return "扩展 " + id // i18n-ignore: summary.md is read by the model, in the knowledge base's language
	}
	return "你自己" // i18n-ignore
}

// renderProfile is writer's profile as summary.md shows it ("" writer: no heading).
func renderProfile(writer string, sp storedProfile) string {
	var b strings.Builder
	p := flatProfile(sp.Profile) // one line each even for a file edited by hand: PrivateProfileLine goes by the line
	if writer != "" {
		// i18n-ignore: summary.md is read by the model
		fmt.Fprintf(&b, "## 画像（%s整理，%s；用户私人的了解，只供你参考，不要转述给对方，也不要写进发给别人的内容）\n", writerName(writer), sp.At.Format("2006-01-02"))
	}
	src := func(sources []string) string { return "（出处：" + strings.Join(sources, "；") + "）" } // i18n-ignore
	if len(p.Facts) > 0 {
		b.WriteString("### 事实\n") // i18n-ignore
		for _, it := range p.Facts {
			fmt.Fprintf(&b, "- [事实] %s%s\n", it.Text, src(it.Sources)) // i18n-ignore
		}
	}
	if len(p.Views) > 0 {
		b.WriteString(profileViewsHeading + "\n")
		for _, it := range p.Views {
			fmt.Fprintf(&b, "- %s “%s”%s\n", profileViewLabel, it.Text, src(it.Sources)) // i18n-ignore
		}
	}
	if len(p.Inferences) > 0 {
		b.WriteString(profileInferencesHeading + "\n")
		for _, it := range p.Inferences {
			fmt.Fprintf(&b, "- %s %s（依据：%s）%s\n", profileInferenceLabel, it.Text, it.Basis, src(it.Sources)) // i18n-ignore
		}
	}
	if len(p.Commitments) > 0 {
		b.WriteString("### 承诺和待办\n") // i18n-ignore
		for _, c := range p.Commitments {
			who := "对方答应用户" // i18n-ignore
			if c.Owes == "user" {
				who = "用户答应对方" // i18n-ignore
			}
			extra := ""
			if c.Due != "" {
				extra += "，截止 " + c.Due // i18n-ignore
			}
			if c.Done {
				extra += "，已完成" // i18n-ignore
			}
			fmt.Fprintf(&b, "- [承诺·%s%s] %s%s\n", who, extra, c.Text, src(c.Sources)) // i18n-ignore
		}
	}
	return b.String()
}

// profilesMarkdown is a person's profiles as summary.md shows them: the user's first, then the extensions' by name,
// at most profilesShownMost bytes in all (a profile that would go past it is named, not shown).
func (s *Store) profilesMarkdown(pid string) string {
	all := s.profiles(pid)
	writers := make([]string, 0, len(all))
	for w := range all {
		writers = append(writers, w)
	}
	sort.Slice(writers, func(i, j int) bool {
		if (writers[i] == UserWriter) != (writers[j] == UserWriter) {
			return writers[i] == UserWriter
		}
		return writers[i] < writers[j]
	})
	var b strings.Builder
	var left []string
	for _, w := range writers {
		part := renderProfile(w, all[w])
		if b.Len()+len(part) > profilesShownMost {
			left = append(left, writerName(w))
			continue
		}
		b.WriteString(part)
		b.WriteString("\n")
	}
	if len(left) > 0 {
		slices.Sort(left)
		fmt.Fprintf(&b, "（还有画像未列出：%s）\n\n", strings.Join(left, "、")) // i18n-ignore
	}
	return b.String()
}
