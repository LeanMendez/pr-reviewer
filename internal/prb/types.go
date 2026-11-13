package prb

import (
	"encoding/json"
	"time"
)

// Identity is the output of `prb auth status`.
type Identity struct {
	Authenticated    bool   `json:"authenticated"`
	User             string `json:"user"`
	AccountID        string `json:"account_id"`
	DefaultWorkspace string `json:"default_workspace"`
}

// PRSummary is one entry of `prb pr list`.
type PRSummary struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Draft  bool   `json:"draft"`
}

// Repo is one entry of `prb repo list`.
type Repo struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

// PRContext is the `prb pr context` bundle (only the fields we use).
type PRContext struct {
	PullRequest    PR               `json:"pull_request"`
	Checks         *Checks          `json:"checks"`
	Mergeability   *json.RawMessage `json:"mergeability"`
	Files          *Files           `json:"files"`
	Threads        *Threads         `json:"threads"`
	Tasks          *Tasks           `json:"tasks"`
	TaskReferences []TaskRef        `json:"task_references"`
	Warnings       []Warning        `json:"warnings"`
}

// Warning is a section that prb could not fill in.
type Warning struct {
	Section string `json:"section"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TaskRef is a ClickUp task reference found in the PR.
type TaskRef struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Confidence string `json:"confidence"`
	StatusHint string `json:"status_hint"`
	URL        string `json:"url"`
}

// PR is the normalized pull request.
type PR struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	State       string    `json:"state"`
	URL         string    `json:"url"`
	Author      string    `json:"author"`
	Draft       bool      `json:"draft"`
	Source      Ref       `json:"source"`
	Destination Ref       `json:"destination"`
	Review      Review    `json:"review"`
	CreatedOn   time.Time `json:"created_on"`
	UpdatedOn   time.Time `json:"updated_on"`
}

// Ref is a PR endpoint.
type Ref struct {
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	Repository string `json:"repository"`
}

// Review summarizes the review status.
type Review struct {
	Approvals        int        `json:"approvals"`
	ChangesRequested int        `json:"changes_requested"`
	Reviewers        []Reviewer `json:"reviewers"`
}

// Reviewer is a reviewer or participant and their decision.
type Reviewer struct {
	Name      string `json:"name"`
	AccountID string `json:"account_id"`
	Role      string `json:"role"`
	State     string `json:"state"`
}

// Checks summarizes build statuses (Overall: pass|fail|pending|stopped|none).
type Checks struct {
	Overall string `json:"overall"`
}

// Tasks counts PR tasks.
type Tasks struct {
	Unresolved int `json:"unresolved"`
}

// Threads counts PR comment threads.
type Threads struct {
	Total      int `json:"total"`
	Unresolved int `json:"unresolved"`
}

// Files is the diffstat plus totals.
type Files struct {
	Count        int        `json:"count"`
	LinesAdded   int        `json:"lines_added"`
	LinesRemoved int        `json:"lines_removed"`
	Files        []FileStat `json:"files"`
}

// FileStat is one changed file.
type FileStat struct {
	Path         string `json:"path"`
	OldPath      string `json:"old_path"`
	Status       string `json:"status"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
}

// Diff is the `prb pr diff` output.
type Diff struct {
	TotalFiles   int        `json:"total_files"`
	Files        []DiffFile `json:"files"`
	OmittedFiles []string   `json:"omitted_files"`
	Truncated    bool       `json:"truncated"`
}

// DiffFile is one file of the diff with its patch.
type DiffFile struct {
	Path         string `json:"path"`
	OldPath      string `json:"old_path"`
	Status       string `json:"status"`
	Patch        string `json:"patch"`
	Binary       bool   `json:"binary"`
	Truncated    bool   `json:"truncated"`
	PatchOmitted bool   `json:"patch_omitted"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
	OmittedLines int    `json:"omitted_lines"`
}
