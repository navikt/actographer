package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Org         string
	Repos       []string
	Token       string
	Debug       bool
	Parallelism int
	OutputPath  string
	FetchSBOM   bool
	Mode        string
}

type repoList []string

func (r *repoList) String() string {
	return strings.Join(*r, ",")
}

func (r *repoList) Set(value string) error {
	repo := strings.TrimSpace(value)
	if err := validateRepo(repo); err != nil {
		return err
	}
	*r = append(*r, repo)
	return nil
}

func NewConfig(args []string) (Config, error) {
	return newConfig(args, os.Getenv, os.Stderr)
}

func newConfig(args []string, getenv func(string) string, stderr io.Writer) (Config, error) {
	var repos repoList

	fs := flag.NewFlagSet("actographer", flag.ContinueOnError)
	fs.SetOutput(stderr)

	org := fs.String("org", "", "GitHub organization to scan")
	fs.Var(&repos, "repo", "Repository to scan in owner/repo form; repeat to scan multiple repos")
	mode := fs.String("mode", "consumer", "Scan mode: consumer or publisher")
	outputPath := fs.String("output-path", "output", "Directory to write inventory files to")
	parallelism := fs.Int("parallelism", 5, "Number of repositories to process concurrently")
	fetchSBOM := fs.Bool("fetch-sbom", false, "Fetch npm lockfile package inventories for JavaScript actions")
	debug := fs.Bool("debug", false, "Enable verbose debug logging")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [flags]\n\n", fs.Name())
		fmt.Fprintln(fs.Output(), "Required:")
		fmt.Fprintln(fs.Output(), "  Choose exactly one target selector:")
		fmt.Fprintln(fs.Output(), "    --org <org>          Scan all repositories in a GitHub organization")
		fmt.Fprintln(fs.Output(), "    --repo <owner/repo>  Scan a specific repository; repeat to scan multiple repos")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Authentication:")
		fmt.Fprintln(fs.Output(), "  GITHUB_TOKEN must be set in the environment.")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Optional mode and enrichment flags:")
		fmt.Fprintln(fs.Output(), "  --mode consumer|publisher  Select inventory mode (default: consumer)")
		fmt.Fprintln(fs.Output(), "  --fetch-sbom               Add npm lockfile inventories for JavaScript actions")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Optional execution flags:")
		fmt.Fprintln(fs.Output(), "  --output-path <dir>        Output directory (default: output)")
		fmt.Fprintln(fs.Output(), "  --parallelism <n>          Concurrent repository workers (default: 5)")
		fmt.Fprintln(fs.Output(), "  --debug                    Enable verbose debug logging")
	}

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if len(fs.Args()) > 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	cfg := Config{
		Org:         strings.TrimSpace(*org),
		Repos:       repos,
		Token:       strings.TrimSpace(getenv("GITHUB_TOKEN")),
		Debug:       *debug,
		Parallelism: *parallelism,
		OutputPath:  strings.TrimSpace(*outputPath),
		FetchSBOM:   *fetchSBOM,
		Mode:        normalizeMode(*mode),
	}

	if cfg.Org != "" && len(cfg.Repos) > 0 {
		return Config{}, errors.New("--org and --repo cannot be used together")
	}
	if cfg.Org == "" && len(cfg.Repos) == 0 {
		return Config{}, errors.New("exactly one of --org or --repo must be provided")
	}
	if cfg.Token == "" {
		return Config{}, errors.New("GITHUB_TOKEN must be set")
	}
	if cfg.Mode != "consumer" && cfg.Mode != "publisher" {
		return Config{}, fmt.Errorf("--mode must be one of consumer or publisher, got %q", *mode)
	}
	if cfg.Parallelism < 1 {
		return Config{}, errors.New("--parallelism must be a positive integer")
	}
	if cfg.OutputPath == "" {
		return Config{}, errors.New("--output-path cannot be empty")
	}
	cfg.OutputPath = filepath.Clean(cfg.OutputPath)

	return cfg, nil
}

func normalizeMode(mode string) string {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		return "consumer"
	}
	return strings.ToLower(trimmed)
}

func validateRepo(repo string) error {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("--repo must be in owner/repo form, got %q", repo)
	}
	return nil
}
