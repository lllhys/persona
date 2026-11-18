package devicewire

// KnowledgeSearch and KnowledgeRead are manager-to-node calls. The manager sets
// the folder and exclusions from its configuration, never from tool arguments.
const (
	KnowledgeSearch = "knowledge.search"
	KnowledgeRead   = "knowledge.read"
)

type KnowledgeFolder struct {
	Path              string   `json:"path"`
	ExcludeDirs       []string `json:"exclude_dirs,omitempty"`
	ExcludeExtensions []string `json:"exclude_extensions,omitempty"`
}

type KnowledgeSearchRequest struct {
	KnowledgeFolder
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type KnowledgeSearchResult struct {
	Paths     []string `json:"paths"`
	Truncated bool     `json:"truncated,omitempty"`
}

type KnowledgeReadRequest struct {
	KnowledgeFolder
	Path string `json:"relative_path"`
}

type KnowledgeReadResult struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}
