package parser

import (
	"fmt"
	"path"

	"github.com/navikt/actographer/internal/models"
)

func ParseNodeLockfile(lockfilePath string, content []byte) (string, []models.SBOMPackage, error) {
	switch path.Base(lockfilePath) {
	case "package-lock.json", "npm-shrinkwrap.json":
		packages, err := ParseNPMLockfile(content)
		return "npm", packages, err
	case "yarn.lock":
		packages, err := ParseYarnLockfile(content)
		return "yarn", packages, err
	case "pnpm-lock.yaml":
		packages, err := ParsePNPMLockfile(content)
		return "pnpm", packages, err
	default:
		return "", nil, fmt.Errorf("unsupported lockfile: %s", lockfilePath)
	}
}
