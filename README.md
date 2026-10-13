# actographer

Ever wondered what lies under the tip of the iceberg that is `uses: <namespace>/action@<ref>` in your workflow? Actographer can help!

Meant as a tool for GitHub Actions workflow dependency indexing, you can point it at a repo (or an org) and it will find workflow files, traverse and build a dependency tree including both direct and transitive dependencies, and then output a structured inventory for audit and analysis. 

## Install

Install from GitHub:

```sh
go install github.com/navikt/actographer/cmd/actographer@latest
```

Build locally:

```sh
make build
```

Run the compiled binary:

```sh
./actographer --help
```

Or run without installing:

```sh
go run ./cmd/actographer/
```

## Usage

Configuration is via command-line flags. For security, `GITHUB_TOKEN` stays in the environment instead of being accepted as a plain flag.

Set `GITHUB_TOKEN` in your shell or secret manager before running the examples below.

```sh
actographer --org myorg
```

The intended publish target is **`github.com/navikt/actographer`**.

### Scan a full org

```sh
actographer --org nais
```

### Scan specific repos

```sh
actographer --repo nais/docker-build-push --repo nais/deploy
```

### Publisher mode — inventory actions your org publishes

```sh
actographer --mode publisher --org nais
```

## Configuration

| Flag / env | Required | Default | Description |
|---|---|---|---|
| `GITHUB_TOKEN` | Yes | — | GitHub personal access token (needs `repo` read scope) |
| `--org` | Yes* | — | GitHub org to scan. Required unless one or more `--repo` flags are set |
| `--repo` | Yes* | — | Repository to scan in `owner/repo` form. Repeat the flag to scan multiple repos |
| `--mode` | No | `consumer` | Scan mode: `consumer` or `publisher` |
| `--fetch-sbom` | No | `false` | Fetch JavaScript action package inventories from supported lockfiles at the scanned ref |
| `--output-path` | No | `output` | Directory to write the inventory JSON file to |
| `--parallelism` | No | `5` | Number of repos processed concurrently |
| `--debug` | No | `false` | Enable verbose debug logging |

Notes:

- Provide exactly one of `--org` or `--repo`.
- `GITHUB_TOKEN` is intentionally env-only so it does not leak via shell history or process lists.
- Use `actographer --help` to see the full flag help text.

## Modes

### Consumer mode (default)

Scans workflows across all repos in an org (or a list of repos) and maps out every `uses:` reference. Each direct workflow dependency is emitted as the root of a fully resolved dependency tree under the job where it appears.

Output file: `output/inventory-<timestamp>.json`

Top-level fields:

- `org` — org name
- `snapshot_date` — RFC3339 timestamp
- `summary` — aggregate stats (repo counts, dep counts, pinned/unpinned breakdown, top actions, transitive GitHub Action counts, and known transitive dependency counts when package inventories are available)
- `repos[]` — per-repo data including workflows and jobs
- `repos[].workflows[].jobs[].dependencies[]` — depth-first dependency trees rooted at each direct `uses:` occurrence
- `repos[].workflows[].jobs[].dependencies[].sbom` — only present when `--fetch-sbom` is enabled and a JavaScript action lockfile inventory was found (or lockfile absence was recorded)

### Publisher mode (`--mode publisher`)

Scans all repos for `action.yml`/`action.yaml` at HEAD to detect which repos are published actions. For each detected action repo, fetches every published tag and resolves the full transitive dependency tree at that tag.

Output file: `output/inventory-<timestamp>.json`

Top-level fields:

- `published_actions[]` — per-action data with `repo`, `archived`, and `versions[]`
- `versions[]` — each entry has a `tag` and `deps[]` (full transitive dep tree)
- `publisher_summary` — aggregate stats

## Output

Both modes write a timestamped JSON file to the output directory (default: `output/`) named `inventory-<timestamp>.json`, plus a sibling human-readable tree view at `inventory-<timestamp>.tree.txt`. A stable `inventory.json` symlink or copy is **not** created automatically; each run produces new files.

Pick the latest generated file like this:

```sh
FILE="$(ls -t output/inventory-*.json | head -1)"
TREE="${FILE%.json}.tree.txt"
```

Then inspect common fields with `jq` or open the tree view directly:

```sh
# Consumer mode summary
jq '.summary' "$FILE"

# Consumer mode repos
jq '.repos[].repo' "$FILE"

# Publisher mode summary
jq '.publisher_summary' "$FILE"

# Human-readable dependency tree
cat "$TREE"
```

The examples above are the supported starting point for inspecting output with `jq` and the generated tree view.

## JavaScript action package inventory

When `--fetch-sbom` is enabled, the resolver will inspect JavaScript actions (`runs.using: node*`) at the exact scanned ref and try to derive a package inventory from supported Node lockfiles.

- The inventory is **source-derived**, not pulled from GitHub's repository SBOM export API.
- It is **ref-specific** for the scanned action ref.
- It currently covers **`package-lock.json`**, **`npm-shrinkwrap.json`**, **`pnpm-lock.yaml`**, and **`yarn.lock`**.
- If no lockfile is found, the action node records that fact instead of inventing unresolved package versions from `package.json`.
- `summary.total_transitive_github_actions` is the count of transitive **GitHub Actions** only.
- `summary.total_known_transitive_deps` adds in resolved package dependencies discovered from action package inventories.

Example:

```sh
actographer --org nais --fetch-sbom
```

## Dependency types

| Type | Example | Transitive resolution |
|---|---|---|
| Action | `actions/checkout@v4` | Yes, if composite |
| Reusable workflow | `org/repo/.github/workflows/ci.yml@main` | Yes |
| Local action | `./actions/my-action` | Yes, if composite |
| Docker action | `docker://alpine:3.18` | No |

## SHA pinning

Each action reference is classified as pinned (`pinned: true`) if the ref is a 40-character hex SHA. Ratchet-style comments (e.g. `# v4`) are preserved in the output.
