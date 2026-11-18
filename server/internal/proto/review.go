package proto

// Reviewing a session's changes (GET /api/v1/machines/{mid}/git/review → node git.review, docs/web/review.md, #151): every
// file that changed in a scope, each with its hunks, for the web's Review panel; read only.

// Review scopes: what the working tree is compared with.
const (
	ReviewUncommitted = "uncommitted" // HEAD: the staged and unstaged changes, and the new files
	ReviewBranch      = "branch"      // where the branch left the default branch (or Base): its commits and the above
	ReviewCommit      = "commit"      // a commit (Base): everything since it
)

// The kinds of failure git.review answers with as rpc.Error codes (the hub passes them on with 400 / 404 / 409).
const (
	CodeNotGit          = "not_git"           // cwd is not in a Git repository
	CodeNoFolder        = "no_folder"         // cwd is not there: moved or deleted (a removed worktree)
	CodeBadScope        = "bad_scope"         // a scope other than the three, or commit without a base
	CodeBadBase         = "bad_base"          // the base is not a commit (or branch) of the repository
	CodeNoDefaultBranch = "no_default_branch" // branch: no origin/HEAD, origin/main, origin/master, main or master
	CodeNoMergeBase     = "no_merge_base"     // branch: HEAD and the default branch have no commit in common
)

type GitReviewRequest struct {
	Cwd   string `json:"cwd"`
	Scope string `json:"scope"`
	// Base: for commit the commit; for branch the branch to compare with instead of the default one.
	Base string `json:"base,omitempty"`
	// Paths: only these files (relative to the root): a file whose diff was too big for the page, asked for on its
	// own; a renamed one with both of its paths.
	Paths []string `json:"paths,omitempty"`
}

type GitReview struct {
	Root  string `json:"root"`
	Scope string `json:"scope"`
	// Base is what the working tree was compared with; its hash pins it for a file asked for later (scope commit).
	Base GitReviewBase  `json:"base"`
	Head *GitReviewHead `json:"head,omitempty"` // absent before the first commit
	// Files in path order; the ones file.read refuses are listed without their diff.
	Files []GitReviewFile `json:"files"`
	// Truncated: more files changed than listed (the node's maxReviewFiles, node/gitreview.go).
	Truncated bool `json:"truncated,omitempty"`
}

type GitReviewBase struct {
	Hash    string `json:"hash,omitempty"`
	Short   string `json:"short,omitempty"`
	Subject string `json:"subject,omitempty"`
	// Ref: the branch the branch scope compared with (origin/main…); empty otherwise.
	Ref string `json:"ref,omitempty"`
	// Empty: the repository has no commit yet; everything is new.
	Empty bool `json:"empty,omitempty"`
}

type GitReviewHead struct {
	Hash   string `json:"hash"`
	Short  string `json:"short"`
	Branch string `json:"branch,omitempty"` // empty when detached
}

type GitReviewFile struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"` // renamed or copied from
	// Status: added, modified, deleted, renamed, copied, type_changed.
	Status    string `json:"status"`
	Untracked bool   `json:"untracked,omitempty"` // a new file Git does not know yet
	Added     int    `json:"added"`
	Deleted   int    `json:"deleted"`
	Binary    bool   `json:"binary,omitempty"`
	Generated bool   `json:"generated,omitempty"` // .gitattributes says linguist-generated
	// Refused: a file file.read refuses (a key, a credentials folder…): listed, its content is not sent.
	Refused bool `json:"refused,omitempty"`
	// Big: its diff is bigger than a page carries; asked for on its own (Paths) it comes unless it is huge.
	Big bool `json:"big,omitempty"`
	// Revision: the new side's blob id (short), which changes when the file does (viewed marks).
	Revision string    `json:"revision,omitempty"`
	Hunks    []GitHunk `json:"hunks,omitempty"`
}

// GitHunk is one hunk of a unified diff: its lines with their prefix (" ", "+", "-", "\"). A side with no lines starts
// after the line the header names (old_start = a+1 for "-a,0"), so the numbers count from where the hunk is.
type GitHunk struct {
	Header   string   `json:"header,omitempty"` // the text after the second @@ (the function it is in)
	OldStart int      `json:"old_start"`
	NewStart int      `json:"new_start"`
	Lines    []string `json:"lines"`
}
