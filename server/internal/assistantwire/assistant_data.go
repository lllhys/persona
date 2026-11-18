package assistantwire

import "time"

// What an extension reads back of the assistant content it writes in its data directory
// (schema/extension/assistant_data.schema.json, docs/extensions/extensions.md "Assistant content from the data
// directory"); the extension manager answers it.

// MethodAssistantDataStatus is the call: no parameters, the calling extension's own data contributions.
const MethodAssistantDataStatus = "assistant.data_status"

// AssistantDataStatusParams are assistant.data_status's parameters: none.
type AssistantDataStatusParams struct{}

// AssistantDataStatusResult is assistant.data_status's answer.
type AssistantDataStatusResult struct {
	Items []AssistantDataItem `json:"items"`
}

// AssistantDataItem is one data contribution as the extension and the apps see it.
type AssistantDataItem struct {
	Contribution string `json:"contribution"` // its id, as the manifest has it
	Kind         string `json:"kind"`         // instructions | skills
	Path         string `json:"path"`         // its file or directory in the data directory
	Limit        int    `json:"limit"`        // bytes
	Bytes        int    `json:"bytes"`        // what it weighs now (all files of skills)
	// State is what the assistant reads of it (adopted, pending, kept, none), Reason why that is not the content as
	// it is now (too_large, invalid, not_regular, unreadable, rate_limited, disabled, switched_off), Detail the rest
	// of a file's problem.
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Hash is the digest of the content as it is now; AdoptedHash and AdoptedAt that of the instructions the
	// assistant reads (or read last, when switched off), and when they were adopted. NextAdoptAt: a pending version
	// is adopted by then at the latest. Times are RFC 3339 (FormatDataTime).
	Hash        string `json:"hash,omitempty"`
	AdoptedHash string `json:"adopted_hash,omitempty"`
	AdoptedAt   string `json:"adopted_at,omitempty"`
	NextAdoptAt string `json:"next_adopt_at,omitempty"`
	// Skills are the data skills sent (kind skills).
	Skills []string `json:"skills,omitempty"`
	// Files are the projects with a file (kind project_context, by slug); Truncated says some content is used cut at
	// its limit (Detail names which); NeverQuote is how many sentences drafts are checked against (inbox_context).
	Files      []string `json:"files,omitempty"`
	Truncated  bool     `json:"truncated,omitempty"`
	NeverQuote int      `json:"never_quote,omitempty"`
}

// ProjectContextParams are plugins.project_context's: the projects' slugs.
type ProjectContextParams struct {
	Projects []string `json:"projects"`
}

// ExtensionContextBlock is one extension's text for a prompt the manager builds: Name is the extension's name,
// Project the project's slug (project context only).
type ExtensionContextBlock struct {
	Extension string `json:"extension"`
	Name      string `json:"name"`
	Project   string `json:"project,omitempty"`
	Text      string `json:"text"`
}

// ExtensionContextResult is plugins.project_context's and plugins.inbox_context's answer; NeverQuote (Inbox only) are
// the sentences a draft must not repeat word for word, never put in a prompt.
type ExtensionContextResult struct {
	Blocks     []ExtensionContextBlock `json:"blocks"`
	NeverQuote []string                `json:"never_quote,omitempty"`
}

// FormatDataTime is a time as AssistantDataItem has it: RFC 3339 in UTC.
func FormatDataTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }
