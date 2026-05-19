package parser

import (
	"testing"

	"github.com/navikt/actographer/internal/models"
)

func TestParseActionRef(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantType models.ActionRefType
		owner    string
		repo     string
		path     string
		ref      string
		pinned   bool
		comment  string
	}{
		{
			name:     "simple action with tag",
			raw:      "actions/checkout@v4",
			wantType: models.ActionRefAction,
			owner:    "actions",
			repo:     "checkout",
			ref:      "v4",
		},
		{
			name:     "action with SHA pin",
			raw:      "actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11",
			wantType: models.ActionRefAction,
			owner:    "actions",
			repo:     "checkout",
			ref:      "b4ffde65f46336ab88eb53be808477a3936bae11",
			pinned:   true,
		},
		{
			name:     "SHA pin with ratchet comment",
			raw:      "actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4",
			wantType: models.ActionRefAction,
			owner:    "actions",
			repo:     "checkout",
			ref:      "b4ffde65f46336ab88eb53be808477a3936bae11",
			pinned:   true,
			comment:  "# v4",
		},
		{
			name:     "action with subpath",
			raw:      "aws-actions/configure-aws-credentials/subdir@v4",
			wantType: models.ActionRefAction,
			owner:    "aws-actions",
			repo:     "configure-aws-credentials",
			path:     "subdir",
			ref:      "v4",
		},
		{
			name:     "reusable workflow",
			raw:      "navikt/ci/.github/workflows/build.yml@main",
			wantType: models.ActionRefWorkflow,
			owner:    "navikt",
			repo:     "ci",
			path:     ".github/workflows/build.yml",
			ref:      "main",
		},
		{
			name:     "docker action",
			raw:      "docker://alpine:3.18",
			wantType: models.ActionRefDocker,
			path:     "alpine:3.18",
		},
		{
			name:     "local action",
			raw:      "./actions/my-action",
			wantType: models.ActionRefLocal,
			path:     "./actions/my-action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseActionRef(tt.raw)

			if got.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.Owner != tt.owner {
				t.Errorf("Owner = %q, want %q", got.Owner, tt.owner)
			}
			if got.Repo != tt.repo {
				t.Errorf("Repo = %q, want %q", got.Repo, tt.repo)
			}
			if got.Path != tt.path {
				t.Errorf("Path = %q, want %q", got.Path, tt.path)
			}
			if got.Ref != tt.ref {
				t.Errorf("Ref = %q, want %q", got.Ref, tt.ref)
			}
			if got.Pinned != tt.pinned {
				t.Errorf("Pinned = %v, want %v", got.Pinned, tt.pinned)
			}
			if got.Comment != tt.comment {
				t.Errorf("Comment = %q, want %q", got.Comment, tt.comment)
			}
		})
	}
}

func TestParseWorkflow(t *testing.T) {
	input := "name: CI\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/setup-go@v5\n      - run: go build ./...\n  deploy:\n    uses: navikt/ci/.github/workflows/deploy.yml@main\n"

	wf, err := ParseWorkflow([]byte(input), "navikt/my-repo", ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("ParseWorkflow failed: %v", err)
	}

	if wf.RepoFullName != "navikt/my-repo" {
		t.Errorf("RepoFullName = %q", wf.RepoFullName)
	}

	if len(wf.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(wf.Jobs))
	}

	var buildJob, deployJob *models.WorkflowJob
	for i := range wf.Jobs {
		switch wf.Jobs[i].Name {
		case "build":
			buildJob = &wf.Jobs[i]
		case "deploy":
			deployJob = &wf.Jobs[i]
		}
	}

	if buildJob == nil {
		t.Fatal("build job not found")
	}
	if len(buildJob.StepUses) != 2 {
		t.Errorf("build job: expected 2 step uses, got %d", len(buildJob.StepUses))
	}
	if buildJob.Uses != nil {
		t.Errorf("build job should not have job-level uses")
	}

	if deployJob == nil {
		t.Fatal("deploy job not found")
	}
	if deployJob.Uses == nil {
		t.Fatal("deploy job should have job-level uses")
	}
	if deployJob.Uses.Type != models.ActionRefWorkflow {
		t.Errorf("deploy uses type = %q, want reusable_workflow", deployJob.Uses.Type)
	}
}

func TestParseCompositeAction(t *testing.T) {
	input := "name: My Composite\ndescription: A composite action\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v4\n    - run: echo hello\n    - uses: docker://node:18\n"

	refs, err := ParseCompositeAction([]byte(input))
	if err != nil {
		t.Fatalf("ParseCompositeAction failed: %v", err)
	}

	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}

	if refs[0].Type != models.ActionRefAction {
		t.Errorf("ref[0] type = %q, want action", refs[0].Type)
	}
	if refs[1].Type != models.ActionRefDocker {
		t.Errorf("ref[1] type = %q, want docker", refs[1].Type)
	}
}

func TestParseCompositeAction_NotComposite(t *testing.T) {
	input := "name: JS Action\nruns:\n  using: node20\n  main: index.js\n"

	refs, err := ParseCompositeAction([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refs != nil {
		t.Errorf("expected nil refs for non-composite, got %d", len(refs))
	}
}

func TestParseActionDefinition(t *testing.T) {
	input := "name: JS Action\nruns:\n  using: node20\n  main: dist/index.js\n"

	def, err := ParseActionDefinition([]byte(input))
	if err != nil {
		t.Fatalf("ParseActionDefinition failed: %v", err)
	}

	if def.RunsUsing != "node20" {
		t.Fatalf("RunsUsing = %q, want node20", def.RunsUsing)
	}
	if !def.IsJavaScriptAction() {
		t.Fatal("expected JS action")
	}
	if len(def.InnerRefs) != 0 {
		t.Fatalf("expected no composite refs, got %d", len(def.InnerRefs))
	}
}

func TestParseNPMLockfile(t *testing.T) {
	t.Run("lockfile v3 packages", func(t *testing.T) {
		input := []byte(`{
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "demo", "version": "1.0.0" },
    "node_modules/@actions/core": { "version": "1.10.1", "integrity": "sha512-core" },
    "node_modules/uuid": { "version": "9.0.1" }
  }
}`)

		packages, err := ParseNPMLockfile(input)
		if err != nil {
			t.Fatalf("ParseNPMLockfile failed: %v", err)
		}
		if len(packages) != 2 {
			t.Fatalf("package count = %d, want 2", len(packages))
		}
	})

	t.Run("lockfile v1 nested dependencies", func(t *testing.T) {
		input := []byte(`{
  "lockfileVersion": 1,
  "dependencies": {
    "@actions/core": {
      "version": "1.10.1",
      "dependencies": {
        "uuid": { "version": "9.0.1" }
      }
    }
  }
}`)

		packages, err := ParseNPMLockfile(input)
		if err != nil {
			t.Fatalf("ParseNPMLockfile failed: %v", err)
		}
		if len(packages) != 2 {
			t.Fatalf("package count = %d, want 2", len(packages))
		}
	})
}

func TestParseYarnLockfile(t *testing.T) {
	t.Run("classic lockfile", func(t *testing.T) {
		input := []byte(`# yarn lockfile v1

"@actions/core@^1.10.1", "@actions/core@^1.11.0":
  version "1.10.1"
  resolved "https://registry.yarnpkg.com/@actions/core/-/core-1.10.1.tgz"
  integrity sha512-core
  dependencies:
    uuid "^9.0.0"

"uuid@^9.0.0":
  version "9.0.1"
  resolved "https://registry.yarnpkg.com/uuid/-/uuid-9.0.1.tgz"
`)

		packages, err := ParseYarnLockfile(input)
		if err != nil {
			t.Fatalf("ParseYarnLockfile failed: %v", err)
		}
		if len(packages) != 2 {
			t.Fatalf("package count = %d, want 2", len(packages))
		}
	})

	t.Run("berry lockfile", func(t *testing.T) {
		input := []byte(`__metadata:
  version: 8

"@actions/core@npm:^1.10.1":
  version: 1.10.1
  resolution: "@actions/core@npm:1.10.1"
  checksum: berry-core

"uuid@npm:^9.0.0":
  version: 9.0.1
  resolution: "uuid@npm:9.0.1"
`)

		packages, err := ParseYarnLockfile(input)
		if err != nil {
			t.Fatalf("ParseYarnLockfile failed: %v", err)
		}
		if len(packages) != 2 {
			t.Fatalf("package count = %d, want 2", len(packages))
		}
	})
}

func TestParsePNPMLockfile(t *testing.T) {
	t.Run("path keyed packages", func(t *testing.T) {
		input := []byte(`lockfileVersion: '6.0'
packages:
  /@actions/core/1.10.1:
    resolution:
      integrity: sha512-core
  /uuid/9.0.1:
    resolution:
      integrity: sha512-uuid
`)

		packages, err := ParsePNPMLockfile(input)
		if err != nil {
			t.Fatalf("ParsePNPMLockfile failed: %v", err)
		}
		if len(packages) != 2 {
			t.Fatalf("package count = %d, want 2", len(packages))
		}
	})

	t.Run("version keyed packages", func(t *testing.T) {
		input := []byte(`lockfileVersion: '9.0'
packages:
  '@actions/core@1.10.1':
    resolution:
      integrity: sha512-core
  'uuid@9.0.1(react@18.0.0)':
    resolution:
      integrity: sha512-uuid
`)

		packages, err := ParsePNPMLockfile(input)
		if err != nil {
			t.Fatalf("ParsePNPMLockfile failed: %v", err)
		}
		if len(packages) != 2 {
			t.Fatalf("package count = %d, want 2", len(packages))
		}
	})
}
