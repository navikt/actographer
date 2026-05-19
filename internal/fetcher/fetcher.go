package fetcher

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/navikt/actographer/internal/config"
)

var apiCallCounter atomic.Int64
var HttpClient = http.DefaultClient

type Fetcher struct {
	Cfg config.Config
}

func NewFetcher(cfg config.Config) *Fetcher {
	return &Fetcher{Cfg: cfg}
}

func GetAPICallCount() int64 {
	return apiCallCounter.Load()
}

type RepoMeta struct {
	FullName string `json:"full_name"`
	Archived bool   `json:"archived"`
}

func (f *Fetcher) GetRepo(ctx context.Context, repoFullName string) (RepoMeta, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s", repoFullName)
	var repo RepoMeta
	if err := doRequest(ctx, "GET", url, f.Cfg.Token, nil, &repo); err != nil {
		return RepoMeta{}, fmt.Errorf("fetching repo %s: %w", repoFullName, err)
	}
	return repo, nil
}

func (f *Fetcher) ListRepos(ctx context.Context) ([]RepoMeta, error) {
	var allRepos []RepoMeta
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/orgs/%s/repos?per_page=100&type=all&page=%d", f.Cfg.Org, page)
		var pageRepos []RepoMeta

		if err := doRequest(ctx, "GET", url, f.Cfg.Token, nil, &pageRepos); err != nil {
			return nil, fmt.Errorf("listing repos page %d: %w", page, err)
		}

		if len(pageRepos) == 0 {
			break
		}

		allRepos = append(allRepos, pageRepos...)
		slog.Info("Fetched repos page", "page", page, "count", len(pageRepos))
		page++
	}

	return allRepos, nil
}

type ghTreeResponse struct {
	Tree []ghTreeEntry `json:"tree"`
}

type ghTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func (f *Fetcher) ListWorkflowFiles(ctx context.Context, repoFullName string) ([]string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/git/trees/HEAD?recursive=1", repoFullName)

	var tree ghTreeResponse
	if err := doRequestOptional404(ctx, "GET", url, f.Cfg.Token, nil, &tree); err != nil {
		return nil, fmt.Errorf("fetching tree for %s: %w", repoFullName, err)
	}

	var paths []string
	for _, entry := range tree.Tree {
		if entry.Type != "blob" {
			continue
		}
		if isWorkflowFile(entry.Path) {
			paths = append(paths, entry.Path)
		}
	}

	return paths, nil
}

func isWorkflowFile(path string) bool {
	if len(path) < len(".github/workflows/x.yml") {
		return false
	}
	const prefix = ".github/workflows/"
	if path[:len(prefix)] != prefix {
		return false
	}
	rest := path[len(prefix):]
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' {
			return false
		}
	}
	return hasSuffix(rest, ".yml") || hasSuffix(rest, ".yaml")
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

type ghContentResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

func (f *Fetcher) GetFileContent(ctx context.Context, repoFullName, path string) ([]byte, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", repoFullName, path)
	return fetchContent(ctx, url, f.Cfg.Token, fmt.Sprintf("%s/%s", repoFullName, path))
}

func (f *Fetcher) GetActionYAML(ctx context.Context, owner, repo, actionPath, ref string) ([]byte, error) {
	for _, filename := range []string{"action.yml", "action.yaml"} {
		filePath := filename
		if actionPath != "" {
			filePath = actionPath + "/" + filename
		}
		decoded, err := f.GetRepoFileAtRef(ctx, owner, repo, filePath, ref)
		if err != nil {
			continue
		}
		if decoded != nil {
			return decoded, nil
		}
	}

	return nil, nil
}

func (f *Fetcher) GetRepoFileAtRef(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", owner, repo, path, ref)
	return fetchContent(ctx, url, f.Cfg.Token, fmt.Sprintf("%s/%s@%s", owner+"/"+repo, path, ref))
}

type ghTag struct {
	Name string `json:"name"`
}

func (f *Fetcher) ListActionTags(ctx context.Context, owner, repo string) ([]string, error) {
	var allTags []string
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/tags?per_page=100&page=%d", owner, repo, page)
		var tags []ghTag

		if err := doRequest(ctx, "GET", url, f.Cfg.Token, nil, &tags); err != nil {
			return nil, fmt.Errorf("listing tags for %s/%s page %d: %w", owner, repo, page, err)
		}

		if len(tags) == 0 {
			break
		}

		for _, t := range tags {
			allTags = append(allTags, t.Name)
		}
		page++
	}

	return allTags, nil
}

func doRequest(ctx context.Context, method, url, token string, body []byte, out interface{}) error {
	for {
		apiCallCounter.Add(1)
		slog.Debug("API request", "method", method, "url", url)

		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := HttpClient.Do(req)
		if err != nil {
			return err
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("reading response body: %w", err)
		}

		if rl := resp.Header.Get("X-RateLimit-Remaining"); rl == "0" {
			reset := resp.Header.Get("X-RateLimit-Reset")
			if ts, parseErr := strconv.ParseInt(reset, 10, 64); parseErr == nil {
				wait := time.Until(time.Unix(ts, 0)) + time.Second
				slog.Warn("Rate limit hit, waiting", "wait", wait.Truncate(time.Second))
				time.Sleep(wait)
				continue
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("GitHub API error: status %d - %s", resp.StatusCode, string(respBody))
		}

		return json.Unmarshal(respBody, out)
	}
}

func doRequestOptional404(ctx context.Context, method, url, token string, body []byte, out interface{}) error {
	for {
		apiCallCounter.Add(1)
		slog.Debug("API request", "method", method, "url", url)

		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := HttpClient.Do(req)
		if err != nil {
			return err
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("reading response body: %w", err)
		}

		if rl := resp.Header.Get("X-RateLimit-Remaining"); rl == "0" {
			reset := resp.Header.Get("X-RateLimit-Reset")
			if ts, parseErr := strconv.ParseInt(reset, 10, 64); parseErr == nil {
				wait := time.Until(time.Unix(ts, 0)) + time.Second
				slog.Warn("Rate limit hit, waiting", "wait", wait.Truncate(time.Second))
				time.Sleep(wait)
				continue
			}
		}

		if resp.StatusCode == 404 || resp.StatusCode == 409 {
			return nil
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("GitHub API error: status %d - %s", resp.StatusCode, string(respBody))
		}

		return json.Unmarshal(respBody, out)
	}
}

func fetchContent(ctx context.Context, url, token, label string) ([]byte, error) {
	var content ghContentResponse
	if err := doRequestOptional404(ctx, "GET", url, token, nil, &content); err != nil {
		return nil, fmt.Errorf("fetching %s: %w", label, err)
	}

	if content.Content == "" {
		return nil, nil
	}

	if content.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected encoding %q for %s", content.Encoding, label)
	}

	raw := strings.ReplaceAll(content.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding base64 for %s: %w", label, err)
	}

	return decoded, nil
}
