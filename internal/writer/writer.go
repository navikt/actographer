package writer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/navikt/actographer/internal/models"
)

type treeNode struct {
	label    string
	children []treeNode
}

func WriteJSON(inventory models.OrgInventory, outputPath string) error {
	if err := ensureOutputDir(outputPath); err != nil {
		return err
	}

	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling inventory: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("writing output file: %w", err)
	}

	return nil
}

func WriteTree(inventory models.OrgInventory, outputPath string) error {
	if err := ensureOutputDir(outputPath); err != nil {
		return err
	}

	if err := os.WriteFile(outputPath, []byte(RenderTree(inventory)), 0o644); err != nil {
		return fmt.Errorf("writing tree output file: %w", err)
	}

	return nil
}

func RenderTree(inventory models.OrgInventory) string {
	var b strings.Builder

	renderTreeNode(&b, treeNode{label: ".", children: buildTreeRoots(inventory)}, "", true, true)

	return b.String()
}

func buildTreeRoots(inventory models.OrgInventory) []treeNode {
	var roots []treeNode

	if inventory.Org != "" {
		roots = append(roots, treeNode{label: fmt.Sprintf("[Org] %s", inventory.Org)})
	}

	roots = append(roots, treeNode{label: fmt.Sprintf("[Snapshot] %s", inventory.SnapshotDate)})

	if inventory.Summary != nil {
		summaryChildren := []treeNode{
			{label: fmt.Sprintf("[Repos scanned] %d", inventory.Summary.ReposScanned)},
			{label: fmt.Sprintf("[Repos with workflows] %d", inventory.Summary.ReposWithWorkflows)},
			{label: fmt.Sprintf("[Total workflows] %d", inventory.Summary.TotalWorkflows)},
			{label: fmt.Sprintf("[Total direct deps] %d", inventory.Summary.TotalDirectDeps)},
			{label: fmt.Sprintf("[Total transitive GitHub Actions] %d", inventory.Summary.TotalTransitiveGitHubActions)},
		}
		if inventory.Summary.TotalKnownTransitiveDeps != nil {
			summaryChildren = append(summaryChildren, treeNode{label: fmt.Sprintf("[Total known transitive deps] %d", *inventory.Summary.TotalKnownTransitiveDeps)})
		}
		roots = append(roots, treeNode{
			label:    "[Summary]",
			children: summaryChildren,
		})
	}

	if inventory.PublisherSummary != nil {
		roots = append(roots, treeNode{
			label: "[Publisher summary]",
			children: []treeNode{
				{label: fmt.Sprintf("[Repos scanned] %d", inventory.PublisherSummary.ReposScanned)},
				{label: fmt.Sprintf("[Actions found] %d", inventory.PublisherSummary.ActionsFound)},
				{label: fmt.Sprintf("[Total versions] %d", inventory.PublisherSummary.TotalVersions)},
				{label: fmt.Sprintf("[Total deps resolved] %d", inventory.PublisherSummary.TotalDepsResolved)},
			},
		})
	}

	for _, repo := range inventory.Repos {
		label := repo.RepoFullName
		if repo.Archived {
			label += " [archived]"
		}

		repoNode := treeNode{label: fmt.Sprintf("[Repo] %s", label)}
		for _, workflow := range repo.Workflows {
			workflowNode := treeNode{label: fmt.Sprintf("[Workflow] %s", workflow.Path)}
			for _, job := range workflow.Jobs {
				jobNode := treeNode{label: fmt.Sprintf("[Job] %s", job.Name)}
				if len(job.Dependencies) == 0 {
					jobNode.children = []treeNode{{label: "[Dependencies] none"}}
				} else {
					jobNode.children = dependencyNodes(job.Dependencies)
				}
				workflowNode.children = append(workflowNode.children, jobNode)
			}
			repoNode.children = append(repoNode.children, workflowNode)
		}
		roots = append(roots, repoNode)
	}

	for _, action := range inventory.PublishedActions {
		label := action.RepoFullName
		if action.Archived {
			label += " [archived]"
		}

		actionNode := treeNode{label: fmt.Sprintf("[Published action] %s", label)}
		for _, version := range action.Versions {
			versionNode := treeNode{label: fmt.Sprintf("[Version] %s", version.Tag)}
			if len(version.Deps) == 0 {
				versionNode.children = []treeNode{{label: "[Dependencies] none"}}
			} else {
				versionNode.children = dependencyNodes(version.Deps)
			}
			actionNode.children = append(actionNode.children, versionNode)
		}
		roots = append(roots, actionNode)
	}

	return roots
}

func dependencyNodes(deps []models.TransitiveDep) []treeNode {
	nodes := make([]treeNode, 0, len(deps))
	for _, dep := range deps {
		node := treeNode{label: fmt.Sprintf("[Dependency] %s", dep.Action.Raw)}
		if len(dep.Deps) > 0 {
			node.children = dependencyNodes(dep.Deps)
		}
		if dep.SBOM != nil {
			node.children = append(node.children, sbomNode(*dep.SBOM))
		}
		nodes = append(nodes, node)
	}

	return nodes
}

func sbomNode(sbom models.ActionSBOM) treeNode {
	label := fmt.Sprintf("[SBOM] %s", sbom.Ecosystem)
	if sbom.Status == "no_lockfile" {
		label += " (no lockfile found)"
	}
	if sbom.SourcePath != "" {
		label += " [" + sbom.SourcePath + "]"
	}

	node := treeNode{label: label}
	for _, pkg := range sbom.Packages {
		node.children = append(node.children, treeNode{
			label: fmt.Sprintf("[Package] %s@%s", pkg.Name, pkg.Version),
		})
	}

	return node
}

func renderTreeNode(b *strings.Builder, node treeNode, prefix string, isLast bool, isRoot bool) {
	if isRoot {
		writeLine(b, "%s", node.label)
	} else {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		writeLine(b, "%s%s%s", prefix, connector, node.label)
	}

	childPrefix := prefix
	if !isRoot {
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}

	for i, child := range node.children {
		renderTreeNode(b, child, childPrefix, i == len(node.children)-1, false)
	}
}

func writeLine(b *strings.Builder, format string, args ...any) {
	b.WriteString(fmt.Sprintf(format, args...))
	b.WriteByte('\n')
}

func ensureOutputDir(outputPath string) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	return nil
}
