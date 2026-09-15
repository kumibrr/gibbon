# Progress spinner for mutating commands (2026-09-15)

Extends the rendering rules in
[2026-09-14-gibbon-design.md](2026-09-14-gibbon-design.md). Applies to the
commands that change the workspace: `init`, `feat -c` (via add), `add`,
`rm`, `sync`, and `feat prune` (execute phase). Read-only commands
(`status`, `feat list`, `doctor`, prune's check phase) are unchanged.

## Problem

Ops run to completion and only then does the CLI render a table. During a
long `init` or `sync` over many repos the user sees nothing, and warnings
are only visible at the end, detached from when they happened.

## Behaviour

When stdout is a terminal and `--json` is not set:

- A single spinner line shows the repos currently being worked on, in the
  order work started:

  ```
  [  = ] api/payments, web (+3 more)
  ```

  The spinner is a bouncing bar, five cells wide, frames
  `[=   ]`, `[ =  ]`, `[  = ]`, `[   =]`, `[  = ]`, `[ =  ]` at ~120 ms.
  At most three repo names are listed; the rest are summarised as
  `(+N more)`. The line is redrawn in place using `\r` and erase-line
  (`\x1b[2K`), never wrapping past the first line because names are
  truncated to fit the terminal width when it can be determined (80 if
  not).

- Warnings and errors print as permanent lines above the spinner, in the
  order they occur, each prefixed with a fixed-width marker:

  ```
  warn  api/users: has uncommitted changes
  fail  api/orders: rename: permission denied
  ```

  Markers are `warn` and `fail`, padded to 6 columns, coloured yellow and
  red respectively via the existing `output.Colour`. There is no `ok`
  marker: clean lines are printed unprefixed.

- A repo that finishes cleanly prints `api/users moved` only when the
  operation covers 4 or fewer repos. For 5 or more, clean completions are
  silent and one summary line prints after the spinner stops:

  ```
  12 repos moved
  fail  10 of 12 repos moved, 2 failed
  ```

  The summary line carries the `fail` marker if any repo failed, `warn` if
  none failed but some warned, and no marker otherwise. The verb is the most common
  action word among the results (`moved`, `added`, `removed`, `fetched`,
  ...); when actions differ, the line reads `12 repos done`.

- The final REPO/ACTION/DETAIL table is **not** printed in this mode; the
  streamed lines replace it. Trailing messages such as
  `Initialised workspace with N repos...`, `Created feature X at ...`,
  `Pruned feature X` and the `cd` hint are kept.

- The spinner line is erased when the operation ends, so the last line the
  user sees is a permanent one.

When stdout is not a terminal, or `--json` is set: no spinner, no streamed
lines, output is identical to today (table or JSON). Existing CLI tests,
which run the binary with a pipe, therefore remain valid.

## Ordering guarantee

Output is chronological: a warning emitted while repo B is in flight prints
before B's completion line. Parallel workers (`pool.Map`) call the reporter
concurrently; the spinner serialises all writes with a mutex, so lines never
interleave mid-line.

## Architecture

### `ops.Progress` (internal/ops/result.go)

```go
// Progress receives events while an operation runs. Implementations must
// be safe for concurrent use. A nil Progress is valid and means "no-op".
type Progress interface {
    Plan(total int)      // number of repos the operation will touch
    Start(repo string)   // work on repo begins
    Warn(msg string)     // an operation-level warning, not tied to a Result
    Finish(r Result)     // work on r.Repo ended; r carries error/warnings
}
```

A package-private `progress` wrapper makes nil-safe calls so ops code never
checks for nil. Ops call `Plan` once they know the repo set, `Start`
immediately before working on a repo, and `Finish` with the same `Result`
they return. Nothing in ops prints; the rule from the original design
holds.

Signatures:

- `InitOptions.Progress`, `AddOptions.Progress`, `RmOptions.Progress`,
  `SyncOptions.Progress` (new fields).
- `CreateFeature` passes `o.Progress` through to `Add`.
- `ExecutePrune(ws, plan, force, workers int, p Progress)` (new parameter).

Init specifics: `Plan` is called after discovery and validation succeed.
The "has uncommitted changes" and "on X, base branch is Y" warnings are
emitted via `Warn` as each repo is checked; they are still appended to
`InitReport.Warnings` for JSON and non-TTY output. Per-repo warnings
(worktree repair, base detection) travel in the `Result` given to `Finish`.
If Init rolls back, nothing extra is emitted; the returned error is printed
by `Run` as today.

### `internal/progress` (new package)

```go
type Spinner struct { /* mu, w, inflight []string, frame int, stop chan */ }

func New(w io.Writer, width int) *Spinner   // width: terminal columns, 0 = 80
func (s *Spinner) Start()                   // begin the ticker goroutine
func (s *Spinner) Begin(name string)
func (s *Spinner) End(name string)
func (s *Spinner) Log(line string)          // permanent line above the spinner
func (s *Spinner) Stop()                    // erase spinner line, stop ticker
```

`Log`, `Begin`, `End`, and the ticker all take the mutex, erase the current
line, write, and redraw. `Stop` is idempotent. The ticker interval is a
field so tests can drive redraws by calling an unexported `tick()` instead.
The package knows nothing about repos or gibbon; it is a generic
"status line plus log lines" widget.

### CLI reporter (internal/cli/progress.go)

`type reporter struct` implements `ops.Progress` on top of a `*progress.Spinner`
and `output.Colour`. It counts total/failed/warned, remembers action
words, and formats lines per the Behaviour section. `finish()` stops the
spinner and prints the summary line when total > 4.

`app` gains `func (a *app) newProgress() *reporter` returning nil when
stdout is not a terminal. Commands do:

```go
p := a.newProgress()            // nil unless TTY && !asJSON
rs := ops.Add(ws, feat, repos, ops.AddOptions{..., Progress: p})
return a.printResults(rs, asJSON, p)
```

`printResults` gains the reporter parameter: when non-nil it calls
`p.finish()` and skips the table, but still returns `errFailed` when any
result failed. `initCmd` follows the same pattern for its own rendering.

Terminal detection is a new `output.IsTerminal(io.Writer) bool` (the
`os.File` + `ModeCharDevice` check currently inside `DetectColour`, which
is refactored to use it). Terminal width comes from `golang.org/x/term`
(`term.GetSize`), added as a dependency; this also gives a portable
`IsTerminal` on Windows.

## Error handling

- An operation that fails before `Plan` (e.g. init refusing to run) emits
  nothing; the error path is unchanged.
- Writes to the spinner ignore write errors; progress must never fail an
  operation.
- `Stop` is deferred in each command right after creating the reporter, so
  a panic or early return never leaves the cursor on a half-drawn line.

## Testing

- `internal/progress`: table tests over a `bytes.Buffer` with the ticker
  disabled, asserting exact byte sequences for Begin/Log/End/Stop and the
  `(+N more)` and width truncation rules. A concurrency test runs Begin/
  Log/End from many goroutines under `-race`.
- `internal/cli`: unit tests for `reporter` formatting using a spinner over
  a buffer: the 4-repo threshold, marker selection, summary verb, and
  summary marker precedence.
- `internal/ops`: a `recordingProgress` test helper; tests assert that
  `Init`, `Add`, `Remove`, `Sync` call `Plan(n)` once, `Start`/`Finish`
  once per repo with matching repo ids, and that Init emits the dirty
  warning via `Warn` between that repo's `Start` and `Finish`.
- Existing CLI binary tests run through a pipe and are unaffected.
