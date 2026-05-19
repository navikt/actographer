package resolver

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/navikt/actographer/internal/models"
	"github.com/navikt/actographer/internal/parser"
)

const MaxDepth = 5

type actionFileFetcher interface {
	GetActionYAML(ctx context.Context, owner, repo, actionPath, ref string) ([]byte, error)
	GetRepoFileAtRef(ctx context.Context, owner, repo, path, ref string) ([]byte, error)
}

type Resolver struct {
	Fetcher   actionFileFetcher
	FetchSBOM bool
	mu        sync.RWMutex
	cache     map[string]resolvedActionData
}

type resolvedActionData struct {
	innerRefs []models.ActionRef
	sbom      *models.ActionSBOM
}

func NewResolver(f actionFileFetcher, fetchSBOM bool) *Resolver {
	return &Resolver{
		Fetcher:   f,
		FetchSBOM: fetchSBOM,
		cache:     make(map[string]resolvedActionData),
	}
}

func (r *Resolver) Resolve(ctx context.Context, ref models.ActionRef) models.TransitiveDep {
	return r.resolve(ctx, ref, 0)
}

func (r *Resolver) resolve(ctx context.Context, ref models.ActionRef, depth int) models.TransitiveDep {
	dep := models.TransitiveDep{
		Action: ref,
		Depth:  depth,
	}

	if depth >= MaxDepth {
		slog.Warn("Max transitive depth reached", "action", ref.Raw, "depth", depth)
		return dep
	}

	if ref.Type != models.ActionRefAction || ref.Owner == "" || ref.Repo == "" {
		return dep
	}

	cacheKey := fmt.Sprintf("%s/%s@%s", ref.Owner, ref.Repo, ref.Ref)
	if ref.Path != "" {
		cacheKey = fmt.Sprintf("%s/%s/%s@%s", ref.Owner, ref.Repo, ref.Path, ref.Ref)
	}

	r.mu.RLock()
	cached, ok := r.cache[cacheKey]
	r.mu.RUnlock()
	if ok {
		dep.SBOM = cached.sbom
		for _, inner := range cached.innerRefs {
			dep.Deps = append(dep.Deps, r.resolve(ctx, inner, depth+1))
		}
		return dep
	}

	resolveRef := ref.Ref
	if resolveRef == "" {
		resolveRef = "HEAD"
	}

	content, err := r.Fetcher.GetActionYAML(ctx, ref.Owner, ref.Repo, ref.Path, resolveRef)
	if err != nil {
		slog.Debug("Could not fetch action.yml", "action", cacheKey, "error", err)
		r.setCache(cacheKey, resolvedActionData{})
		return dep
	}

	if content == nil {
		r.setCache(cacheKey, resolvedActionData{})
		return dep
	}

	def, err := parser.ParseActionDefinition(content)
	if err != nil {
		slog.Debug("Could not parse action.yml", "action", cacheKey, "error", err)
		r.setCache(cacheKey, resolvedActionData{})
		return dep
	}

	if r.FetchSBOM && def.IsJavaScriptAction() {
		dep.SBOM = r.fetchActionSBOM(ctx, ref, resolveRef)
	}

	r.setCache(cacheKey, resolvedActionData{
		innerRefs: def.InnerRefs,
		sbom:      dep.SBOM,
	})

	for _, inner := range def.InnerRefs {
		dep.Deps = append(dep.Deps, r.resolve(ctx, inner, depth+1))
	}

	return dep
}

func (r *Resolver) fetchActionSBOM(ctx context.Context, ref models.ActionRef, resolveRef string) *models.ActionSBOM {
	for _, candidatePath := range nodeLockfileCandidates(ref.Path) {
		content, err := r.Fetcher.GetRepoFileAtRef(ctx, ref.Owner, ref.Repo, candidatePath, resolveRef)
		if err != nil {
			slog.Debug("Could not fetch JS action manifest", "action", ref.Raw, "path", candidatePath, "error", err)
			continue
		}
		if content == nil {
			continue
		}

		ecosystem, packages, err := parser.ParseNodeLockfile(candidatePath, content)
		if err != nil {
			slog.Debug("Could not parse JS lockfile", "action", ref.Raw, "path", candidatePath, "error", err)
			continue
		}
		if len(packages) == 0 {
			slog.Debug("Parsed lockfile had no packages", "action", ref.Raw, "path", candidatePath)
			continue
		}

		sort.Slice(packages, func(i, j int) bool {
			if packages[i].Name == packages[j].Name {
				return packages[i].Version < packages[j].Version
			}
			return packages[i].Name < packages[j].Name
		})

		return &models.ActionSBOM{
			Ecosystem:  ecosystem,
			SourcePath: candidatePath,
			Status:     "resolved",
			Packages:   packages,
		}
	}

	return &models.ActionSBOM{
		Ecosystem: "node",
		Status:    "no_lockfile",
	}
}

func nodeLockfileCandidates(actionPath string) []string {
	filenames := []string{"package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock"}
	seen := make(map[string]bool)
	var candidates []string

	current := strings.Trim(actionPath, "/")
	for {
		for _, filename := range filenames {
			candidate := filename
			if current != "" {
				candidate = path.Join(current, filename)
			}
			if !seen[candidate] {
				seen[candidate] = true
				candidates = append(candidates, candidate)
			}
		}

		if current == "" {
			break
		}

		current = path.Dir(current)
		if current == "." {
			current = ""
		}
	}

	return candidates
}

func (r *Resolver) setCache(key string, data resolvedActionData) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[key] = data
}
