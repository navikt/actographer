package parser

import (
	"strings"

	"github.com/navikt/actographer/internal/models"
	"gopkg.in/yaml.v3"
)

type pnpmLockfile struct {
	Packages  map[string]pnpmLockPackage `yaml:"packages"`
	Snapshots map[string]pnpmLockPackage `yaml:"snapshots"`
}

type pnpmLockPackage struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Dev        bool   `yaml:"dev"`
	Optional   bool   `yaml:"optional"`
	Resolution struct {
		Integrity string `yaml:"integrity"`
		Tarball   string `yaml:"tarball"`
	} `yaml:"resolution"`
}

func ParsePNPMLockfile(content []byte) ([]models.SBOMPackage, error) {
	var lockfile pnpmLockfile
	if err := yaml.Unmarshal(content, &lockfile); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var result []models.SBOMPackage

	appendEntries := func(entries map[string]pnpmLockPackage) {
		for key, entry := range entries {
			name, version := pnpmIdentity(key, entry)
			if name == "" || version == "" {
				continue
			}

			dupKey := name + "@" + version
			if seen[dupKey] {
				continue
			}
			seen[dupKey] = true

			result = append(result, models.SBOMPackage{
				Name:      name,
				Version:   version,
				Integrity: entry.Resolution.Integrity,
				Resolved:  entry.Resolution.Tarball,
				Dev:       entry.Dev,
				Optional:  entry.Optional,
			})
		}
	}

	appendEntries(lockfile.Packages)
	appendEntries(lockfile.Snapshots)

	return result, nil
}

func pnpmIdentity(key string, entry pnpmLockPackage) (string, string) {
	name := strings.TrimSpace(entry.Name)
	version := cleanPNPMVersion(entry.Version)
	if name != "" && version != "" {
		return name, version
	}

	raw := strings.TrimPrefix(strings.TrimSpace(key), "/")
	if raw == "" {
		return "", ""
	}

	if idx := strings.Index(raw, "("); idx != -1 {
		raw = raw[:idx]
	}

	if strings.HasPrefix(raw, "@") && strings.Count(raw, "/") >= 2 {
		parts := strings.Split(raw, "/")
		return parts[0] + "/" + parts[1], cleanPNPMVersion(parts[2])
	}

	if strings.Contains(raw, "/") {
		parts := strings.SplitN(raw, "/", 2)
		return parts[0], cleanPNPMVersion(parts[1])
	}

	if idx := strings.LastIndex(raw, "@"); idx > 0 {
		return raw[:idx], cleanPNPMVersion(raw[idx+1:])
	}

	return "", ""
}

func cleanPNPMVersion(version string) string {
	version = strings.TrimSpace(version)
	if idx := strings.Index(version, "("); idx != -1 {
		version = version[:idx]
	}
	if idx := strings.Index(version, "_"); idx != -1 {
		version = version[:idx]
	}
	return version
}
