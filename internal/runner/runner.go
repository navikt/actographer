package runner

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/navikt/actographer/internal/config"
	"github.com/navikt/actographer/internal/fetcher"
	"github.com/navikt/actographer/internal/models"
	"github.com/navikt/actographer/internal/parser"
	"github.com/navikt/actographer/internal/writer"
	"golang.org/x/sync/errgroup"
)

type App struct {
	Cfg      config.Config
	Fetcher  fetcherClient
	Resolver resolverClient
}

type fetcherClient interface {
	GetRepo(ctx context.Context, repoFullName string) (fetcher.RepoMeta, error)
	ListRepos(ctx context.Context) ([]fetcher.RepoMeta, error)
	ListWorkflowFiles(ctx context.Context, repoFullName string) ([]string, error)
	GetFileContent(ctx context.Context, repoFullName, path string) ([]byte, error)
	ListActionTags(ctx context.Context, owner, repo string) ([]string, error)
	GetActionYAML(ctx context.Context, owner, repo, actionPath, ref string) ([]byte, error)
}

type resolverClient interface {
	Resolve(ctx context.Context, ref models.ActionRef) models.TransitiveDep
}

func NewApp(cfg config.Config, f fetcherClient, r resolverClient) *App {
	return &App{
		Cfg:      cfg,
		Fetcher:  f,
		Resolver: r,
	}
}

func (a *App) Run(rootCtx context.Context) error {
	start := time.Now()
	snapshot := start.UTC().Format(time.RFC3339)

	var repos []fetcher.RepoMeta
	var err error

	if len(a.Cfg.Repos) > 0 {
		slog.Info("Repo list mode", "count", len(a.Cfg.Repos))
		for _, name := range a.Cfg.Repos {
			repo, err := a.Fetcher.GetRepo(rootCtx, name)
			if err != nil {
				slog.Error("Failed to get repo", "repo", name, "error", err)
				continue
			}
			repos = append(repos, repo)
		}
	} else {
		slog.Info("Listing repos", "org", a.Cfg.Org)
		repos, err = a.Fetcher.ListRepos(rootCtx)
		if err != nil {
			return err
		}
	}
	slog.Info("Found repos", "count", len(repos))

	if a.Cfg.Mode == "publisher" {
		return a.runPublisherMode(rootCtx, repos, start, snapshot)
	}

	var mu sync.Mutex
	var repoInventories []models.RepoInventory

	sem := make(chan struct{}, a.Cfg.Parallelism)
	g, workerCtx := errgroup.WithContext(rootCtx)

	for _, repo := range repos {
		repo := repo
		sem <- struct{}{}

		g.Go(func() error {
			defer func() { <-sem }()

			inv, err := a.processRepo(workerCtx, repo.FullName, repo.Archived)
			if err != nil {
				slog.Error("Failed to process repo", "repo", repo.FullName, "error", err)
				return nil
			}

			if len(inv.Workflows) > 0 {
				mu.Lock()
				repoInventories = append(repoInventories, inv)
				mu.Unlock()
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	inventory := models.OrgInventory{
		Org:          a.Cfg.Org,
		SnapshotDate: snapshot,
		Summary:      buildSummary(len(repos), repoInventories, a.Cfg.FetchSBOM),
		Repos:        repoInventories,
	}

	outputFile := fmt.Sprintf("%s/inventory-%s.json", a.Cfg.OutputPath, start.UTC().Format("20060102T150405Z"))
	slog.Info("Writing output", "path", outputFile, "repos_with_workflows", len(repoInventories))
	if err := writer.WriteJSON(inventory, outputFile); err != nil {
		return err
	}
	if err := writer.WriteTree(inventory, strings.TrimSuffix(outputFile, ".json")+".tree.txt"); err != nil {
		return err
	}

	slog.Info("Done",
		"duration", time.Since(start).Truncate(time.Second).String(),
		"api_calls", fetcher.GetAPICallCount(),
		"repos_scanned", len(repos),
		"repos_with_workflows", len(repoInventories),
	)

	return nil
}

func (a *App) processRepo(ctx context.Context, repoFullName string, archived bool) (models.RepoInventory, error) {
	inv := models.RepoInventory{
		RepoFullName: repoFullName,
		Archived:     archived,
	}

	workflowPaths, err := a.Fetcher.ListWorkflowFiles(ctx, repoFullName)
	if err != nil {
		return inv, err
	}

	if len(workflowPaths) == 0 {
		return inv, nil
	}

	slog.Debug("Found workflows", "repo", repoFullName, "count", len(workflowPaths))

	for _, path := range workflowPaths {
		content, err := a.Fetcher.GetFileContent(ctx, repoFullName, path)
		if err != nil {
			slog.Warn("Could not fetch workflow", "repo", repoFullName, "path", path, "error", err)
			continue
		}

		wf, err := parser.ParseWorkflow(content, repoFullName, path)
		if err != nil {
			slog.Warn("Could not parse workflow", "repo", repoFullName, "path", path, "error", err)
			continue
		}

		inv.Workflows = append(inv.Workflows, wf)
	}

	for wfIdx := range inv.Workflows {
		for jobIdx := range inv.Workflows[wfIdx].Jobs {
			job := &inv.Workflows[wfIdx].Jobs[jobIdx]
			if job.Uses != nil {
				job.Dependencies = append(job.Dependencies, a.Resolver.Resolve(ctx, *job.Uses))
			}
			for _, ref := range job.StepUses {
				job.Dependencies = append(job.Dependencies, a.Resolver.Resolve(ctx, ref))
			}
		}
	}

	return inv, nil
}

func buildSummary(reposScanned int, inventories []models.RepoInventory, includeKnownTransitive bool) *models.Summary {
	counts := make(map[string]int)
	transitiveCounts := make(map[string]int)
	var totalWorkflows, totalDeps, pinned, unpinned int
	var totalTransitive, totalKnownTransitive, maxDepth int

	for _, inv := range inventories {
		totalWorkflows += len(inv.Workflows)
		for _, wf := range inv.Workflows {
			for _, j := range wf.Jobs {
				if j.Uses != nil {
					totalDeps++
					key := j.Uses.Owner + "/" + j.Uses.Repo
					if j.Uses.Path != "" {
						key += "/" + j.Uses.Path
					}
					counts[key]++
					if j.Uses.Pinned {
						pinned++
					} else {
						unpinned++
					}
				}
				for _, ref := range j.StepUses {
					totalDeps++
					key := ref.Owner + "/" + ref.Repo
					if ref.Path != "" {
						key += "/" + ref.Path
					}
					if ref.Type == models.ActionRefLocal || ref.Type == models.ActionRefDocker {
						key = ref.Raw
					}
					counts[key]++
					if ref.Pinned {
						pinned++
					} else {
						unpinned++
					}
				}
			}
		}
		seenRoots := make(map[string]bool)
		for _, wf := range inv.Workflows {
			for _, j := range wf.Jobs {
				for _, dep := range j.Dependencies {
					rootKey := dep.Action.Raw
					if seenRoots[rootKey] {
						continue
					}
					seenRoots[rootKey] = true
					walkTransitiveDeps(dep, transitiveCounts, &totalTransitive, &maxDepth)
					totalKnownTransitive += countKnownTransitiveDeps(dep)
				}
			}
		}
	}

	type kv struct {
		key   string
		count int
	}
	var sorted []kv
	for k, v := range counts {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	limit := 10
	if len(sorted) < limit {
		limit = len(sorted)
	}
	var top []models.ActionCount
	for _, s := range sorted[:limit] {
		top = append(top, models.ActionCount{Action: s.key, Count: s.count})
	}

	var transitiveSorted []kv
	for k, v := range transitiveCounts {
		transitiveSorted = append(transitiveSorted, kv{k, v})
	}
	sort.Slice(transitiveSorted, func(i, j int) bool {
		return transitiveSorted[i].count > transitiveSorted[j].count
	})
	tLimit := 10
	if len(transitiveSorted) < tLimit {
		tLimit = len(transitiveSorted)
	}
	var topTransitive []models.ActionCount
	for _, s := range transitiveSorted[:tLimit] {
		topTransitive = append(topTransitive, models.ActionCount{Action: s.key, Count: s.count})
	}

	summary := &models.Summary{
		ReposScanned:                 reposScanned,
		ReposWithWorkflows:           len(inventories),
		TotalWorkflows:               totalWorkflows,
		TotalDirectDeps:              totalDeps,
		UniqueDirectDeps:             len(counts),
		PinnedDeps:                   pinned,
		UnpinnedDeps:                 unpinned,
		TotalTransitiveGitHubActions: totalTransitive,
		UniqueTransitiveDeps:         len(transitiveCounts),
		MaxDepth:                     maxDepth,
		TopActions:                   top,
		TopTransitiveActions:         topTransitive,
	}

	if includeKnownTransitive {
		summary.TotalKnownTransitiveDeps = &totalKnownTransitive
	}

	return summary
}

func walkTransitiveDeps(dep models.TransitiveDep, counts map[string]int, total *int, maxDepth *int) {
	for _, d := range dep.Deps {
		*total++
		key := d.Action.Owner + "/" + d.Action.Repo
		if d.Action.Path != "" {
			key += "/" + d.Action.Path
		}
		counts[key]++
		if d.Depth > *maxDepth {
			*maxDepth = d.Depth
		}
		walkTransitiveDeps(d, counts, total, maxDepth)
	}
}

func countKnownTransitiveDeps(dep models.TransitiveDep) int {
	total := len(dep.Deps)
	if dep.SBOM != nil {
		total += len(dep.SBOM.Packages)
	}
	for _, child := range dep.Deps {
		total += countKnownTransitiveDeps(child)
	}
	return total
}

func (a *App) runPublisherMode(ctx context.Context, repos []fetcher.RepoMeta, start time.Time, snapshot string) error {
	rootCtx := ctx

	// Phase 1: detect action repos in parallel (repos with action.yml at HEAD)
	var mu sync.Mutex
	var actionRepos []fetcher.RepoMeta

	sem := make(chan struct{}, a.Cfg.Parallelism)
	g, ctx := errgroup.WithContext(rootCtx)

	for _, repo := range repos {
		repo := repo
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()
			parts := strings.SplitN(repo.FullName, "/", 2)
			if len(parts) != 2 {
				return nil
			}
			content, err := a.Fetcher.GetActionYAML(ctx, parts[0], parts[1], "", "HEAD")
			if err != nil {
				slog.Debug("Could not check action.yml at HEAD", "repo", repo.FullName, "error", err)
				return nil
			}
			if content == nil {
				return nil
			}
			mu.Lock()
			actionRepos = append(actionRepos, repo)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	slog.Info("Detected published action repos", "count", len(actionRepos))

	// Phase 2: process each action repo in parallel
	var mu2 sync.Mutex
	var publishedActions []models.PublishedAction

	sem2 := make(chan struct{}, a.Cfg.Parallelism)
	g2, ctx := errgroup.WithContext(rootCtx)

	for _, repo := range actionRepos {
		repo := repo
		sem2 <- struct{}{}
		g2.Go(func() error {
			defer func() { <-sem2 }()
			pa, err := a.processPublishedAction(ctx, repo)
			if err != nil {
				slog.Error("Failed to process published action", "repo", repo.FullName, "error", err)
				return nil
			}
			mu2.Lock()
			publishedActions = append(publishedActions, pa)
			mu2.Unlock()
			return nil
		})
	}

	if err := g2.Wait(); err != nil {
		return err
	}

	inventory := models.OrgInventory{
		Org:              a.Cfg.Org,
		SnapshotDate:     snapshot,
		PublisherSummary: buildPublisherSummary(len(repos), publishedActions),
		PublishedActions: publishedActions,
	}

	outputFile := fmt.Sprintf("%s/inventory-%s.json", a.Cfg.OutputPath, start.UTC().Format("20060102T150405Z"))
	slog.Info("Writing output", "path", outputFile, "published_actions", len(publishedActions))
	if err := writer.WriteJSON(inventory, outputFile); err != nil {
		return err
	}
	if err := writer.WriteTree(inventory, strings.TrimSuffix(outputFile, ".json")+".tree.txt"); err != nil {
		return err
	}

	slog.Info("Done",
		"duration", time.Since(start).Truncate(time.Second).String(),
		"api_calls", fetcher.GetAPICallCount(),
		"repos_scanned", len(repos),
		"published_actions", len(publishedActions),
	)

	return nil
}

func (a *App) processPublishedAction(ctx context.Context, repo fetcher.RepoMeta) (models.PublishedAction, error) {
	parts := strings.SplitN(repo.FullName, "/", 2)
	owner, repoName := parts[0], parts[1]

	pa := models.PublishedAction{
		RepoFullName: repo.FullName,
		Archived:     repo.Archived,
	}

	tags, err := a.Fetcher.ListActionTags(ctx, owner, repoName)
	if err != nil {
		return pa, fmt.Errorf("listing tags for %s: %w", repo.FullName, err)
	}

	slog.Debug("Processing published action", "repo", repo.FullName, "tags", len(tags))

	refsToProcess := tags
	if len(tags) == 0 {
		refsToProcess = []string{"HEAD"}
	}

	for _, tag := range refsToProcess {
		content, err := a.Fetcher.GetActionYAML(ctx, owner, repoName, "", tag)
		if err != nil || content == nil {
			slog.Debug("No action.yml at tag", "repo", repo.FullName, "tag", tag)
			continue
		}

		refs, err := parser.ParseCompositeAction(content)
		if err != nil {
			slog.Warn("Could not parse action.yml at tag", "repo", repo.FullName, "tag", tag, "error", err)
			continue
		}

		av := models.ActionVersion{Tag: tag}
		for _, ref := range refs {
			dep := a.Resolver.Resolve(ctx, ref)
			av.Deps = append(av.Deps, dep)
		}
		pa.Versions = append(pa.Versions, av)
	}

	return pa, nil
}

func buildPublisherSummary(reposScanned int, actions []models.PublishedAction) *models.PublisherSummary {
	totalVersions := 0
	totalDeps := 0
	for _, a := range actions {
		totalVersions += len(a.Versions)
		for _, v := range a.Versions {
			totalDeps += countTransitiveDeps(v.Deps)
		}
	}
	return &models.PublisherSummary{
		ReposScanned:      reposScanned,
		ActionsFound:      len(actions),
		TotalVersions:     totalVersions,
		TotalDepsResolved: totalDeps,
	}
}

func countTransitiveDeps(deps []models.TransitiveDep) int {
	count := len(deps)
	for _, d := range deps {
		count += countTransitiveDeps(d.Deps)
	}
	return count
}
