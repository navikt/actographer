package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/navikt/actographer/internal/config"
	"github.com/navikt/actographer/internal/fetcher"
	"github.com/navikt/actographer/internal/models"
	"github.com/navikt/actographer/internal/resolver"
)

type fakeFetcher struct {
	repos         []fetcher.RepoMeta
	repoByName    map[string]fetcher.RepoMeta
	workflowFiles map[string][]string
	fileContents  map[string][]byte
	actionYAML    map[string][]byte
	refFiles      map[string][]byte
	actionTags    map[string][]string
	strictTagCtx  bool
}

func (f *fakeFetcher) GetRepo(_ context.Context, repoFullName string) (fetcher.RepoMeta, error) {
	if repo, ok := f.repoByName[repoFullName]; ok {
		return repo, nil
	}
	return fetcher.RepoMeta{}, fmt.Errorf("repo %s not found", repoFullName)
}

func (f *fakeFetcher) ListRepos(context.Context) ([]fetcher.RepoMeta, error) {
	return f.repos, nil
}

func (f *fakeFetcher) ListWorkflowFiles(_ context.Context, repoFullName string) ([]string, error) {
	return f.workflowFiles[repoFullName], nil
}

func (f *fakeFetcher) GetFileContent(_ context.Context, repoFullName, path string) ([]byte, error) {
	return f.fileContents[repoFullName+":"+path], nil
}

func (f *fakeFetcher) ListActionTags(ctx context.Context, owner, repo string) ([]string, error) {
	if f.strictTagCtx && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return f.actionTags[owner+"/"+repo], nil
}

func (f *fakeFetcher) GetActionYAML(_ context.Context, owner, repo, actionPath, ref string) ([]byte, error) {
	return f.actionYAML[actionKey(owner, repo, actionPath, ref)], nil
}

func (f *fakeFetcher) GetRepoFileAtRef(_ context.Context, owner, repo, path, ref string) ([]byte, error) {
	return f.refFiles[actionKey(owner, repo, path, ref)], nil
}

func actionKey(owner, repo, actionPath, ref string) string {
	return fmt.Sprintf("%s/%s/%s@%s", owner, repo, actionPath, ref)
}

func readSingleInventoryFile(t *testing.T, dir string) models.OrgInventory {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "inventory-*.json"))
	if err != nil {
		t.Fatalf("glob inventory files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 inventory file, got %d", len(files))
	}

	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read inventory file: %v", err)
	}

	var inventory models.OrgInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatalf("unmarshal inventory: %v", err)
	}

	return inventory
}

func readSingleTreeFile(t *testing.T, dir string) string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "inventory-*.tree.txt"))
	if err != nil {
		t.Fatalf("glob tree files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 tree file, got %d", len(files))
	}

	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read tree file: %v", err)
	}

	return string(data)
}

func findJob(t *testing.T, workflow models.Workflow, name string) models.WorkflowJob {
	t.Helper()

	for _, job := range workflow.Jobs {
		if job.Name == name {
			return job
		}
	}

	t.Fatalf("job %q not found", name)
	return models.WorkflowJob{}
}

func TestRunConsumerModeWritesInventory(t *testing.T) {
	outputDir := t.TempDir()
	fetcher := &fakeFetcher{
		repos: []fetcher.RepoMeta{
			{FullName: "navikt/app", Archived: false},
		},
		workflowFiles: map[string][]string{
			"navikt/app": {".github/workflows/ci.yml"},
		},
		fileContents: map[string][]byte{
			"navikt/app:.github/workflows/ci.yml": []byte("name: CI\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/setup-go@v5\n      - uses: navikt/js-action@v1\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-go@v5\n  deploy:\n    uses: navikt/ci/.github/workflows/deploy.yml@main\n"),
		},
		actionYAML: map[string][]byte{
			actionKey("actions", "setup-go", "", "v5"): []byte("name: setup-go\nruns:\n  using: composite\n  steps:\n    - uses: actions/cache@v4\n"),
			actionKey("navikt", "js-action", "", "v1"): []byte("name: js-action\nruns:\n  using: node20\n  main: dist/index.js\n"),
		},
		refFiles: map[string][]byte{
			actionKey("navikt", "js-action", "package-lock.json", "v1"): []byte(`{
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "js-action", "version": "1.0.0" },
    "node_modules/@actions/core": { "version": "1.10.1" },
    "node_modules/uuid": { "version": "9.0.1" }
  }
}`),
		},
		actionTags: map[string][]string{
			"actions/checkout": {"v4"},
			"actions/setup-go": {"v5"},
			"navikt/js-action": {"v1"},
		},
		strictTagCtx: true,
	}

	cfg := config.Config{
		Org:         "navikt",
		Token:       "test-token",
		Parallelism: 1,
		OutputPath:  outputDir,
		FetchSBOM:   true,
	}

	app := NewApp(cfg, fetcher, resolver.NewResolver(fetcher, true))
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	inventory := readSingleInventoryFile(t, outputDir)
	treeOutput := readSingleTreeFile(t, outputDir)

	if inventory.Summary == nil {
		t.Fatal("summary should be present")
	}
	if inventory.Summary.TotalDirectDeps != 5 {
		t.Fatalf("TotalDirectDeps = %d, want 5", inventory.Summary.TotalDirectDeps)
	}
	if inventory.Summary.TotalTransitiveGitHubActions != 1 {
		t.Fatalf("TotalTransitiveGitHubActions = %d, want 1", inventory.Summary.TotalTransitiveGitHubActions)
	}
	if inventory.Summary.TotalKnownTransitiveDeps == nil || *inventory.Summary.TotalKnownTransitiveDeps != 3 {
		t.Fatalf("TotalKnownTransitiveDeps = %#v, want 3", inventory.Summary.TotalKnownTransitiveDeps)
	}
	if len(inventory.Repos) != 1 {
		t.Fatalf("expected 1 repo inventory, got %d", len(inventory.Repos))
	}
	if len(inventory.Repos[0].Workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(inventory.Repos[0].Workflows))
	}

	buildJob := findJob(t, inventory.Repos[0].Workflows[0], "build")
	if len(buildJob.Dependencies) != 3 {
		t.Fatalf("build dependencies = %d, want 3", len(buildJob.Dependencies))
	}
	if buildJob.Dependencies[1].Action.Raw != "actions/setup-go@v5" {
		t.Fatalf("build root = %q, want actions/setup-go@v5", buildJob.Dependencies[1].Action.Raw)
	}
	if len(buildJob.Dependencies[1].Deps) != 1 {
		t.Fatalf("build transitive deps = %d, want 1", len(buildJob.Dependencies[1].Deps))
	}
	if buildJob.Dependencies[1].Deps[0].Action.Raw != "actions/cache@v4" {
		t.Fatalf("nested dep = %q, want actions/cache@v4", buildJob.Dependencies[1].Deps[0].Action.Raw)
	}

	testJob := findJob(t, inventory.Repos[0].Workflows[0], "test")
	if len(testJob.Dependencies) != 1 {
		t.Fatalf("test dependencies = %d, want 1", len(testJob.Dependencies))
	}
	if testJob.Dependencies[0].Action.Raw != "actions/setup-go@v5" {
		t.Fatalf("test root = %q, want actions/setup-go@v5", testJob.Dependencies[0].Action.Raw)
	}
	if len(testJob.Dependencies[0].Deps) != 1 {
		t.Fatalf("test transitive deps = %d, want 1", len(testJob.Dependencies[0].Deps))
	}

	jsAction := buildJob.Dependencies[2]
	if jsAction.Action.Raw != "navikt/js-action@v1" {
		t.Fatalf("js action root = %q, want navikt/js-action@v1", jsAction.Action.Raw)
	}
	if jsAction.SBOM == nil || jsAction.SBOM.Status != "resolved" {
		t.Fatalf("expected resolved SBOM, got %#v", jsAction.SBOM)
	}
	if len(jsAction.SBOM.Packages) != 2 {
		t.Fatalf("js action SBOM packages = %d, want 2", len(jsAction.SBOM.Packages))
	}
	if !strings.Contains(treeOutput, ".\n") {
		t.Fatalf("tree output missing root:\n%s", treeOutput)
	}
	if !strings.Contains(treeOutput, "└── [Repo] navikt/app") && !strings.Contains(treeOutput, "├── [Repo] navikt/app") {
		t.Fatalf("tree output missing repo:\n%s", treeOutput)
	}
	if !strings.Contains(treeOutput, "[Workflow] .github/workflows/ci.yml") {
		t.Fatalf("tree output missing workflow:\n%s", treeOutput)
	}
	if !strings.Contains(treeOutput, "[Job] build") {
		t.Fatalf("tree output missing build job:\n%s", treeOutput)
	}
	if !strings.Contains(treeOutput, "[Dependency] actions/setup-go@v5") {
		t.Fatalf("tree output missing setup-go branch:\n%s", treeOutput)
	}
	if !strings.Contains(treeOutput, "[Dependency] actions/cache@v4") {
		t.Fatalf("tree output missing nested dependency:\n%s", treeOutput)
	}
	if !strings.Contains(treeOutput, "[SBOM] npm [package-lock.json]") {
		t.Fatalf("tree output missing SBOM node:\n%s", treeOutput)
	}
	if !strings.Contains(treeOutput, "[Package] @actions/core@1.10.1") {
		t.Fatalf("tree output missing package node:\n%s", treeOutput)
	}
	if !strings.Contains(treeOutput, "[Total known transitive deps] 3") {
		t.Fatalf("tree output missing known transitive summary count:\n%s", treeOutput)
	}
	if !strings.Contains(treeOutput, "[Total transitive GitHub Actions] 1") {
		t.Fatalf("tree output missing transitive GitHub Actions summary count:\n%s", treeOutput)
	}
}

func TestRunConsumerModeOmitsKnownTransitiveDepsWithoutSBOM(t *testing.T) {
	outputDir := t.TempDir()
	fetcher := &fakeFetcher{
		repos: []fetcher.RepoMeta{
			{FullName: "navikt/app", Archived: false},
		},
		workflowFiles: map[string][]string{
			"navikt/app": {".github/workflows/ci.yml"},
		},
		fileContents: map[string][]byte{
			"navikt/app:.github/workflows/ci.yml": []byte("name: CI\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/setup-go@v5\n"),
		},
		actionYAML: map[string][]byte{
			actionKey("actions", "setup-go", "", "v5"): []byte("name: setup-go\nruns:\n  using: composite\n  steps:\n    - uses: actions/cache@v4\n"),
		},
	}

	cfg := config.Config{
		Org:         "navikt",
		Token:       "test-token",
		Parallelism: 1,
		OutputPath:  outputDir,
		FetchSBOM:   false,
	}

	app := NewApp(cfg, fetcher, resolver.NewResolver(fetcher, false))
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	inventory := readSingleInventoryFile(t, outputDir)
	treeOutput := readSingleTreeFile(t, outputDir)

	if inventory.Summary == nil {
		t.Fatal("summary should be present")
	}
	if inventory.Summary.TotalKnownTransitiveDeps != nil {
		t.Fatalf("expected TotalKnownTransitiveDeps to be omitted, got %#v", inventory.Summary.TotalKnownTransitiveDeps)
	}
	if strings.Contains(treeOutput, "[Total known transitive deps]") {
		t.Fatalf("tree output should omit known transitive deps when SBOM is disabled:\n%s", treeOutput)
	}
}

func TestRunPublisherModeWritesInventory(t *testing.T) {
	outputDir := t.TempDir()
	fetcher := &fakeFetcher{
		repos: []fetcher.RepoMeta{
			{FullName: "navikt/published-action", Archived: false},
			{FullName: "navikt/not-an-action", Archived: false},
		},
		actionYAML: map[string][]byte{
			actionKey("navikt", "published-action", "", "HEAD"):   []byte("name: published\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v4\n"),
			actionKey("navikt", "published-action", "", "v1.0.0"): []byte("name: published\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v4\n"),
		},
		actionTags: map[string][]string{
			"navikt/published-action": {"v1.0.0"},
		},
	}

	cfg := config.Config{
		Org:         "navikt",
		Token:       "test-token",
		Parallelism: 1,
		OutputPath:  outputDir,
		Mode:        "publisher",
	}

	app := NewApp(cfg, fetcher, resolver.NewResolver(fetcher, false))
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	inventory := readSingleInventoryFile(t, outputDir)
	treeOutput := readSingleTreeFile(t, outputDir)

	if inventory.PublisherSummary == nil {
		t.Fatal("publisher summary should be present")
	}
	if inventory.PublisherSummary.ActionsFound != 1 {
		t.Fatalf("ActionsFound = %d, want 1", inventory.PublisherSummary.ActionsFound)
	}
	if inventory.PublisherSummary.TotalVersions != 1 {
		t.Fatalf("TotalVersions = %d, want 1", inventory.PublisherSummary.TotalVersions)
	}
	if inventory.PublisherSummary.TotalDepsResolved != 1 {
		t.Fatalf("TotalDepsResolved = %d, want 1", inventory.PublisherSummary.TotalDepsResolved)
	}
	if len(inventory.PublishedActions) != 1 {
		t.Fatalf("expected 1 published action, got %d", len(inventory.PublishedActions))
	}
	if len(inventory.PublishedActions[0].Versions) != 1 {
		t.Fatalf("expected 1 action version, got %d", len(inventory.PublishedActions[0].Versions))
	}
	if inventory.PublishedActions[0].Versions[0].Deps[0].Action.Raw != "actions/checkout@v4" {
		t.Fatalf("version dep = %q, want actions/checkout@v4", inventory.PublishedActions[0].Versions[0].Deps[0].Action.Raw)
	}
	if !strings.Contains(treeOutput, "└── [Published action] navikt/published-action") && !strings.Contains(treeOutput, "├── [Published action] navikt/published-action") {
		t.Fatalf("tree output missing published action:\n%s", treeOutput)
	}
	if !strings.Contains(treeOutput, "[Version] v1.0.0") {
		t.Fatalf("tree output missing version:\n%s", treeOutput)
	}
}
