package fetcher

import "testing"

func TestIsWorkflowFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{".github/workflows/ci.yml", true},
		{".github/workflows/deploy.yaml", true},
		{".github/workflows/build.yml", true},
		{".github/workflows/sub/nested.yml", false},
		{".github/dependabot.yml", false},
		{"workflows/ci.yml", false},
		{".github/workflows/readme.md", false},
		{"", false},
		{".github/workflows/.yml", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isWorkflowFile(tt.path)
			if got != tt.want {
				t.Errorf("isWorkflowFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
