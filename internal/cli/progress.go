package cli

import (
	"fmt"
	"io"
	"os"
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

	mu       sync.Mutex
	total    int
	finished int
	failed   int
	warned   int
	actions  map[string]int // action word -> count, for clean results
	done     bool
}

func newReporter(w io.Writer, c output.Colour, width int) *reporter {
	return &reporter{sp: progress.New(w, width), w: w, colour: c, actions: map[string]int{}}
}

// progressAllowed reports whether a spinner should run given the terminal's
// TERM and NO_COLOR environment values. Dumb or unset terminals, and
// NO_COLOR being set, disable it.
func progressAllowed(term, noColor string) bool {
	if noColor != "" {
		return false
	}
	if term == "" || term == "dumb" {
		return false
	}
	return true
}

// newProgress returns a started reporter when stdout is a terminal and
// JSON output is off; otherwise nil, which disables progress entirely.
func (a *app) newProgress(asJSON bool) *reporter {
	if asJSON || !output.IsTerminal(a.out) {
		return nil
	}
	if !progressAllowed(os.Getenv("TERM"), os.Getenv("NO_COLOR")) {
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
	r.finished++
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
	case res.Err != nil && len(res.Warnings) > 0:
		r.sp.Log(r.marker("fail") + r.colour.Red(res.Repo+": "+res.Err.Error()+" (warnings: "+joinWarnings(res.Warnings)+")"))
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

// sawResults reports whether Finish was ever called, i.e. the reporter was
// actually driven through at least one repo. Nil-safe.
func (r *reporter) sawResults() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finished > 0
}

// planned reports whether Plan was ever called with a positive total, i.e.
// the operation actually got far enough to schedule work. Nil-safe.
func (r *reporter) planned() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.total > 0
}

// log prints a permanent line above the spinner, or does nothing on a nil
// receiver.
func (r *reporter) log(line string) {
	if r == nil {
		return
	}
	r.sp.Log(line)
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
	ok := r.finished - r.failed
	var line string
	switch {
	case r.failed > 0:
		line = fmt.Sprintf("%d of %d repos %s, %d failed", ok, r.total, r.verb(), r.failed)
	case r.finished < r.total:
		line = fmt.Sprintf("%d of %d repos %s", ok, r.total, r.verb())
	default:
		line = fmt.Sprintf("%d repos %s", r.total, r.verb())
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
	for k := range r.actions {
		return k
	}
	return "done"
}
