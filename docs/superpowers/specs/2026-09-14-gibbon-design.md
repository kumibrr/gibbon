# Gibbon: multirepo feature worktree manager

Date: 2026-09-14
Status: approved

## Problem

A workspace holds 80+ related git repositories. Feature work today lives in
`.worktrees/` inside each repo, so a single feature is scattered across dozens
of hidden directories and there is no view of "which repos does feature X
touch". Gibbon inverts the layout: features become top-level directories that
mirror the workspace, and each repo inside a feature is a `git worktree` of a
primary clone kept under `base/`.

## Layout and vocabulary

```
workspace/
  .gibbon/
    config.toml              # workspace defaults + per-repo overrides
    features/<name>.toml     # per-feature metadata (branch template, created_at)
  base/                      # primary clones, checked out on their base branch
    api/users-service/       # repo id: "api/users-service"
    api/billing/
    web/
  payments-v2/               # a feature
    api/users-service/       # git worktree of base/api/users-service
    api/billing/
```

- **Workspace**: the directory containing `.gibbon/`. Located by walking up
  from the current directory, like git does. No global registry in v1.
- **Repo**: any directory under `base/` containing `.git`. Its **id** is the
  path relative to `base/` (`api/users-service`). Discovery walks `base/`
  recursively, skips dot-directories, and never descends into a repo. No depth
  limit.
- **Group folder**: a non-repo directory on the way down from `base/`. Purely
  organisational. Features mirror group folders.
- **Feature**: a directory directly under the workspace root that is not
  `base` and not dot-prefixed.
- **Membership** is derived, never stored: repo R is in feature F iff
  `F/<R>` exists and is a registered worktree of `base/<R>`.
- **Stored state** is only what cannot be derived:
  - `config.toml`: default branch template, worker count, and a table of
    per-repo overrides keyed by repo id (currently `base_branch`).
  - `features/<name>.toml`: `branch_template` override and `created_at`.
- **Dirty**: uncommitted changes to tracked files or untracked non-ignored
  files. Ignored files do not count.

## Rules

- **Feature names**: non-empty, no `/`, not `base`, not dot-prefixed, and a
  valid git ref component (no spaces, `..`, `~`, `^`, `:`, `?`, `*`, `[`,
  `\`, control characters, or trailing `.lock`).
- **Branch template**: default `{feature}`. Set per workspace in config,
  overridable per feature at creation. Slashes are allowed here, so
  `feat/{feature}` gives namespaced branches with flat directories.
- **Base branch detection** per repo, in order: `refs/remotes/origin/HEAD`,
  local `main`, local `master`, currently checked-out branch. Detected lazily
  for repos with no config entry and persisted to config on first use.
- **Repo resolution** for `add`/`rm`: a full id; a bare directory name if
  unique across the workspace (ambiguity is an error listing candidates); a
  glob against ids; or `group/` meaning every repo under that group.
- **Reserved**: `base`, `.gibbon`.

## Command surface

Global flags: `-j N` worker pool size for cross-repo git operations
(default 8). `--json` on list-type commands. Colour only when stdout is a TTY.
Any cross-repo command exits non-zero if any repo failed, after completing
the others and printing the full result table.

### `gibbon init [--base-branch B]`

One-shot migration of an existing flat or nested workspace.

Refuses if: `.gibbon/` exists; `base/` exists and is non-empty; any
discovered repo is named `base`. Validates the full move plan before the
first move.

Then, for each repo found by walking the workspace root (same discovery rules
as `base/`): moves it to `base/<relpath>`, creating group folders; runs
`git worktree repair` in it so any legacy `.worktrees/` entries keep working
(gibbon otherwise ignores them); removes group folders left empty. Detects
and records each repo's base branch (`--base-branch` sets all). Writes
`config.toml`. Prints a table of skipped non-git items and of repos that are
dirty or not on their base branch. Moves are sequential; on a failed move it
stops and prints moved / not moved.

### `gibbon feat`

Lists features with repo count and dirty count. `--json`.

### `gibbon feat -c NAME [--from OTHER] [--branch-template T]`

Creates `NAME/` and `.gibbon/features/NAME.toml`. `--from` adds `OTHER`'s
repo set, branched from base (not from `OTHER`'s branches). Shell wrapper
cds into the new feature. Accepting repos as positional args is deferred.

### `gibbon feat switch NAME`

Prints the feature path. Shell wrapper cds. `base` is a valid target.

### `gibbon feat prune [NAME] [--force]`

Target is `NAME`, or the feature containing cwd. Per repo, refuses unless
`--force` when: dirty; branch has commits not on its upstream; branch not
merged into the repo's base branch. If nothing blocks (or forced): removes
each worktree, deletes each feature branch, deletes the directory and the
metadata file. Prints the plan table before acting and does nothing if any
repo is blocked. Shell wrapper cds to workspace root if cwd was inside the
pruned feature.

### `gibbon add REPO... [--all] [--branch B] [--feature F]`

Target feature from cwd or `--feature`. Resolves each argument per the
resolution rules. Per repo: `git fetch` in the base clone; pick the branch
(`--branch`, else the feature's template); if it exists locally, use it; if
it exists only on `origin`, create a tracking branch; otherwise branch from
the base branch. Creates group folders, then `git worktree add`. Per-repo
failures are reported and do not abort the rest. Already-present repos are
reported as no-ops.

### `gibbon rm REPO... [--delete-branch] [--force] [--feature F]`

Removes the worktree and keeps the branch. Refuses on dirty unless `--force`.
`--delete-branch` additionally applies prune's checks (unpushed, unmerged)
and deletes the branch. Removes group folders left empty.

### `gibbon status [--all] [--json]`

Per repo in the current feature (or every feature with `--all`): branch,
ahead/behind base, ahead/behind upstream, dirty, and drift flags: directory
present but not a registered worktree; registered worktree whose directory is
missing.

### `gibbon sync [--prune] [-j N]`

Fetches every base clone (`--prune` prunes remote-tracking refs). Fast-
forwards the base branch only where the clone is clean and checked out on it;
reports others as skipped with the reason. Never touches feature branches.

### `gibbon doctor [--fix]`

Checks:
1. legacy `.worktrees/` inside base repos;
2. worktree registrations whose directory is gone;
3. directories under a feature that are not registered worktrees;
4. feature repos with no counterpart in `base/`;
5. branches matching a feature template with no live worktree (orphans);
6. feature metadata without a directory, and directories without metadata;
7. config entries for repos that no longer exist;
8. base clones that are dirty or not on their base branch.

`--fix` performs only non-destructive repairs: `git worktree prune`,
`git worktree repair`, creating missing metadata, dropping stale config
entries. It never deletes branches or directories; for those it prints the
command a human would run.

### `gibbon shell-init bash|zsh`

Emits a wrapper function `gibbon` that intercepts `feat -c`, `feat switch`
and `feat prune` to run `cd` on the path the binary prints, and forwards
everything else. Install with `eval "$(gibbon shell-init bash)"`. Without the
wrapper every command still works; `switch` prints the path so
`cd "$(gibbon feat switch x)"` is valid.

## Architecture

Go module `github.com/kumibrr/gibbon`. cobra for the CLI. Git operations
shell out to the `git` binary. Go version pinned in `.mise.toml`.

```
cmd/gibbon/main.go          # entry
internal/cli/               # one file per cobra command; flags and rendering only
internal/workspace/         # locate workspace, load/save config and feature metadata
internal/discover/          # walk base/ and features; Repo and Feature models
internal/git/               # thin typed wrapper around git subprocess calls
internal/ops/               # init, add, rm, prune, sync, doctor as pure operations
internal/pool/              # bounded worker pool with ordered result collection
internal/output/            # table and JSON renderers
internal/shell/             # embedded wrapper scripts
```

`ops` never prints and never reads flags. It receives a resolved request and
returns per-repo results (`Repo`, `Action`, `Err`, `Warnings`). `cli` renders
them. Cross-repo git work goes through `pool`; init's directory moves stay
sequential.

## Error handling

- Each command validates fully before mutating anything, then runs to
  completion across repos and reports all failures together.
- Refusals name the blocker per repo and the flag that overrides it.
- Init is the only command that moves directories and stops on the first
  failed move with a moved / not moved table.

## Testing

- Unit tests: feature name validation, branch templating, repo id resolution,
  base branch detection order, config round-trip.
- Integration tests: build real repos with a bare "origin" in `t.TempDir()`,
  run the ops layer, assert on the filesystem and `git worktree list`. Cover
  init on a nested tree with non-git items and legacy `.worktrees`; add with
  local, remote-only and absent branches; prune refusals and success; rm
  keeping and deleting branches; sync fast-forward and skip; every doctor
  check with and without `--fix`.
- Shell wrapper: run bash and zsh with the emitted function and check `pwd`
  after `feat switch`.

## Out of scope for v1

Global workspace registry; `sync --rebase` of feature branches; fish; a
`config` command; migrating legacy `.worktrees`; goreleaser packaging;
positional repos on `feat -c`.
