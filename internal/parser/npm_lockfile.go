package parser

import (
	"encoding/json"
	"strings"

	"github.com/navikt/actographer/internal/models"
)

type npmLockfile struct {
	LockfileVersion int                          `json:"lockfileVersion"`
	Packages        map[string]npmLockPackage    `json:"packages"`
	Dependencies    map[string]npmLockDependency `json:"dependencies"`
}

type npmLockPackage struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Resolved  string `json:"resolved"`
	Integrity string `json:"integrity"`
	Dev       bool   `json:"dev"`
	Optional  bool   `json:"optional"`
	Link      bool   `json:"link"`
}

type npmLockDependency struct {
	Version      string                       `json:"version"`
	Resolved     string                       `json:"resolved"`
	Integrity    string                       `json:"integrity"`
	Dev          bool                         `json:"dev"`
	Optional     bool                         `json:"optional"`
	Dependencies map[string]npmLockDependency `json:"dependencies"`
}

func ParseNPMLockfile(content []byte) ([]models.SBOMPackage, error) {
	var lockfile npmLockfile
	if err := json.Unmarshal(content, &lockfile); err != nil {
		return nil, err
	}

	if len(lockfile.Packages) > 0 {
		return parseNPMPackages(lockfile.Packages), nil
	}

	return parseNPMDependencies(lockfile.Dependencies), nil
}

func parseNPMPackages(packages map[string]npmLockPackage) []models.SBOMPackage {
	seen := make(map[string]bool)
	var result []models.SBOMPackage

	for path, pkg := range packages {
		if path == "" || pkg.Link || pkg.Version == "" {
			continue
		}

		name := pkg.Name
		if name == "" {
			name = packageNameFromLockfilePath(path)
		}
		if name == "" {
			continue
		}

		key := name + "@" + pkg.Version
		if seen[key] {
			continue
		}
		seen[key] = true

		result = append(result, models.SBOMPackage{
			Name:      name,
			Version:   pkg.Version,
			Resolved:  pkg.Resolved,
			Integrity: pkg.Integrity,
			Dev:       pkg.Dev,
			Optional:  pkg.Optional,
		})
	}

	return result
}

func parseNPMDependencies(dependencies map[string]npmLockDependency) []models.SBOMPackage {
	seen := make(map[string]bool)
	var result []models.SBOMPackage
	walkNPMDependencies(dependencies, seen, &result)
	return result
}

func walkNPMDependencies(dependencies map[string]npmLockDependency, seen map[string]bool, result *[]models.SBOMPackage) {
	for name, dep := range dependencies {
		if dep.Version == "" {
			continue
		}

		key := name + "@" + dep.Version
		if !seen[key] {
			seen[key] = true
			*result = append(*result, models.SBOMPackage{
				Name:      name,
				Version:   dep.Version,
				Resolved:  dep.Resolved,
				Integrity: dep.Integrity,
				Dev:       dep.Dev,
				Optional:  dep.Optional,
			})
		}

		if len(dep.Dependencies) > 0 {
			walkNPMDependencies(dep.Dependencies, seen, result)
		}
	}
}

func packageNameFromLockfilePath(path string) string {
	const marker = "node_modules/"

	idx := strings.LastIndex(path, marker)
	if idx == -1 {
		return ""
	}

	return path[idx+len(marker):]
}
