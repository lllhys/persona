package kb

import (
	"fmt"
	"slices"

	"github.com/nwverse/persona/server/internal/kbwire"
)

// Which update fields each kind of entry uses (apply). A field a kind does not use is taken out of the update before
// it is applied, so it is neither locked nor attributed for nothing, and the writer is told (IgnoredField) instead of
// the value going silently (docs/assistant/knowledge.md "Fields each kind takes").

// IgnoredField: a field an update carried that its kind does not use.
type IgnoredField = kbwire.IgnoredField

// commonFields are every kind's: the tagging and the pinned facts.
var commonFields = []string{"importance", "lock", "pinned", "relevance", "tags"}

var kindFields = map[string][]string{
	KindProject: {"aliases", "chats", "dirs", "goal", "log", "merge_into", "my_role", "name", "note", "people", "project_kind",
		"remove_aliases", "remove_chats", "remove_people", "source", "status", "summary"},
	KindChat:    {"name", "projects", "remove_people", "remove_projects", "summary"},
	KindPerson:  {"note", "profile", "projects", "remove_chats", "remove_projects", "status"},
	KindDoc:     {"note", "projects", "remove_projects"},
	KindMeeting: {"note", "projects", "remove_projects"},
	KindEvent:   {"note", "projects", "remove_projects"},
	// A topic's summary is refused with its own error (apply): it is written from the topic's points.
	KindTopic:     {"aliases", "name", "note", "remove_aliases", "summary"},
	KindRecording: {"projects", "remove_projects"},
}

// setFields are the fields u carries, by their JSON names.
func setFields(u Update) []string {
	var out []string
	for name, set := range map[string]bool{
		"pinned": u.Pinned != nil, "lock": u.Lock != nil, "remove_people": len(u.RemovePeople) > 0, "remove_chats": len(u.RemoveChats) > 0,
		"remove_aliases": len(u.RemoveAliases) > 0, "remove_projects": len(u.RemoveProjects) > 0, "name": u.Name != "",
		"project_kind": u.Kind != "", "status": u.Status != "", "goal": u.Goal != "", "my_role": u.MyRole != "", "note": u.Note != "",
		"importance": u.Importance != "", "relevance": u.Relevance != nil, "tags": u.Tags != nil, "summary": u.Summary != "",
		"log": u.Log != "", "source": u.Source != "", "aliases": len(u.Aliases) > 0, "chats": len(u.Chats) > 0,
		"people": len(u.People) > 0, "dirs": len(u.Dirs) > 0, "projects": len(u.Projects) > 0, "merge_into": u.MergeInto != "",
		"profile": u.Profile != nil,
	} {
		if set {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// Unsupported are the fields u carries that kind does not use, and why, in name order; none for an unknown kind
// (apply refuses it).
func Unsupported(kind string, u Update) []IgnoredField {
	own, known := kindFields[kind]
	if !known {
		return nil
	}
	var out []IgnoredField
	for _, f := range setFields(u) {
		switch {
		case slices.Contains(commonFields, f) || slices.Contains(own, f) && !(f == "source" && u.Log == ""):
			continue
		case f == "source":
			out = append(out, IgnoredField{Field: f, Reason: "source names who writes a log line: it goes with log only"})
		case kind == KindPerson && (f == "summary" || f == "log"):
			out = append(out, IgnoredField{Field: f, Reason: "a person's summary.md is written from what is known of them: put what you know in profile (or pinned, note)"})
		default:
			out = append(out, IgnoredField{Field: f, Reason: fmt.Sprintf("%s entries do not use %s", kind, f)})
		}
	}
	return out
}

// stripUnsupported is u without the fields kind does not use (Unsupported).
func stripUnsupported(kind string, u Update) Update {
	for _, f := range Unsupported(kind, u) {
		switch f.Field {
		case "pinned":
			u.Pinned = nil
		case "remove_people":
			u.RemovePeople = nil
		case "remove_chats":
			u.RemoveChats = nil
		case "remove_aliases":
			u.RemoveAliases = nil
		case "remove_projects":
			u.RemoveProjects = nil
		case "name":
			u.Name = ""
		case "project_kind":
			u.Kind = ""
		case "status":
			u.Status = ""
		case "goal":
			u.Goal = ""
		case "my_role":
			u.MyRole = ""
		case "note":
			u.Note = ""
		case "summary":
			u.Summary = ""
		case "log":
			u.Log = ""
		case "source":
			u.Source = ""
		case "aliases":
			u.Aliases = nil
		case "chats":
			u.Chats = nil
		case "people":
			u.People = nil
		case "dirs":
			u.Dirs = nil
		case "projects":
			u.Projects = nil
		case "merge_into":
			u.MergeInto = ""
		case "profile":
			u.Profile = nil
		}
	}
	return u
}
