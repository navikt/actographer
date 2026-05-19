package models

type ActionRefType string

const (
	ActionRefAction   ActionRefType = "action"
	ActionRefWorkflow ActionRefType = "reusable_workflow"
	ActionRefDocker   ActionRefType = "docker"
	ActionRefLocal    ActionRefType = "local"
)

type ActionRef struct {
	Raw     string        `json:"raw"`
	Owner   string        `json:"owner,omitempty"`
	Repo    string        `json:"repo,omitempty"`
	Path    string        `json:"path,omitempty"`
	Ref     string        `json:"ref,omitempty"`
	Type    ActionRefType `json:"type"`
	Pinned  bool          `json:"pinned"`
	Comment string        `json:"comment,omitempty"`
}

type WorkflowJob struct {
	Name         string          `json:"name"`
	Uses         *ActionRef      `json:"uses,omitempty"`
	StepUses     []ActionRef     `json:"step_uses,omitempty"`
	Dependencies []TransitiveDep `json:"dependencies,omitempty"`
}

type Workflow struct {
	RepoFullName string        `json:"repo"`
	Path         string        `json:"path"`
	Jobs         []WorkflowJob `json:"jobs"`
}

type TransitiveDep struct {
	Action ActionRef       `json:"action"`
	Depth  int             `json:"depth"`
	SBOM   *ActionSBOM     `json:"sbom,omitempty"`
	Deps   []TransitiveDep `json:"deps,omitempty"`
}

type ActionSBOM struct {
	Ecosystem  string        `json:"ecosystem"`
	SourcePath string        `json:"source_path,omitempty"`
	Status     string        `json:"status,omitempty"`
	Packages   []SBOMPackage `json:"packages,omitempty"`
}

type SBOMPackage struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Integrity string `json:"integrity,omitempty"`
	Resolved  string `json:"resolved,omitempty"`
	Dev       bool   `json:"dev,omitempty"`
	Optional  bool   `json:"optional,omitempty"`
}

type RepoInventory struct {
	RepoFullName string     `json:"repo"`
	Archived     bool       `json:"archived"`
	Workflows    []Workflow `json:"workflows"`
}

type ActionVersion struct {
	Tag  string          `json:"tag"`
	Deps []TransitiveDep `json:"deps,omitempty"`
}

type PublishedAction struct {
	RepoFullName string          `json:"repo"`
	Archived     bool            `json:"archived"`
	Versions     []ActionVersion `json:"versions"`
}

type PublisherSummary struct {
	ReposScanned      int `json:"repos_scanned"`
	ActionsFound      int `json:"actions_found"`
	TotalVersions     int `json:"total_versions"`
	TotalDepsResolved int `json:"total_deps_resolved"`
}

type OrgInventory struct {
	Org              string            `json:"org,omitempty"`
	SnapshotDate     string            `json:"snapshot_date"`
	Summary          *Summary          `json:"summary,omitempty"`
	PublisherSummary *PublisherSummary `json:"publisher_summary,omitempty"`
	Repos            []RepoInventory   `json:"repos,omitempty"`
	PublishedActions []PublishedAction `json:"published_actions,omitempty"`
}

type Summary struct {
	ReposScanned                 int           `json:"repos_scanned"`
	ReposWithWorkflows           int           `json:"repos_with_workflows"`
	TotalWorkflows               int           `json:"total_workflows"`
	TotalDirectDeps              int           `json:"total_direct_deps"`
	UniqueDirectDeps             int           `json:"unique_direct_deps"`
	PinnedDeps                   int           `json:"pinned_deps"`
	UnpinnedDeps                 int           `json:"unpinned_deps"`
	TotalTransitiveGitHubActions int           `json:"total_transitive_github_actions"`
	TotalKnownTransitiveDeps     *int          `json:"total_known_transitive_deps,omitempty"`
	UniqueTransitiveDeps         int           `json:"unique_transitive_deps"`
	MaxDepth                     int           `json:"max_depth"`
	TopActions                   []ActionCount `json:"top_actions"`
	TopTransitiveActions         []ActionCount `json:"top_transitive_actions,omitempty"`
}

type ActionCount struct {
	Action string `json:"action"`
	Count  int    `json:"count"`
}
