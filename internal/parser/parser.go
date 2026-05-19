package parser

import (
	"regexp"
	"strings"

	"github.com/navikt/actographer/internal/models"
	"gopkg.in/yaml.v3"
)

type workflow struct {
	Jobs map[string]job `yaml:"jobs"`
}

type job struct {
	Name  string `yaml:"name"`
	Uses  string `yaml:"uses"`
	Steps []step `yaml:"steps"`
}

type step struct {
	Uses string `yaml:"uses"`
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func ParseWorkflow(content []byte, repoFullName, path string) (models.Workflow, error) {
	var wf workflow
	if err := yaml.Unmarshal(content, &wf); err != nil {
		return models.Workflow{}, err
	}

	result := models.Workflow{
		RepoFullName: repoFullName,
		Path:         path,
	}

	for jobID, j := range wf.Jobs {
		name := j.Name
		if name == "" {
			name = jobID
		}

		wj := models.WorkflowJob{Name: name}

		if j.Uses != "" {
			ref := ParseActionRef(j.Uses)
			wj.Uses = &ref
		}

		for _, s := range j.Steps {
			if s.Uses == "" {
				continue
			}
			ref := ParseActionRef(s.Uses)
			wj.StepUses = append(wj.StepUses, ref)
		}

		result.Jobs = append(result.Jobs, wj)
	}

	return result, nil
}

func ParseActionRef(raw string) models.ActionRef {
	ref := models.ActionRef{Raw: raw}

	comment := ""
	if idx := strings.Index(raw, " "); idx != -1 {
		comment = strings.TrimSpace(raw[idx+1:])
		raw = raw[:idx]
		ref.Comment = comment
	}

	if strings.HasPrefix(raw, "docker://") {
		ref.Type = models.ActionRefDocker
		ref.Path = strings.TrimPrefix(raw, "docker://")
		return ref
	}

	if strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		ref.Type = models.ActionRefLocal
		ref.Path = raw
		return ref
	}

	parts := strings.SplitN(raw, "@", 2)
	actionPath := parts[0]
	if len(parts) == 2 {
		ref.Ref = parts[1]
		ref.Pinned = shaPattern.MatchString(parts[1])
	}

	if isReusableWorkflow(actionPath) {
		ref.Type = models.ActionRefWorkflow
	} else {
		ref.Type = models.ActionRefAction
	}

	segments := strings.SplitN(actionPath, "/", 3)
	if len(segments) >= 2 {
		ref.Owner = segments[0]
		ref.Repo = segments[1]
	}
	if len(segments) == 3 {
		ref.Path = segments[2]
	}

	return ref
}

func isReusableWorkflow(actionPath string) bool {
	return strings.Contains(actionPath, "/.github/workflows/")
}

func ParseCompositeAction(content []byte) ([]models.ActionRef, error) {
	def, err := ParseActionDefinition(content)
	if err != nil {
		return nil, err
	}

	if def.RunsUsing != "composite" {
		return nil, nil
	}

	return def.InnerRefs, nil
}
