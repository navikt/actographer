package parser

import (
	"strings"

	"github.com/navikt/actographer/internal/models"
	"gopkg.in/yaml.v3"
)

type yarnBerryEntry struct {
	Version    string `yaml:"version"`
	Resolution string `yaml:"resolution"`
	Integrity  string `yaml:"integrity"`
	Checksum   string `yaml:"checksum"`
}

func ParseYarnLockfile(content []byte) ([]models.SBOMPackage, error) {
	text := string(content)
	if strings.Contains(text, "__metadata:") {
		return parseYarnBerryLockfile(content)
	}
	return parseYarnClassicLockfile(text), nil
}

func parseYarnClassicLockfile(content string) []models.SBOMPackage {
	seen := make(map[string]bool)
	var result []models.SBOMPackage

	var currentName string
	var currentPackage models.SBOMPackage

	flush := func() {
		if currentName == "" || currentPackage.Version == "" {
			currentName = ""
			currentPackage = models.SBOMPackage{}
			return
		}

		currentPackage.Name = currentName
		key := currentPackage.Name + "@" + currentPackage.Version
		if !seen[key] {
			seen[key] = true
			result = append(result, currentPackage)
		}

		currentName = ""
		currentPackage = models.SBOMPackage{}
	}

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !strings.HasPrefix(line, " ") && strings.HasSuffix(trimmed, ":") {
			flush()
			currentName = yarnDescriptorName(strings.TrimSuffix(trimmed, ":"))
			continue
		}

		if currentName == "" || !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "    ") {
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "version "):
			currentPackage.Version = yarnValue(strings.TrimPrefix(trimmed, "version "))
		case strings.HasPrefix(trimmed, "resolved "):
			currentPackage.Resolved = yarnValue(strings.TrimPrefix(trimmed, "resolved "))
		case strings.HasPrefix(trimmed, "integrity "):
			currentPackage.Integrity = yarnValue(strings.TrimPrefix(trimmed, "integrity "))
		}
	}

	flush()
	return result
}

func parseYarnBerryLockfile(content []byte) ([]models.SBOMPackage, error) {
	var entries map[string]yarnBerryEntry
	if err := yaml.Unmarshal(content, &entries); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var result []models.SBOMPackage
	for key, entry := range entries {
		if key == "__metadata" || entry.Version == "" {
			continue
		}

		name := yarnDescriptorName(key)
		if name == "" {
			continue
		}

		pkg := models.SBOMPackage{
			Name:     name,
			Version:  yarnValue(entry.Version),
			Resolved: yarnValue(entry.Resolution),
		}
		if entry.Integrity != "" {
			pkg.Integrity = yarnValue(entry.Integrity)
		} else if entry.Checksum != "" {
			pkg.Integrity = yarnValue(entry.Checksum)
		}

		dupKey := pkg.Name + "@" + pkg.Version
		if seen[dupKey] {
			continue
		}
		seen[dupKey] = true
		result = append(result, pkg)
	}

	return result, nil
}

func yarnDescriptorName(descriptor string) string {
	first := strings.Split(descriptor, ",")[0]
	first = strings.TrimSpace(first)
	first = yarnValue(first)

	if idx := strings.Index(first, "@npm:"); idx != -1 {
		return first[:idx]
	}

	if strings.HasPrefix(first, "@") {
		if idx := strings.LastIndex(first, "@"); idx > 0 {
			return first[:idx]
		}
		return first
	}

	if idx := strings.LastIndex(first, "@"); idx > 0 {
		return first[:idx]
	}

	return first
}

func yarnValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "\"")
	value = strings.TrimSuffix(value, "\"")
	return value
}
