package config

import (
	"flag"
	"strings"
	"testing"
)

func TestNewConfig(t *testing.T) {
	t.Run("parses org flags", func(t *testing.T) {
		cfg, err := newConfig(
			[]string{"--org", "navikt", "--parallelism", "8", "--output-path", "artifacts", "--fetch-sbom", "--debug"},
			func(key string) string {
				if key == "GITHUB_TOKEN" {
					return "secret"
				}
				return ""
			},
			&strings.Builder{},
		)
		if err != nil {
			t.Fatalf("newConfig returned error: %v", err)
		}
		if cfg.Org != "navikt" {
			t.Fatalf("expected org navikt, got %q", cfg.Org)
		}
		if cfg.Mode != "consumer" {
			t.Fatalf("expected consumer mode, got %q", cfg.Mode)
		}
		if cfg.Parallelism != 8 {
			t.Fatalf("expected parallelism 8, got %d", cfg.Parallelism)
		}
		if cfg.OutputPath != "artifacts" {
			t.Fatalf("expected output path artifacts, got %q", cfg.OutputPath)
		}
		if !cfg.FetchSBOM || !cfg.Debug {
			t.Fatalf("expected boolean flags to be true: %+v", cfg)
		}
	})

	t.Run("parses repeated repo flags", func(t *testing.T) {
		cfg, err := newConfig(
			[]string{"--repo", "navikt/actographer", "--repo", "navikt/tpt-backend"},
			func(key string) string {
				if key == "GITHUB_TOKEN" {
					return "secret"
				}
				return ""
			},
			&strings.Builder{},
		)
		if err != nil {
			t.Fatalf("newConfig returned error: %v", err)
		}
		if len(cfg.Repos) != 2 {
			t.Fatalf("expected 2 repos, got %d", len(cfg.Repos))
		}
	})

	t.Run("defaults to consumer mode", func(t *testing.T) {
		cfg, err := newConfig(
			[]string{"--org", "navikt"},
			func(key string) string {
				if key == "GITHUB_TOKEN" {
					return "secret"
				}
				return ""
			},
			&strings.Builder{},
		)
		if err != nil {
			t.Fatalf("newConfig returned error: %v", err)
		}
		if cfg.Mode != "consumer" {
			t.Fatalf("expected consumer mode, got %q", cfg.Mode)
		}
	})

	t.Run("rejects missing target", func(t *testing.T) {
		_, err := newConfig([]string{}, func(string) string { return "secret" }, &strings.Builder{})
		if err == nil || err.Error() != "exactly one of --org or --repo must be provided" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects both org and repo", func(t *testing.T) {
		_, err := newConfig(
			[]string{"--org", "navikt", "--repo", "navikt/actographer"},
			func(string) string { return "secret" },
			&strings.Builder{},
		)
		if err == nil || err.Error() != "--org and --repo cannot be used together" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects missing token", func(t *testing.T) {
		_, err := newConfig(
			[]string{"--org", "navikt"},
			func(string) string { return "" },
			&strings.Builder{},
		)
		if err == nil || err.Error() != "GITHUB_TOKEN must be set" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects invalid mode", func(t *testing.T) {
		_, err := newConfig(
			[]string{"--org", "navikt", "--mode", "publish"},
			func(string) string { return "secret" },
			&strings.Builder{},
		)
		if err == nil || !strings.Contains(err.Error(), "--mode must be one of consumer or publisher") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects invalid repo flag", func(t *testing.T) {
		_, err := newConfig(
			[]string{"--repo", "navikt"},
			func(string) string { return "secret" },
			&strings.Builder{},
		)
		if err == nil || !strings.Contains(err.Error(), "--repo must be in owner/repo form") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects invalid parallelism", func(t *testing.T) {
		_, err := newConfig(
			[]string{"--org", "navikt", "--parallelism", "0"},
			func(string) string { return "secret" },
			&strings.Builder{},
		)
		if err == nil || err.Error() != "--parallelism must be a positive integer" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects positional arguments", func(t *testing.T) {
		_, err := newConfig(
			[]string{"--org", "navikt", "extra"},
			func(string) string { return "secret" },
			&strings.Builder{},
		)
		if err == nil || !strings.Contains(err.Error(), "unexpected positional arguments") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns flag help sentinel", func(t *testing.T) {
		var stderr strings.Builder
		_, err := newConfig(
			[]string{"--help"},
			func(string) string { return "secret" },
			&stderr,
		)
		if err != flag.ErrHelp {
			t.Fatalf("expected help error, got %v", err)
		}
		helpText := stderr.String()
		for _, want := range []string{
			"Required:",
			"Authentication:",
			"Optional mode and enrichment flags:",
			"Optional execution flags:",
		} {
			if !strings.Contains(helpText, want) {
				t.Fatalf("expected help text to contain %q, got:\n%s", want, helpText)
			}
		}
	})
}
