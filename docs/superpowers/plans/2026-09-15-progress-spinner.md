# Progress Spinner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a live bouncing-bar spinner with the in-flight repos while mutating commands run, and stream warnings and failures chronologically above it, without changing piped or `--json` output.

**Architecture:** Ops gain an optional `ops.Progress` callback (`Plan`/`Start`/`Warn`/`Finish`) invoked as work happens; ops still never print. A generic `internal/progress.Spinner` widget owns the terminal status line and serialises writes with a mutex. A small `reporter` in `internal/cli` implements `ops.Progress` on top of the spinner and replaces the summary table when stdout is a terminal.

**Tech Stack:** Go 1.27, cobra (existing), new dependency `golang.org/x/term` for terminal detection and width.

**Spec:** `docs/superpowers/specs/2026-09-15-gibbon-progress-spinner-design.md`

## Global Constraints

- No emojis anywhere in output. Markers are the words `warn` and `fail` padded to 6 columns; clean lines have no marker.
- Spinner frames, exactly: `[=   ]`, `[ =  ]`, `[  = ]`, `[   =]`, `[  = ]`, `[ =  ]` at 120 ms.
- At most 3 repo names on the spinner line, then `(+N more)`. Line truncated to terminal width (80 when unknown).
- Per-repo clean completion lines only when total repos ≤ 4. Otherwise one summary line after the spinner.
- Output when stdout is not a terminal, or with `--json`, is byte-for-byte what it is today.
- Nothing in `internal/ops` prints. Ops functions stay synchronous and return the same values as today.
- Progress callbacks are invoked from `pool.Map` goroutines: every implementation must be safe for concurrent use.
- Run `gofmt -l -w ./cmd ./internal` and `go vet ./...` before each commit. `go` is available via `mise` (`export PATH="$HOME/.local/share/mise/shims:$PATH"`).
- Commit messages end with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.

---

## File structure

| File | Responsibility |
|---|---|
| `internal/output/output.go` (modify) | `IsTerminal`, `TerminalWidth`; `DetectColour` reuses `IsTerminal`. |
| `internal/output/output_test.go` (create) | Non-file writers are not terminals and have width 0. |
| `internal/progress/spinner.go` (create) | Generic status-line widget: Begin/End/Log/Stop, ticker, redraw, truncation. |
| `internal/progress/spinner_test.go` (create) | Exact byte sequences, name list rules, truncation, race test. |
| `internal/ops/progress.go` (create) | `Progress` interface and nil-safe `notify` wrapper. |
| `internal/ops/fixture_test.go` (modify) | `recProgress` recording helper and `assertProgress` helper. |
| `internal/ops/add.go`, `rm.go`, `sync.go`, `init.go`, `feature.go` (modify) | Emit progress events. |
| `internal/ops/*_test.go` (modify) | Assert events per op. |
| `internal/cli/progress.go` (create) | `reporter` implementing `ops.Progress`; `app.newProgress`. |
| `internal/cli/progress_test.go` (create) | Formatting, threshold, summary rules. |
| `internal/cli/root.go`, `init.go`, `feat.go`, `add_rm.go`, `status_sync_doctor.go` (modify) | Wire reporter into commands. |
| `README.md` (modify) | Document the progress output. |

---

### Task 1: Terminal detection and width in `output`

**Files:**
- Modify: `internal/output/output.go:15-30`
- Create: `internal/output/output_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces: `func IsTerminal(w io.Writer) bool`, `func TerminalWidth(w io.Writer) int` (0 when unknown).

- [ ] **Step 1: Add the dependency**

```bash
export PATH="$HOME/.local/share/mise/shims:$PATH"
go get golang.org/x/term@latest
```

- [ ] **Step 2: Write the failing test**

`internal/output/output_test.go`:

```go
package output

import (
	"bytes"
	"testing"
)

func TestNonFileWriterIsNotTerminal(t *testing.T) {
	var buf bytes.Buffer
	if IsTerminal(&buf) {
		t.Fatal("bytes.Buffer reported as terminal")
	}
	if w := TerminalWidth(&buf); w != 0 {
		t.Fatalf("width = %d, want 0", w)
	}
	if DetectColour(&buf) {
		t.Fatal("colour enabled for bytes.Buffer")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/output/ -run TestNonFileWriterIsNotTerminal`
Expected: FAIL, `undefined: IsTerminal`.

- [ ] **Step 4: Implement**

Replace the `DetectColour` function in `internal/output/output.go` with:

```go
// IsTerminal reports whether w is a character device (an interactive
// terminal). Anything that is not an *os.File is never a terminal.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// TerminalWidth returns the column count of the terminal behind w, or 0
// when w is not a terminal or the size cannot be determined.
func TerminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil || cols <= 0 {
		return 0
	}
	return cols
}

// DetectColour enables colour when w is a terminal and NO_COLOR is unset.
func DetectColour(w io.Writer) Colour {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return Colour(IsTerminal(w))
}
```

Add `"golang.org/x/term"` to the import block.

- [ ] **Step 5: Run tests**

Run: `go test ./... && go vet ./...`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -l -w ./cmd ./internal && go mod tidy
git add go.mod go.sum internal/output
git commit -m "feat(output): add IsTerminal and TerminalWidth via x/term

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: `internal/progress` spinner widget

**Files:**
- Create: `internal/progress/spinner.go`
- Create: `internal/progress/spinner_test.go`

**Interfaces:**
- Produces:
  ```go
  func New(w io.Writer, width int) *Spinner  // width 0 => 80; does NOT start the ticker
  func (s *Spinner) Start()                  // starts the 120 ms ticker goroutine
  func (s *Spinner) Begin(name string)
  func (s *Spinner) End(name string)
  func (s *Spinner) Log(line string)         // permanent line above the status line
  func (s *Spinner) Stop()                   // erase status line, stop ticker; idempotent
  ```
- Redraw protocol: status line is written without a trailing newline. To print anything else, the widget emits `\r\x1b[2K` (only if a status line is on screen), writes the content, then redraws the status line if any name is in flight.

- [ ] **Step 1: Write the failing tests**

`internal/progress/spinner_test.go`:

```go
package progress

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

const clear = "\r\x1b[2K"

func TestBeginLogEndStopSequence(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, 80)
	s.Begin("api/users")
	s.Log("warn  api/users: dirty")
	s.End("api/users")
	s.Stop()
	want := "[=   ] api/users" +
		clear + "warn  api/users: dirty\n" + "[=   ] api/users" +
		clear
	if got := buf.String(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestLogWithNothingInFlightPrintsPlainLine(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, 80)
	s.Log("hello")
	s.Stop()
	if got := buf.String(); got != "hello\n" {
		t.Fatalf("got %q", got)
	}
}

func TestTickAdvancesFrame(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, 80)
	s.Begin("a")
	s.tick()
	s.tick()
	if got := buf.String(); got != "[=   ] a"+clear+"[ =  ] a"+clear+"[  = ] a" {
		t.Fatalf("got %q", got)
	}
}

func TestFramesBounce(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, 80)
	s.Begin("a")
	var seen []string
	for i := 0; i < len(frames); i++ {
		seen = append(seen, s.line())
		s.tick()
	}
	want := []string{"[=   ] a", "[ =  ] a", "[  = ] a", "[   =] a", "[  = ] a", "[ =  ] a"}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("got %v", seen)
	}
	if s.line() != "[=   ] a" {
		t.Fatalf("did not wrap: %q", s.line())
	}
}

func TestNamesListedInStartOrderWithOverflow(t *testing.T) {
	s := New(&bytes.Buffer{}, 80)
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		s.Begin(n)
	}
	if got := s.line(); got != "[=   ] a, b, c (+2 more)" {
		t.Fatalf("got %q", got)
	}
	s.End("b")
	if got := s.line(); got != "[=   ] a, c, d (+1 more)" {
		t.Fatalf("got %q", got)
	}
	s.End("zzz") // unknown name is ignored
	if got := s.line(); got != "[=   ] a, c, d (+1 more)" {
		t.Fatalf("got %q", got)
	}
}

func TestLineTruncatedToWidth(t *testing.T) {
	s := New(&bytes.Buffer{}, 20)
	s.Begin(strings.Repeat("x", 40))
	got := s.line()
	if len(got) != 19 || !strings.HasSuffix(got, "...") {
		t.Fatalf("got %q (len %d)", got, len(got))
	}
}

func TestStopIsIdempotentAndBlocksFurtherDrawing(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, 80)
	s.Begin("a")
	s.Stop()
	s.Stop()
	s.Begin("b")
	s.tick()
	if got := buf.String(); got != "[=   ] a"+clear {
		t.Fatalf("got %q", got)
	}
}

func TestStartStopWithTicker(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, 80)
	s.Start()
	s.Begin("a")
	s.Stop() // must return and not leak the goroutine
	if !strings.HasSuffix(buf.String(), clear) {
		t.Fatalf("status line not cleared: %q", buf.String())
	}
}

func TestConcurrentUse(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, 80)
	s.Start()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n := fmt.Sprint(i)
			s.Begin(n)
			s.Log("line " + n)
			s.End(n)
		}(i)
	}
	wg.Wait()
	s.Stop()
	for i := 0; i < 50; i++ {
		if !strings.Contains(buf.String(), fmt.Sprintf("line %d\n", i)) {
			t.Fatalf("missing line %d", i)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/progress/`
Expected: FAIL to compile, `undefined: New`.

- [ ] **Step 3: Implement**

`internal/progress/spinner.go`:

```go
// Package progress draws a single live status line ("spinner") at the
// bottom of a terminal while permanent log lines scroll above it. It knows
// nothing about the caller's domain.
package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

var frames = []string{"[=   ]", "[ =  ]", "[  = ]", "[   =]", "[  = ]", "[ =  ]"}

const (
	defaultWidth = 80
	interval     = 120 * time.Millisecond
	maxNames     = 3
	clearLine    = "\r\x1b[2K"
)

// Spinner is safe for concurrent use.
type Spinner struct {
	mu       sync.Mutex
	w        io.Writer
	width    int
	inflight []string
	frame    int
	drawn    bool // a status line is currently on screen
	stopped  bool
	done     chan struct{}
	wg       sync.WaitGroup
}

// New creates a spinner writing to w. width is the terminal column count;
// 0 means 80. The ticker is not started; call Start.
func New(w io.Writer, width int) *Spinner {
	if width <= 0 {
		width = defaultWidth
	}
	return &Spinner{w: w, width: width, done: make(chan struct{})}
}

// Start begins animating the status line. Stop must be called afterwards.
func (s *Spinner) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-t.C:
				s.tick()
			}
		}
	}()
}

// Begin adds name to the in-flight list and redraws.
func (s *Spinner) Begin(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.inflight = append(s.inflight, name)
	s.clear()
	s.redraw()
}

// End removes name from the in-flight list and redraws. Unknown names are
// ignored.
func (s *Spinner) End(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	for i, n := range s.inflight {
		if n == name {
			s.inflight = append(s.inflight[:i], s.inflight[i+1:]...)
			break
		}
	}
	s.clear()
	s.redraw()
}

// Log writes a permanent line above the status line.
func (s *Spinner) Log(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clear()
	fmt.Fprintln(s.w, line)
	s.redraw()
}

// Stop erases the status line and stops the ticker. It is idempotent; after
// Stop, Begin/End/tick draw nothing, but Log still prints plain lines.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.clear()
	close(s.done)
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Spinner) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || len(s.inflight) == 0 {
		return
	}
	s.frame = (s.frame + 1) % len(frames)
	s.clear()
	s.redraw()
}

// clear erases the status line if one is on screen. Caller holds mu.
func (s *Spinner) clear() {
	if s.drawn {
		fmt.Fprint(s.w, clearLine)
		s.drawn = false
	}
}

// redraw writes the status line if anything is in flight. Caller holds mu.
func (s *Spinner) redraw() {
	if s.stopped || len(s.inflight) == 0 {
		return
	}
	fmt.Fprint(s.w, s.line())
	s.drawn = true
}

// line renders the current status line, truncated to width-1 columns so
// the cursor never wraps. Caller holds mu (tests call it directly).
func (s *Spinner) line() string {
	names := s.inflight
	extra := 0
	if len(names) > maxNames {
		extra = len(names) - maxNames
		names = names[:maxNames]
	}
	text := frames[s.frame] + " " + strings.Join(names, ", ")
	if extra > 0 {
		text += fmt.Sprintf(" (+%d more)", extra)
	}
	limit := s.width - 1
	if len(text) > limit {
		if limit > 3 {
			text = text[:limit-3] + "..."
		} else {
			text = text[:limit]
		}
	}
	return text
}
```

- [ ] **Step 4: Run tests, including the race detector**

Run: `go test -race ./internal/progress/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l -w ./cmd ./internal
git add internal/progress
git commit -m "feat(progress): add status-line spinner widget

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: `ops.Progress` interface, recording helper, and Add events

**Files:**
- Create: `internal/ops/progress.go`
- Modify: `internal/ops/fixture_test.go` (append helpers)
- Modify: `internal/ops/add.go:15-53`
- Modify: `internal/ops/add_rm_test.go:12-45`

**Interfaces:**
- Produces:
  ```go
  type Progress interface {
      Plan(total int)
      Start(repo string)
      Warn(msg string)
      Finish(r Result)
  }
  type notify struct{ p Progress }   // unexported, nil-safe forwarding
  ```
  `AddOptions.Progress Progress` field.
- Test helper (in `ops_test`): `type recProgress struct` with `events []string` in the forms `plan N`, `start ID`, `warn MSG`, `finish ID`, `finish ID err`; `func assertProgress(t, rec, total int, ids ...string)`.

- [ ] **Step 1: Write the interface**

`internal/ops/progress.go`:

```go
package ops

// Progress receives events while an operation runs. Implementations must be
// safe for concurrent use: operations call it from worker goroutines. A nil
// Progress is valid and means no reporting.
type Progress interface {
	// Plan reports how many repos the operation will touch, once known.
	Plan(total int)
	// Start is called immediately before work on repo begins.
	Start(repo string)
	// Warn reports an operation-level warning not tied to one Result.
	Warn(msg string)
	// Finish is called when work on r.Repo has ended, with the same Result
	// the operation returns for it.
	Finish(r Result)
}

// notify forwards to a Progress, ignoring a nil one.
type notify struct{ p Progress }

func (n notify) Plan(total int)    { if n.p != nil { n.p.Plan(total) } }
func (n notify) Start(repo string) { if n.p != nil { n.p.Start(repo) } }
func (n notify) Warn(msg string)   { if n.p != nil { n.p.Warn(msg) } }
func (n notify) Finish(r Result) Result {
	if n.p != nil {
		n.p.Finish(r)
	}
	return r
}
```

`notify.Finish` returns the Result so ops can write `return pg.Finish(result(...))`.

- [ ] **Step 2: Add the recording helper to `internal/ops/fixture_test.go`**

Append (add `"fmt"`, `"sort"`, `"strings"`, `"sync"` to the imports):

```go
// recProgress records ops.Progress events in call order.
type recProgress struct {
	mu     sync.Mutex
	events []string
}

func (r *recProgress) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}
func (r *recProgress) Plan(n int)         { r.add(fmt.Sprintf("plan %d", n)) }
func (r *recProgress) Start(repo string)  { r.add("start " + repo) }
func (r *recProgress) Warn(msg string)    { r.add("warn " + msg) }
func (r *recProgress) Finish(res ops.Result) {
	s := "finish " + res.Repo
	if res.Err != nil {
		s += " err"
	}
	r.add(s)
}

// index returns the position of the first event with prefix, or -1.
func (r *recProgress) index(prefix string) int {
	for i, e := range r.events {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

// assertProgress checks that exactly one "plan total" was recorded first,
// and that every id has exactly one start followed later by one finish.
func assertProgress(t *testing.T, r *recProgress, total int, ids ...string) {
	t.Helper()
	if len(r.events) == 0 || r.events[0] != fmt.Sprintf("plan %d", total) {
		t.Fatalf("first event should be %q, got %v", fmt.Sprintf("plan %d", total), r.events)
	}
	plans := 0
	for _, e := range r.events {
		if strings.HasPrefix(e, "plan ") {
			plans++
		}
	}
	if plans != 1 {
		t.Fatalf("plan called %d times: %v", plans, r.events)
	}
	sort.Strings(ids)
	for _, id := range ids {
		starts, finishes := 0, 0
		for _, e := range r.events {
			if e == "start "+id {
				starts++
			}
			if e == "finish "+id || e == "finish "+id+" err" {
				finishes++
			}
		}
		if starts != 1 || finishes != 1 {
			t.Fatalf("%s: %d starts, %d finishes: %v", id, starts, finishes, r.events)
		}
		if r.index("start "+id) > r.index("finish "+id) {
			t.Fatalf("%s finished before it started: %v", id, r.events)
		}
	}
}
```

- [ ] **Step 3: Write the failing Add test**

In `internal/ops/add_rm_test.go`, `TestAddBranchSources`, change the `ops.Add` call and add assertions right after `f.mustOK(rs)`:

```go
	rec := &recProgress{}
	rs := ops.Add(f.ws, "pay", f.repos("fresh", "local", "remote"), ops.AddOptions{Workers: 3, Progress: rec})
	f.mustOK(rs)
	assertProgress(t, rec, 3, "fresh", "local", "remote")
```

And in `TestAddFailuresDoNotAbortOthers` (line 61), pass `Progress: rec` the same way and assert the failing repo produced `finish <id> err`. Read the test first to find the failing repo's id; then add:

```go
	if rec.index("finish "+failingID+" err") < 0 {
		t.Fatalf("expected an error finish for %s: %v", failingID, rec.events)
	}
```

where `failingID` is the id the test already treats as failing.

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/ops/ -run 'TestAdd'`
Expected: FAIL, `unknown field Progress in struct literal`.

- [ ] **Step 5: Implement in `internal/ops/add.go`**

Add the field:

```go
type AddOptions struct {
	Branch   string // overrides the feature's branch template for this call
	Workers  int
	Progress Progress // optional; receives per-repo events
}
```

In `Add`, after the branch is resolved and before building `jobs`, add `pg := notify{o.Progress}` and `pg.Plan(len(repos))`. Replace the `pool.Map` body with:

```go
	return pool.Map(jobs, o.Workers, func(j job) Result {
		pg.Start(j.repo.ID)
		if j.err != nil {
			return pg.Finish(result(j.repo.ID, "", j.err))
		}
		return pg.Finish(addOne(ws, feature, j.repo, branch, j.base))
	})
```

The two early-return error paths (`feature does not exist`, branch template error) stay as they are: they happen before `Plan`.

- [ ] **Step 6: Run tests**

Run: `go test -race ./internal/ops/ && go vet ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
gofmt -l -w ./cmd ./internal
git add internal/ops
git commit -m "feat(ops): add Progress callback and emit events from Add

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: Progress events from Remove and Sync

**Files:**
- Modify: `internal/ops/rm.go:16-42`
- Modify: `internal/ops/sync.go:13-35`
- Modify: `internal/ops/add_rm_test.go` (`TestRemoveKeepsBranch`, line 78)
- Modify: `internal/ops/status_sync_test.go` (`TestSync`, line 46)

**Interfaces:**
- Consumes: `Progress`, `notify` from Task 3; `recProgress`, `assertProgress` from Task 3.
- Produces: `RmOptions.Progress Progress`, `SyncOptions.Progress Progress`.

- [ ] **Step 1: Write the failing tests**

In `TestRemoveKeepsBranch`, find the `ops.Remove(...)` call. Change it to pass a recorder and assert. The repo ids are whatever the test already removes; call them `ids` below and substitute the literal list:

```go
	rec := &recProgress{}
	rs := ops.Remove(f.ws, "pay", f.repos(ids...), ops.RmOptions{Workers: 2, Progress: rec})
	f.mustOK(rs)
	assertProgress(t, rec, len(ids), ids...)
```

In `TestSync`, find the `ops.Sync(...)` call and do the same with `ops.SyncOptions{Workers: 2, Progress: rec}` and `assertProgress(t, rec, N, ids...)` where `N` and `ids` are the fixture's repos.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ops/ -run 'TestRemoveKeepsBranch|TestSync'`
Expected: FAIL, `unknown field Progress`.

- [ ] **Step 3: Implement in `rm.go`**

```go
type RmOptions struct {
	DeleteBranch bool
	Force        bool
	KeepDir      bool // used by prune: the whole feature dir is removed afterwards
	Workers      int
	Progress     Progress // optional; receives per-repo events
}
```

In `Remove`, add `pg := notify{o.Progress}` and `pg.Plan(len(repos))` before `stepOutOf`, and change the map body:

```go
	return pool.Map(jobs, o.Workers, func(j job) Result {
		pg.Start(j.repo.ID)
		return pg.Finish(removeOne(ws, feature, j.repo, j.base, o))
	})
```

- [ ] **Step 4: Implement in `sync.go`**

```go
type SyncOptions struct {
	Prune    bool
	Workers  int
	Progress Progress // optional; receives per-repo events
}
```

In `Sync`, after `repos` is discovered, add `pg := notify{o.Progress}` and `pg.Plan(len(repos))`. Wrap the existing worker body: rename the existing anonymous function to a local `one := func(j job) Result { ...existing body... }` and use

```go
	return pool.Map(jobs, o.Workers, func(j job) Result {
		pg.Start(j.repo.ID)
		return pg.Finish(one(j))
	}), nil
```

- [ ] **Step 5: Run tests**

Run: `go test -race ./internal/ops/ && go vet ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -l -w ./cmd ./internal
git add internal/ops
git commit -m "feat(ops): emit progress events from Remove and Sync

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: Progress events from Init

**Files:**
- Modify: `internal/ops/init.go:16-20, 84-129`
- Modify: `internal/ops/init_test.go` (add a test)

**Interfaces:**
- Consumes: `Progress`, `notify`, `recProgress`, `assertProgress`.
- Produces: `InitOptions.Progress Progress`. Init calls `Plan(len(repos))` after validation, `Start`/`Finish` per repo, and `Warn` for each dirty / off-base / cleanup warning at the moment it is appended to `rep.Warnings`.

- [ ] **Step 1: Write the failing test**

Append to `internal/ops/init_test.go` (imports already include `os`, `path/filepath`, `testing`, `ops`, `testutil`; add `"strings"` if absent):

```go
func TestInitReportsProgress(t *testing.T) {
	root := t.TempDir()
	testutil.NewRepo(t, filepath.Join(root, "a"), "main")
	testutil.NewRepo(t, filepath.Join(root, "b"), "main")
	testutil.WriteFile(t, filepath.Join(root, "b", "dirty.txt"), "x\n")

	rec := &recProgress{}
	if _, err := ops.Init(root, ops.InitOptions{Progress: rec}); err != nil {
		t.Fatal(err)
	}
	assertProgress(t, rec, 2, "a", "b")

	warn := rec.index("warn b: has uncommitted changes")
	if warn < 0 {
		t.Fatalf("dirty warning not reported: %v", rec.events)
	}
	if !(rec.index("start b") < warn && warn < rec.index("finish b")) {
		t.Fatalf("dirty warning not between start and finish of b: %v", rec.events)
	}
	for _, e := range rec.events {
		if strings.HasPrefix(e, "warn a") {
			t.Fatalf("unexpected warning for clean repo: %v", rec.events)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ops/ -run TestInitReportsProgress`
Expected: FAIL, `unknown field Progress`.

- [ ] **Step 3: Implement**

`InitOptions`:

```go
type InitOptions struct {
	// BaseBranch, when set, is recorded for every repo instead of detecting.
	BaseBranch string
	// Progress, when set, receives per-repo events.
	Progress Progress
}
```

In `Init`, immediately after the reserved-name loop and before the `var moved []movedRepo` line, add:

```go
	pg := notify{o.Progress}
	pg.Plan(len(w.Repos))
```

Add a small helper next to `Init` so warnings are recorded and emitted together:

```go
// warn records an operation-level warning in the report and reports it.
func (pg notify) warn(rep *InitReport, msg string) {
	rep.Warnings = append(rep.Warnings, msg)
	pg.Warn(msg)
}
```

Inside the move loop:

- First line of the loop body: `pg.Start(r.ID)`.
- The two `return rep, err` inside the mkdir/rename failures become `pg.Finish(result(r.ID, "", err)); return rep, err` (emit the failure, then return).
- Replace `rep.Warnings = append(rep.Warnings, r.ID+": has uncommitted changes")` with `pg.warn(&rep, r.ID+": has uncommitted changes")`.
- Replace `rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: on %s, base branch is %s", r.ID, shown, base))` with `pg.warn(&rep, fmt.Sprintf("%s: on %s, base branch is %s", r.ID, shown, base))`.
- Replace `rep.Moved = append(rep.Moved, result(r.ID, "moved", nil, warnings...))` with `rep.Moved = append(rep.Moved, pg.Finish(result(r.ID, "moved", nil, warnings...)))`.

After the loop, replace `rep.Warnings = append(rep.Warnings, "cleanup: "+err.Error())` with `pg.warn(&rep, "cleanup: "+err.Error())`.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/ops/ && go vet ./...`
Expected: PASS, including the existing rollback test.

- [ ] **Step 5: Commit**

```bash
gofmt -l -w ./cmd ./internal
git add internal/ops
git commit -m "feat(ops): emit progress events and live warnings from Init

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: Progress through CreateFeature and ExecutePrune

**Files:**
- Modify: `internal/ops/feature.go:16-53, 176-184`
- Modify: `internal/ops/feature_test.go`

**Interfaces:**
- Consumes: `Progress`, `notify`, `recProgress`, `assertProgress`.
- Produces: `CreateFeatureOptions.Progress Progress`; `func ExecutePrune(ws *workspace.Workspace, plan PrunePlan, force bool, workers int, p Progress) ([]Result, error)`.

- [ ] **Step 1: Write the failing tests**

In `internal/ops/feature_test.go`, find the existing test that calls `ops.ExecutePrune` and change the call to pass a recorder, asserting afterwards. Substitute the feature's repo ids for `ids`:

```go
	rec := &recProgress{}
	rs, err := ops.ExecutePrune(f.ws, plan, false, 2, rec)
	if err != nil {
		t.Fatal(err)
	}
	assertProgress(t, rec, len(ids), ids...)
```

Find the existing test that calls `ops.CreateFeature` with `From:` set and add `Progress: rec` to its options with an `assertProgress` over the copied repo ids.

If no test uses `From`, add one:

```go
func TestCreateFeatureFromReportsProgress(t *testing.T) {
	f := newFixture(t, "a", "b")
	f.feature("src")
	f.mustOK(ops.Add(f.ws, "src", f.repos("a", "b"), ops.AddOptions{Workers: 2}))
	rec := &recProgress{}
	rs, err := ops.CreateFeature(f.ws, "dst", ops.CreateFeatureOptions{From: "src", Workers: 2, Progress: rec})
	if err != nil {
		t.Fatal(err)
	}
	f.mustOK(rs)
	assertProgress(t, rec, 2, "a", "b")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ops/ -run 'Prune|CreateFeature'`
Expected: FAIL to compile.

- [ ] **Step 3: Implement**

`CreateFeatureOptions`:

```go
type CreateFeatureOptions struct {
	From           string // copy the repo set of this feature
	BranchTemplate string // per-feature override of the branch template
	Workers        int
	Progress       Progress // optional; forwarded to Add
}
```

Last line of `CreateFeature`: `return Add(ws, name, fromRepos, AddOptions{Workers: o.Workers, Progress: o.Progress}), nil`.

`ExecutePrune` signature and the `Remove` call:

```go
func ExecutePrune(ws *workspace.Workspace, plan PrunePlan, force bool, workers int, p Progress) ([]Result, error) {
	...
	results := Remove(ws, plan.Feature, repos, RmOptions{DeleteBranch: true, Force: true, KeepDir: true, Workers: workers, Progress: p})
```

Fix the existing caller in `internal/cli/feat.go:featPruneCmd` by passing `nil` for now: `ops.ExecutePrune(ws, plan, force, workers, nil)`. Task 8 replaces the `nil`.

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test -race ./internal/ops/ && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l -w ./cmd ./internal
git add internal/ops internal/cli
git commit -m "feat(ops): thread Progress through CreateFeature and ExecutePrune

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: CLI reporter

**Files:**
- Create: `internal/cli/progress.go`
- Create: `internal/cli/progress_test.go` (package `cli`, internal test)

**Interfaces:**
- Consumes: `progress.New/Start/Begin/End/Log/Stop` (Task 2), `output.Colour`, `output.IsTerminal`, `output.TerminalWidth` (Task 1), `ops.Progress`, `ops.Result`.
- Produces:
  ```go
  type reporter struct{...}                             // implements ops.Progress; all methods nil-receiver safe
  func newReporter(w io.Writer, c output.Colour, width int) *reporter  // does not start the ticker
  func (r *reporter) start()                            // starts the spinner ticker
  func (r *reporter) finish()                           // stop spinner, print summary if total > 4; idempotent
  func (r *reporter) stop()                             // stop spinner only (for defer)
  func (a *app) newProgress(asJSON bool) *reporter      // nil unless IsTerminal(a.out) && !asJSON; started
  const streamEachUpTo = 4
  ```
- **Gotcha:** a nil `*reporter` stored in an `ops.Progress` field is a non-nil interface. That is why every `reporter` method must return early when `r == nil`.

- [ ] **Step 1: Write the failing tests**

`internal/cli/progress_test.go`:

```go
package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/kumibrr/gibbon/internal/ops"
)

// lines strips spinner control sequences and returns the permanent lines.
func lines(buf *bytes.Buffer) []string {
	s := strings.ReplaceAll(buf.String(), "\r\x1b[2K", "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" && !strings.HasPrefix(l, "[") { // drop spinner frames
			out = append(out, l)
		}
	}
	return out
}

func newTestReporter() (*reporter, *bytes.Buffer) {
	var buf bytes.Buffer
	return newReporter(&buf, false, 80), &buf
}

func TestReporterStreamsEachRepoWhenFourOrFewer(t *testing.T) {
	r, buf := newTestReporter()
	r.Plan(2)
	r.Start("a")
	r.Finish(ops.Result{Repo: "a", Action: "moved"})
	r.Start("b")
	r.Warn("b: has uncommitted changes")
	r.Finish(ops.Result{Repo: "b", Action: "moved", Warnings: []string{"worktree repair: boom"}})
	r.finish()
	want := []string{
		"a moved",
		"warn  b: has uncommitted changes",
		"warn  b moved: worktree repair: boom",
	}
	if got := lines(buf); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestReporterFailureLine(t *testing.T) {
	r, buf := newTestReporter()
	r.Plan(1)
	r.Start("a")
	r.Finish(ops.Result{Repo: "a", Err: errors.New("rename: denied")})
	r.finish()
	if got := lines(buf); len(got) != 1 || got[0] != "fail  a: rename: denied" {
		t.Fatalf("got %q", got)
	}
}

func TestReporterSummaryAboveFour(t *testing.T) {
	r, buf := newTestReporter()
	r.Plan(5)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		r.Start(id)
		r.Finish(ops.Result{Repo: id, Action: "moved"})
	}
	r.finish()
	if got := lines(buf); len(got) != 1 || got[0] != "5 repos moved" {
		t.Fatalf("got %q", got)
	}
}

func TestReporterSummaryWithFailuresAndWarnings(t *testing.T) {
	r, buf := newTestReporter()
	r.Plan(6)
	r.Finish(ops.Result{Repo: "a", Action: "fetched"})
	r.Finish(ops.Result{Repo: "b", Action: "fetched"})
	r.Finish(ops.Result{Repo: "c", Action: "fetched", Warnings: []string{"base branch unknown"}})
	r.Finish(ops.Result{Repo: "d", Action: "fetched"})
	r.Finish(ops.Result{Repo: "e", Err: errors.New("no network")})
	r.Finish(ops.Result{Repo: "f", Err: errors.New("no network")})
	r.finish()
	got := lines(buf)
	want := []string{
		"warn  c fetched: base branch unknown",
		"fail  e: no network",
		"fail  f: no network",
		"fail  4 of 6 repos fetched, 2 failed, 1 with warnings",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestReporterSummaryWarnMarkerWithoutFailures(t *testing.T) {
	r, buf := newTestReporter()
	r.Plan(5)
	for _, id := range []string{"a", "b", "c", "d"} {
		r.Finish(ops.Result{Repo: id, Action: "created"})
	}
	r.Finish(ops.Result{Repo: "e", Action: "created", Warnings: []string{"fetch failed: offline"}})
	r.finish()
	got := lines(buf)
	if got[len(got)-1] != "warn  5 repos created, 1 with warnings" {
		t.Fatalf("got %q", got)
	}
}

func TestReporterSummaryMixedActionsSaysDone(t *testing.T) {
	r, buf := newTestReporter()
	r.Plan(5)
	acts := []string{"created", "created", "exists", "tracked-remote", "created"}
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		r.Finish(ops.Result{Repo: id, Action: acts[i]})
	}
	r.finish()
	if got := lines(buf); got[0] != "5 repos done" {
		t.Fatalf("got %q", got)
	}
}

func TestReporterFinishIsIdempotent(t *testing.T) {
	r, buf := newTestReporter()
	r.Plan(5)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		r.Finish(ops.Result{Repo: id, Action: "moved"})
	}
	r.finish()
	r.finish()
	r.stop()
	if got := lines(buf); len(got) != 1 {
		t.Fatalf("summary printed more than once: %q", got)
	}
}

func TestNilReporterIsSafe(t *testing.T) {
	var r *reporter
	r.Plan(1)
	r.Start("a")
	r.Warn("x")
	r.Finish(ops.Result{Repo: "a"})
	r.finish()
	r.stop()
}

func TestNewProgressNilForNonTerminal(t *testing.T) {
	a := &app{out: &bytes.Buffer{}}
	if a.newProgress(false) != nil {
		t.Fatal("expected nil reporter for buffer stdout")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'Reporter|NewProgress'`
Expected: FAIL to compile, `undefined: newReporter`.

- [ ] **Step 3: Implement**

`internal/cli/progress.go`:

```go
package cli

import (
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/kumibrr/gibbon/internal/ops"
	"github.com/kumibrr/gibbon/internal/output"
	"github.com/kumibrr/gibbon/internal/progress"
)

// streamEachUpTo is the largest repo count for which every clean completion
// prints its own line. Above it, only warnings/failures stream and a summary
// line is printed at the end.
const streamEachUpTo = 4

// reporter renders ops.Progress events onto a spinner. All methods are safe
// on a nil receiver so a nil *reporter can be passed straight into ops
// option structs.
type reporter struct {
	sp     *progress.Spinner
	w      io.Writer
	colour output.Colour

	mu      sync.Mutex
	total   int
	failed  int
	warned  int
	actions map[string]int // action word -> count, for clean results
	done    bool
}

func newReporter(w io.Writer, c output.Colour, width int) *reporter {
	return &reporter{sp: progress.New(w, width), w: w, colour: c, actions: map[string]int{}}
}

// newProgress returns a started reporter when stdout is a terminal and
// JSON output is off; otherwise nil, which disables progress entirely.
func (a *app) newProgress(asJSON bool) *reporter {
	if asJSON || !output.IsTerminal(a.out) {
		return nil
	}
	r := newReporter(a.out, a.colour, output.TerminalWidth(a.out))
	r.start()
	return r
}

func (r *reporter) start() {
	if r == nil {
		return
	}
	r.sp.Start()
}

func (r *reporter) marker(kind string) string {
	switch kind {
	case "warn":
		return r.colour.Yellow("warn") + "  "
	default:
		return r.colour.Red("fail") + "  "
	}
}

// Plan implements ops.Progress.
func (r *reporter) Plan(total int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.total = total
	r.mu.Unlock()
}

// Start implements ops.Progress.
func (r *reporter) Start(repo string) {
	if r == nil {
		return
	}
	r.sp.Begin(repo)
}

// Warn implements ops.Progress.
func (r *reporter) Warn(msg string) {
	if r == nil {
		return
	}
	r.sp.Log(r.marker("warn") + r.colour.Yellow(msg))
}

// Finish implements ops.Progress.
func (r *reporter) Finish(res ops.Result) {
	if r == nil {
		return
	}
	r.sp.End(res.Repo)
	r.mu.Lock()
	total := r.total
	switch {
	case res.Err != nil:
		r.failed++
	case len(res.Warnings) > 0:
		r.warned++
		r.actions[res.Action]++
	default:
		r.actions[res.Action]++
	}
	r.mu.Unlock()

	switch {
	case res.Err != nil:
		r.sp.Log(r.marker("fail") + r.colour.Red(res.Repo+": "+res.Err.Error()))
	case len(res.Warnings) > 0:
		r.sp.Log(r.marker("warn") + r.colour.Yellow(res.Repo+" "+res.Action+": "+joinWarnings(res.Warnings)))
	case total <= streamEachUpTo:
		r.sp.Log(res.Repo + " " + res.Action)
	}
}

// stop halts the spinner without printing a summary. Safe to defer.
func (r *reporter) stop() {
	if r == nil {
		return
	}
	r.sp.Stop()
}

// finish stops the spinner and prints the summary line when the operation
// covered more than streamEachUpTo repos. Idempotent.
func (r *reporter) finish() {
	if r == nil {
		return
	}
	r.sp.Stop()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done || r.total <= streamEachUpTo {
		r.done = true
		return
	}
	r.done = true
	ok := r.total - r.failed
	line := fmt.Sprintf("%d repos %s", r.total, r.verb())
	if r.failed > 0 {
		line = fmt.Sprintf("%d of %d repos %s, %d failed", ok, r.total, r.verb(), r.failed)
	}
	if r.warned > 0 {
		line += fmt.Sprintf(", %d with warnings", r.warned)
	}
	switch {
	case r.failed > 0:
		line = r.marker("fail") + r.colour.Red(line)
	case r.warned > 0:
		line = r.marker("warn") + r.colour.Yellow(line)
	}
	fmt.Fprintln(r.w, line)
}

// verb is the single action word shared by all clean results, or "done"
// when they differ or there were none. Caller holds mu.
func (r *reporter) verb() string {
	if len(r.actions) != 1 {
		return "done"
	}
	keys := make([]string, 0, 1)
	for k := range r.actions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys[0]
}
```

Note the spec said "most common action word"; the tests pin the simpler, unambiguous rule: one distinct action word → use it, otherwise `done`. Update the spec sentence in Task 9.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/cli/ -run 'Reporter|NewProgress' && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l -w ./cmd ./internal
git add internal/cli/progress.go internal/cli/progress_test.go
git commit -m "feat(cli): add progress reporter over the spinner

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 8: Wire the reporter into commands

**Files:**
- Modify: `internal/cli/root.go:103-133` (`printResults`)
- Modify: `internal/cli/add_rm.go` (add, rm)
- Modify: `internal/cli/status_sync_doctor.go` (sync)
- Modify: `internal/cli/feat.go` (`featCreate`, `featPruneCmd`)
- Modify: `internal/cli/init.go`
- Modify: `internal/cli/cli_test.go` (one new binary test)

**Interfaces:**
- Consumes: `(*app).newProgress(asJSON) *reporter`, `(*reporter).finish()`, `(*reporter).stop()` from Task 7; `Progress` fields and `ExecutePrune` signature from Tasks 3-6.
- Produces: `func (a *app) printResults(rs []ops.Result, asJSON bool, p *reporter) error`.

- [ ] **Step 1: Write the failing binary test**

The CLI tests run the built binary through a pipe, so they must keep seeing the table. Add to `internal/cli/cli_test.go` a regression guard (adapt `gibbon(...)` to the helper the file already uses to run the binary in a workspace; read the file to find it):

```go
func TestPipedOutputHasNoSpinnerSequences(t *testing.T) {
	ws := newWorkspace(t, "a", "b") // use the file's existing workspace fixture helper
	r := gibbon(t, ws, "feat", "-c", "pay")
	r = gibbon(t, filepath.Join(ws, "pay"), "add", "--all")
	if strings.Contains(r.out, "\x1b[2K") || strings.Contains(r.out, "[=") {
		t.Fatalf("spinner sequences leaked into piped output:\n%s", r.out)
	}
	if !strings.Contains(r.out, "REPO") {
		t.Fatalf("table missing from piped output:\n%s", r.out)
	}
}
```

If the file's helpers have different names, use those; the assertions are what matter.

- [ ] **Step 2: Run it to confirm it passes today (baseline)**

Run: `go test ./internal/cli/ -run TestPipedOutputHasNoSpinnerSequences`
Expected: PASS (this test guards the non-TTY path through the rest of the task).

- [ ] **Step 3: Change `printResults` in `root.go`**

```go
// printResults renders per-repo results and returns errFailed if any failed.
// When p is non-nil the results were already streamed live, so only the
// summary is printed and the table is skipped.
func (a *app) printResults(rs []ops.Result, asJSON bool, p *reporter) error {
	switch {
	case asJSON:
		if err := output.JSON(a.out, rs); err != nil {
			return err
		}
	case p != nil:
		p.finish()
	case len(rs) > 0:
		rows := make([][]string, 0, len(rs))
		for _, r := range rs {
			detail := ""
			action := r.Action
			switch {
			case r.Err != nil:
				action = a.colour.Red(action)
				detail = a.colour.Red(r.Err.Error())
			case len(r.Warnings) > 0:
				action = a.colour.Yellow(action)
				detail = a.colour.Yellow(joinWarnings(r.Warnings))
			default:
				action = a.colour.Green(action)
			}
			rows = append(rows, []string{r.Repo, action, detail})
		}
		output.Table(a.out, a.colour, []string{"REPO", "ACTION", "DETAIL"}, rows)
	}
	if ops.AnyFailed(rs) {
		return errFailed
	}
	return nil
}
```

- [ ] **Step 4: Wire `add` and `rm` in `add_rm.go`**

Replace the last two lines of each `RunE`:

```go
			p := a.newProgress(asJSON)
			defer p.stop()
			rs := ops.Add(ws, feat, repos, ops.AddOptions{Branch: branch, Workers: workers, Progress: p})
			return a.printResults(rs, asJSON, p)
```

```go
			p := a.newProgress(asJSON)
			defer p.stop()
			rs := ops.Remove(ws, feat, repos, ops.RmOptions{DeleteBranch: deleteBranch, Force: force, Workers: workers, Progress: p})
			return a.printResults(rs, asJSON, p)
```

- [ ] **Step 5: Wire `sync` in `status_sync_doctor.go`**

```go
			p := a.newProgress(asJSON)
			defer p.stop()
			rs, err := ops.Sync(ws, ops.SyncOptions{Prune: prune, Workers: workers, Progress: p})
			if err != nil {
				return err
			}
			return a.printResults(rs, asJSON, p)
```

- [ ] **Step 6: Wire `feat -c` and `feat prune` in `feat.go`**

`featCreate`: create the reporter before `ops.CreateFeature` and print the "Created feature" header first so it lands above the stream:

```go
func (a *app) featCreate(name, from, template string, asJSON bool) error {
	ws, workers, err := a.workspace()
	if err != nil {
		return err
	}
	dir := ws.FeatureDir(name)
	p := a.newProgress(asJSON)
	defer p.stop()
	if p != nil {
		fmt.Fprintf(a.out, "Creating feature %s at %s\n", a.colour.Bold(name), dir)
	}
	rs, err := ops.CreateFeature(ws, name, ops.CreateFeatureOptions{From: from, BranchTemplate: template, Workers: workers, Progress: p})
	if err != nil {
		return err
	}
	wrapped, cdErr := shell.RequestCD(dir)
	if cdErr != nil {
		return cdErr
	}
	if asJSON {
		return a.printResults(rs, true, nil)
	}
	if p == nil {
		fmt.Fprintf(a.out, "Created feature %s at %s\n", a.colour.Bold(name), dir)
	}
	if err := a.printResults(rs, false, p); err != nil {
		return err
	}
	if p != nil {
		fmt.Fprintf(a.out, "Created feature %s\n", a.colour.Bold(name))
	}
	if !wrapped {
		fmt.Fprintln(a.out, a.colour.Dim("cd "+dir))
	}
	return nil
}
```

`featPruneCmd`: replace the `ExecutePrune` call and result printing:

```go
			p := a.newProgress(asJSON)
			defer p.stop()
			rs, execErr := ops.ExecutePrune(ws, plan, force, workers, p)
			if asJSON {
				output.JSON(a.out, map[string]any{"plan": plan, "results": rs, "error": errString(execErr)})
			} else if p != nil {
				p.finish()
			} else if len(rs) > 0 {
				fmt.Fprintln(a.out)
				a.printResults(rs, false, nil)
			}
```

(The check-phase table printed before this stays as is: it is read-only and happens before the reporter is created.)

- [ ] **Step 7: Wire `init` in `init.go`**

Replace the `RunE` body from `rep, err := ops.Init(...)` through the `Moved` table and `Warnings` block:

```go
			p := a.newProgress(asJSON)
			defer p.stop()
			rep, err := ops.Init(dir, ops.InitOptions{BaseBranch: baseBranch, Progress: p})
			if asJSON {
				output.JSON(a.out, rep)
				return err
			}
			if p != nil {
				p.finish()
			} else if len(rep.Moved) > 0 {
				rows := [][]string{}
				for _, r := range rep.Moved {
					rows = append(rows, []string{r.Repo, a.colour.Green(r.Action), a.colour.Yellow(joinWarnings(r.Warnings))})
				}
				output.Table(a.out, a.colour, []string{"REPO", "ACTION", "DETAIL"}, rows)
			}
			if len(rep.NotMoved) > 0 {
				fmt.Fprintln(a.out, a.colour.Red("\nNot moved:"))
				for _, id := range rep.NotMoved {
					fmt.Fprintln(a.out, "  "+id)
				}
			}
			if len(rep.Skipped) > 0 {
				fmt.Fprintln(a.out, a.colour.Dim("\nLeft in place (not git repositories):"))
				for _, s := range rep.Skipped {
					fmt.Fprintln(a.out, a.colour.Dim("  "+s))
				}
			}
			if p == nil && len(rep.Warnings) > 0 {
				fmt.Fprintln(a.out, a.colour.Yellow("\nWarnings:"))
				for _, w := range rep.Warnings {
					fmt.Fprintln(a.out, a.colour.Yellow("  "+w))
				}
			}
			if err == nil {
				fmt.Fprintf(a.out, "\nInitialised workspace with %d repos. Next: eval \"$(gibbon shell-init bash)\" and gibbon feat -c NAME\n", len(rep.Moved))
			}
			return err
```

The warnings block is skipped when `p != nil` because those warnings were already streamed live.

- [ ] **Step 8: Build and run everything**

Run: `go build ./... && go vet ./... && go test -race ./...`
Expected: PASS, including the baseline binary test from Step 1.

- [ ] **Step 9: Manual TTY check**

```bash
go build -o /tmp/gibbon ./cmd/gibbon
d=$(mktemp -d); for r in a b c d e f; do git -C "$d" init -q "$r"; git -C "$d/$r" commit -q --allow-empty -m init; done
echo x > "$d/b/dirty"
script -qc "cd $d && /tmp/gibbon init" /dev/null
```

Expected: a `warn  b: has uncommitted changes` line, no per-repo lines (6 repos), then `6 repos moved` and the "Initialised workspace" line. No leftover spinner text. Then in the same workspace:

```bash
script -qc "cd $d && /tmp/gibbon feat -c t && cd t && /tmp/gibbon add a b" /dev/null
```

Expected: `a created` and `b created` lines (2 repos ≤ 4), no summary line.

- [ ] **Step 10: Commit**

```bash
gofmt -l -w ./cmd ./internal
git add internal/cli
git commit -m "feat(cli): stream progress with a spinner on mutating commands

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 9: Docs

**Files:**
- Modify: `README.md` (Commands section, after the line "run every repo, print a table, and exit non-zero if any repo failed.")
- Modify: `docs/superpowers/specs/2026-09-15-gibbon-progress-spinner-design.md` (summary verb rule)

- [ ] **Step 1: README**

After the sentence ending "exit non-zero if any repo failed." add:

```markdown
When stdout is a terminal, `init`, `feat -c`, `add`, `rm`, `sync` and
`feat prune` show a live status line with the repos being worked on and
stream warnings (`warn`) and failures (`fail`) as they happen. With four
repos or fewer each completion is printed; above that only a summary line
is printed at the end. When piped, or with `--json`, output is the usual
table or JSON and nothing is streamed.
```

- [ ] **Step 2: Spec**

In the spec's Behaviour section replace

`The verb is the most common action word among the results (\`moved\`, \`added\`, \`removed\`, \`fetched\`, ...); when actions differ, the line reads \`12 repos done\`.`

with

`The verb is the action word shared by every clean result (\`moved\`, \`created\`, \`fetched\`, ...); when clean results carry different actions, or there are none, the line reads \`12 repos done\`. When some repos warned, the line ends with \`, N with warnings\`.`

- [ ] **Step 3: Commit**

```bash
git add README.md docs/superpowers/specs/2026-09-15-gibbon-progress-spinner-design.md
git commit -m "docs: describe live progress output

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```
