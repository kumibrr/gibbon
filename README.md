# gibbon

Feature worktrees for multirepo workspaces.

You have a directory with dozens of related git repositories. Feature work
spans several of them at once. gibbon lays the workspace out so each feature
is one directory that mirrors the repo tree, with every repo inside it being a
`git worktree` of a primary clone kept under `base/`:

```
workspace/
  .gibbon/                 # config + feature metadata
  base/                    # primary clones on their base branch
    api/users-service/
    api/billing/
    web/
  payments-v2/             # a feature
    api/users-service/     # worktree on branch payments-v2
    api/billing/
```

Group folders like `api/` are just organisation; gibbon mirrors them.

## Install

Requires `git` on PATH.

### From a release

Download the archive for your platform from the
[releases page](https://github.com/kumibrr/gibbon/releases), verify it against
`checksums.txt`, and put the binary on your PATH. Builds are published for
Windows, Linux and macOS on amd64 and arm64. Check with `gibbon --version`.

### From source

Go is pinned in `.mise.toml`.

```sh
mise install
make install            # go install ./cmd/gibbon
make dist               # cross-compile every platform into dist/
```

### Releasing

Push a tag like `v1.2.0`. The `Release` workflow runs the tests, builds every
platform, and publishes a GitHub release with archives and checksums. The `CI`
workflow runs vet, tests and gofmt on Windows, Linux and macOS for every push
and pull request.

Add the shell wrapper so `feat -c`, `feat switch` and `feat prune` can `cd`:

```sh
eval "$(gibbon shell-init bash)"   # or zsh; on Windows use Git Bash
```

Without the wrapper every command still works; `feat switch` prints the path.
There is no PowerShell or cmd wrapper yet.

## Commands

| Command | What it does |
|---|---|
| `gibbon init [DIR] [--base-branch B]` | One-time migration. Moves every repo under `base/`, records base branches, leaves non-git files where they are. Refuses if `.gibbon/` exists or `base/` is non-empty. Legacy `.worktrees/` inside repos keep working but are not managed. |
| `gibbon feat` | List features with repo and dirty counts. |
| `gibbon feat -c NAME [--from OTHER] [--branch-template T]` | Create a feature (and cd into it). `--from` adds the same repos as another feature, branched from base. |
| `gibbon feat switch NAME` | cd to a feature, or to `base`. |
| `gibbon feat prune [NAME] [--force]` | Remove a feature: worktrees, branches, directory, metadata. Refuses on uncommitted, unpushed or unmerged work. |
| `gibbon add REPO... [--all] [--branch B] [--feature F]` | Add repos to the current feature. `REPO` is an id (`api/users`), a unique bare name (`users`), a glob (`api/*`) or a group (`api/`). Reuses a local branch, tracks a remote one, or branches from base. |
| `gibbon rm REPO... [--delete-branch] [--force]` | Remove repos from the feature. Keeps branches unless `--delete-branch`. |
| `gibbon status [--all] [--json]` | Per repo: branch, ahead/behind base and upstream, dirty, drift flags. |
| `gibbon sync [--prune]` | Fetch every base clone; fast-forward base branches that are clean and checked out. |
| `gibbon doctor [--fix]` | Audit for orphan branches, stale or broken worktrees, legacy `.worktrees/`, missing metadata, stale config, dirty base clones. `--fix` applies only non-destructive repairs. |
| `gibbon shell-init bash\|zsh` | Print the shell wrapper. |

Global: `-j N` sets parallelism (default from config, 8). Cross-repo commands
run every repo, print a table, and exit non-zero if any repo failed.

## Configuration

`.gibbon/config.toml`:

```toml
branch_template = "{feature}"   # e.g. "feat/{feature}" for namespaced branches
workers = 8

[repos."api/users-service"]
base_branch = "develop"          # detected at init; edit by hand to override
```

Everything else is derived from the filesystem. Drop a new clone into `base/`
and it is immediately available to `add`. A repo is in a feature iff
`<feature>/<repo>` is a registered worktree of `base/<repo>`.

Feature names cannot contain `/`, start with `.`, or be `base`. Use the
branch template for namespaced branch names.

## Development

```sh
make test    # real git in temp dirs; no mocks
make vet
```

Design: `docs/superpowers/specs/2026-09-14-gibbon-design.md`.
