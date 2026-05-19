package resolver

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/navikt/actographer/internal/models"
)

type fakeActionYAMLFetcher struct {
	mu       sync.Mutex
	contents map[string][]byte
	files    map[string][]byte
	errs     map[string]error
	calls    map[string]int
}

func (f *fakeActionYAMLFetcher) GetActionYAML(_ context.Context, owner, repo, actionPath, ref string) ([]byte, error) {
	key := fmt.Sprintf("%s/%s/%s@%s", owner, repo, actionPath, ref)

	f.mu.Lock()
	defer f.mu.Unlock()

	if err, ok := f.errs[key]; ok {
		return nil, err
	}

	return f.contents[key], nil
}

func (f *fakeActionYAMLFetcher) GetRepoFileAtRef(_ context.Context, owner, repo, path, ref string) ([]byte, error) {
	key := fmt.Sprintf("%s/%s/%s@%s", owner, repo, path, ref)

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.calls != nil {
		f.calls[key]++
	}
	if err, ok := f.errs[key]; ok {
		return nil, err
	}

	return f.files[key], nil
}

func TestResolveCompositeActionTree(t *testing.T) {
	fetcher := &fakeActionYAMLFetcher{
		contents: map[string][]byte{
			"actions/setup-go/@v5": []byte("name: setup-go\nruns:\n  using: composite\n  steps:\n    - uses: actions/cache@v4\n    - uses: docker://alpine:3.18\n"),
			"actions/cache/@v4":    []byte("name: cache\nruns:\n  using: composite\n  steps:\n    - uses: actions/upload-artifact@v4\n"),
		},
		files: map[string][]byte{},
		errs:  map[string]error{},
	}

	r := NewResolver(fetcher, false)
	ref := models.ActionRef{
		Raw:   "actions/setup-go@v5",
		Owner: "actions",
		Repo:  "setup-go",
		Ref:   "v5",
		Type:  models.ActionRefAction,
	}

	dep := r.Resolve(context.Background(), ref)

	if dep.Action.Raw != ref.Raw {
		t.Fatalf("root action = %q, want %q", dep.Action.Raw, ref.Raw)
	}
	if len(dep.Deps) != 2 {
		t.Fatalf("expected 2 direct deps, got %d", len(dep.Deps))
	}
	if dep.Deps[0].Action.Raw != "actions/cache@v4" {
		t.Fatalf("first dep = %q, want actions/cache@v4", dep.Deps[0].Action.Raw)
	}
	if len(dep.Deps[0].Deps) != 1 {
		t.Fatalf("expected nested cache deps, got %d", len(dep.Deps[0].Deps))
	}
	if dep.Deps[0].Deps[0].Action.Raw != "actions/upload-artifact@v4" {
		t.Fatalf("nested dep = %q, want actions/upload-artifact@v4", dep.Deps[0].Deps[0].Action.Raw)
	}
	if dep.Deps[1].Action.Type != models.ActionRefDocker {
		t.Fatalf("second dep type = %q, want %q", dep.Deps[1].Action.Type, models.ActionRefDocker)
	}
}

func TestResolveMissingOrNonCompositeActionReturnsLeaf(t *testing.T) {
	tests := []struct {
		name     string
		contents map[string][]byte
		errs     map[string]error
	}{
		{
			name:     "missing action yaml",
			contents: map[string][]byte{},
			errs:     map[string]error{},
		},
		{
			name: "non composite action",
			contents: map[string][]byte{
				"actions/setup-node/@v4": []byte("name: setup-node\nruns:\n  using: node20\n  main: dist/index.js\n"),
			},
			errs: map[string]error{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewResolver(&fakeActionYAMLFetcher{
				contents: tc.contents,
				files:    map[string][]byte{},
				errs:     tc.errs,
			}, false)
			ref := models.ActionRef{
				Raw:   "actions/setup-node@v4",
				Owner: "actions",
				Repo:  "setup-node",
				Ref:   "v4",
				Type:  models.ActionRefAction,
			}

			dep := r.Resolve(context.Background(), ref)

			if dep.Action.Raw != ref.Raw {
				t.Fatalf("root action = %q, want %q", dep.Action.Raw, ref.Raw)
			}
			if len(dep.Deps) != 0 {
				t.Fatalf("expected no deps, got %d", len(dep.Deps))
			}
		})
	}
}

func TestResolveIsSafeForConcurrentCacheAccess(t *testing.T) {
	fetcher := &fakeActionYAMLFetcher{
		contents: map[string][]byte{
			"actions/setup-go/@v5": []byte("name: setup-go\nruns:\n  using: composite\n  steps:\n    - uses: actions/cache@v4\n"),
		},
		files: map[string][]byte{},
		errs:  map[string]error{},
	}

	r := NewResolver(fetcher, false)
	ref := models.ActionRef{
		Raw:   "actions/setup-go@v5",
		Owner: "actions",
		Repo:  "setup-go",
		Ref:   "v5",
		Type:  models.ActionRefAction,
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dep := r.Resolve(context.Background(), ref)
			if len(dep.Deps) != 1 {
				t.Errorf("expected 1 dep, got %d", len(dep.Deps))
			}
		}()
	}
	wg.Wait()
}

func TestResolveJavaScriptActionSBOM(t *testing.T) {
	fetcher := &fakeActionYAMLFetcher{
		contents: map[string][]byte{
			"navikt/js-action/@v1": []byte("name: js-action\nruns:\n  using: node20\n  main: dist/index.js\n"),
		},
		files: map[string][]byte{
			"navikt/js-action/package-lock.json@v1": []byte(`{
  "name": "js-action",
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "js-action", "version": "1.0.0" },
    "node_modules/@actions/core": { "version": "1.10.1", "integrity": "sha512-core" },
    "node_modules/uuid": { "version": "9.0.1", "integrity": "sha512-uuid" }
  }
}`),
		},
		errs:  map[string]error{},
		calls: map[string]int{},
	}

	r := NewResolver(fetcher, true)
	ref := models.ActionRef{
		Raw:   "navikt/js-action@v1",
		Owner: "navikt",
		Repo:  "js-action",
		Ref:   "v1",
		Type:  models.ActionRefAction,
	}

	dep := r.Resolve(context.Background(), ref)

	if dep.SBOM == nil {
		t.Fatal("expected SBOM on JS action")
	}
	if dep.SBOM.Status != "resolved" {
		t.Fatalf("SBOM status = %q, want resolved", dep.SBOM.Status)
	}
	if dep.SBOM.SourcePath != "package-lock.json" {
		t.Fatalf("SBOM source path = %q, want package-lock.json", dep.SBOM.SourcePath)
	}
	if len(dep.SBOM.Packages) != 2 {
		t.Fatalf("SBOM package count = %d, want 2", len(dep.SBOM.Packages))
	}
}

func TestResolveJavaScriptActionSBOMFallsBackToRepoRootAndCachesNegative(t *testing.T) {
	fetcher := &fakeActionYAMLFetcher{
		contents: map[string][]byte{
			"actions/cache/save@v4": []byte("name: save\nruns:\n  using: node20\n  main: dist/save/index.js\n"),
			"navikt/no-lock/@v1":    []byte("name: no-lock\nruns:\n  using: node20\n  main: dist/index.js\n"),
		},
		files: map[string][]byte{
			"actions/cache/package-lock.json@v4": []byte(`{
  "name": "cache",
  "lockfileVersion": 3,
  "packages": {
    "node_modules/@actions/core": { "version": "1.10.1" }
  }
}`),
		},
		errs:  map[string]error{},
		calls: map[string]int{},
	}

	r := NewResolver(fetcher, true)

	rootFallback := r.Resolve(context.Background(), models.ActionRef{
		Raw:   "actions/cache/save@v4",
		Owner: "actions",
		Repo:  "cache",
		Path:  "save",
		Ref:   "v4",
		Type:  models.ActionRefAction,
	})
	if rootFallback.SBOM == nil || rootFallback.SBOM.SourcePath != "package-lock.json" {
		t.Fatalf("expected repo root package-lock fallback, got %#v", rootFallback.SBOM)
	}

	noLockRef := models.ActionRef{
		Raw:   "navikt/no-lock@v1",
		Owner: "navikt",
		Repo:  "no-lock",
		Ref:   "v1",
		Type:  models.ActionRefAction,
	}
	first := r.Resolve(context.Background(), noLockRef)
	second := r.Resolve(context.Background(), noLockRef)

	if first.SBOM == nil || first.SBOM.Status != "no_lockfile" {
		t.Fatalf("expected no_lockfile status, got %#v", first.SBOM)
	}
	if second.SBOM == nil || second.SBOM.Status != "no_lockfile" {
		t.Fatalf("expected cached no_lockfile status, got %#v", second.SBOM)
	}
	if fetcher.calls["navikt/no-lock/package-lock.json@v1"] != 1 {
		t.Fatalf("package-lock fetch count = %d, want 1", fetcher.calls["navikt/no-lock/package-lock.json@v1"])
	}
	if fetcher.calls["navikt/no-lock/npm-shrinkwrap.json@v1"] != 1 {
		t.Fatalf("npm-shrinkwrap fetch count = %d, want 1", fetcher.calls["navikt/no-lock/npm-shrinkwrap.json@v1"])
	}
	if fetcher.calls["navikt/no-lock/pnpm-lock.yaml@v1"] != 1 {
		t.Fatalf("pnpm-lock fetch count = %d, want 1", fetcher.calls["navikt/no-lock/pnpm-lock.yaml@v1"])
	}
	if fetcher.calls["navikt/no-lock/yarn.lock@v1"] != 1 {
		t.Fatalf("yarn lock fetch count = %d, want 1", fetcher.calls["navikt/no-lock/yarn.lock@v1"])
	}
}

func TestResolveJavaScriptActionSBOMSupportsYarnAndPNPM(t *testing.T) {
	tests := []struct {
		name       string
		ref        models.ActionRef
		lockfile   string
		contentKey string
		ecosystem  string
	}{
		{
			name: "yarn classic",
			ref: models.ActionRef{
				Raw:   "navikt/yarn-action@v1",
				Owner: "navikt",
				Repo:  "yarn-action",
				Ref:   "v1",
				Type:  models.ActionRefAction,
			},
			lockfile:   "yarn.lock",
			contentKey: "navikt/yarn-action/yarn.lock@v1",
			ecosystem:  "yarn",
		},
		{
			name: "pnpm lockfile",
			ref: models.ActionRef{
				Raw:   "navikt/pnpm-action@v1",
				Owner: "navikt",
				Repo:  "pnpm-action",
				Ref:   "v1",
				Type:  models.ActionRefAction,
			},
			lockfile:   "pnpm-lock.yaml",
			contentKey: "navikt/pnpm-action/pnpm-lock.yaml@v1",
			ecosystem:  "pnpm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := &fakeActionYAMLFetcher{
				contents: map[string][]byte{
					fmt.Sprintf("%s/%s/@%s", tc.ref.Owner, tc.ref.Repo, tc.ref.Ref): []byte("name: js-action\nruns:\n  using: node20\n  main: dist/index.js\n"),
				},
				files: map[string][]byte{
					"navikt/yarn-action/yarn.lock@v1": []byte(`# yarn lockfile v1
"@actions/core@^1.10.1":
  version "1.10.1"
"uuid@^9.0.0":
  version "9.0.1"
`),
					"navikt/pnpm-action/pnpm-lock.yaml@v1": []byte(`lockfileVersion: '9.0'
packages:
  '@actions/core@1.10.1':
    resolution:
      integrity: sha512-core
  'uuid@9.0.1':
    resolution:
      integrity: sha512-uuid
`),
				},
				errs:  map[string]error{},
				calls: map[string]int{},
			}

			r := NewResolver(fetcher, true)
			dep := r.Resolve(context.Background(), tc.ref)

			if dep.SBOM == nil {
				t.Fatal("expected SBOM on JS action")
			}
			if dep.SBOM.Status != "resolved" {
				t.Fatalf("SBOM status = %q, want resolved", dep.SBOM.Status)
			}
			if dep.SBOM.SourcePath != tc.lockfile {
				t.Fatalf("SBOM source path = %q, want %q", dep.SBOM.SourcePath, tc.lockfile)
			}
			if dep.SBOM.Ecosystem != tc.ecosystem {
				t.Fatalf("SBOM ecosystem = %q, want %q", dep.SBOM.Ecosystem, tc.ecosystem)
			}
			if len(dep.SBOM.Packages) != 2 {
				t.Fatalf("SBOM package count = %d, want 2", len(dep.SBOM.Packages))
			}
		})
	}
}
