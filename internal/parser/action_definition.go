package parser

import (
	"strings"

	"github.com/navikt/actographer/internal/models"
	"gopkg.in/yaml.v3"
)

type ActionDefinition struct {
	RunsUsing string
	InnerRefs []models.ActionRef
}

func (d ActionDefinition) IsJavaScriptAction() bool {
	return strings.HasPrefix(d.RunsUsing, "node")
}

func ParseActionDefinition(content []byte) (ActionDefinition, error) {
	var action struct {
		Runs struct {
			Using string `yaml:"using"`
			Steps []step `yaml:"steps"`
		} `yaml:"runs"`
	}

	if err := yaml.Unmarshal(content, &action); err != nil {
		return ActionDefinition{}, err
	}

	def := ActionDefinition{
		RunsUsing: action.Runs.Using,
	}

	if action.Runs.Using != "composite" {
		return def, nil
	}

	for _, s := range action.Runs.Steps {
		if s.Uses == "" {
			continue
		}
		def.InnerRefs = append(def.InnerRefs, ParseActionRef(s.Uses))
	}

	return def, nil
}
