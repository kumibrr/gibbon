package cli

import (
	"fmt"
	"io"
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
	for k := range r.actions {
		return k
	}
	return "done"
}
