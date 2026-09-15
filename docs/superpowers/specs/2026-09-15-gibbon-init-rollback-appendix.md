# Appendix: init failure rollback (2026-09-15)

Supersedes the "moved / not moved" behavior described under `gibbon init`
and in Error handling in
[2026-09-14-gibbon-design.md](2026-09-14-gibbon-design.md).

- Previously, if a move failed partway through `init`, repos already moved
  stayed under `base/` and the caller had to move them back and remove
  `base/` manually.
- Now, if any step after the first repo move fails — a later move, or
  writing `config.toml` / creating `.gibbon/features` — Init rolls back
  instead of leaving a partially migrated workspace.
- Rollback moves every already-moved repo back to its original path
  (recreating any group folder that was pruned), re-runs `git worktree
  repair` against the restored location, then removes `base/` if this run
  created it fresh (or just the subdirectories it created, if `base/`
  pre-existed empty), and removes `.gibbon/` if this run created it.
- The report reflects this: `Moved` is emptied and `NotMoved` lists every
  discovered repo.
- Rollback is best-effort: if a specific repo can't be restored (e.g. a
  permissions problem), it's named explicitly in the returned error so only
  that repo needs manual attention, instead of the whole workspace.
- Implementation: `internal/ops/init.go` (`rollbackInit`), covered by
  `TestInitRollsBackOnMidLoopFailure` in `internal/ops/init_test.go`.
