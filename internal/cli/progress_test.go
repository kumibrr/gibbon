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

func TestReporterFailureLineKeepsWarnings(t *testing.T) {
	r, buf := newTestReporter()
	r.Plan(1)
	r.Finish(ops.Result{Repo: "a", Err: errors.New("boom"), Warnings: []string{"fetch failed: timeout"}})
	r.finish()
	if got := lines(buf); len(got) != 1 || got[0] != "fail  a: boom (warnings: fetch failed: timeout)" {
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

// TestPrintResultsFallsBackToTableWhenReporterSawNoResults covers a
// pre-Plan failure (e.g. feature missing, branch template error): ops
// returns a []Result without ever driving the reporter through Plan/Finish.
// printResults must not silently call finish() (which prints nothing for
// total 0) and swallow the failure; it must fall back to the table.
func TestPrintResultsFallsBackToTableWhenReporterSawNoResults(t *testing.T) {
	var buf bytes.Buffer
	a := &app{out: &buf, colour: false}
	r := newReporter(&buf, false, 80)
	err := a.printResults([]ops.Result{{Repo: "", Action: "failed", Err: errors.New(`feature "x" does not exist`)}}, false, r)
	if !errors.Is(err, errFailed) {
		t.Fatalf("expected errFailed, got %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "REPO") {
		t.Fatalf("expected table header in output, got %q", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Fatalf("expected failure detail in output, got %q", out)
	}
}

func TestReporterSummaryUsesFinishedNotTotalWhenAborted(t *testing.T) {
	r, buf := newTestReporter()
	r.Plan(12)
	r.Finish(ops.Result{Repo: "a", Action: "moved"})
	r.Finish(ops.Result{Repo: "b", Action: "moved"})
	r.Finish(ops.Result{Repo: "c", Err: errors.New("boom")})
	r.finish()
	got := lines(buf)
	want := "fail  2 of 12 repos moved, 1 failed"
	if len(got) == 0 || got[len(got)-1] != want {
		t.Fatalf("got %q\nwant last line %q", got, want)
	}
}

func TestReporterSummaryNoFailuresWhenAborted(t *testing.T) {
	r, buf := newTestReporter()
	r.Plan(12)
	r.Finish(ops.Result{Repo: "a", Action: "moved"})
	r.Finish(ops.Result{Repo: "b", Action: "moved"})
	r.finish()
	got := lines(buf)
	want := "2 of 12 repos moved"
	if len(got) == 0 || got[len(got)-1] != want {
		t.Fatalf("got %q\nwant last line %q", got, want)
	}
}

func TestProgressAllowedDumbTerminal(t *testing.T) {
	cases := []struct {
		term, noColor string
		want          bool
	}{
		{"xterm-256color", "", true},
		{"dumb", "", false},
		{"", "", false},
		{"xterm-256color", "1", false},
	}
	for _, c := range cases {
		if got := progressAllowed(c.term, c.noColor); got != c.want {
			t.Fatalf("progressAllowed(%q, %q) = %v, want %v", c.term, c.noColor, got, c.want)
		}
	}
}

func TestReporterPlanned(t *testing.T) {
	r, _ := newTestReporter()
	if r.planned() {
		t.Fatal("expected planned() false before Plan")
	}
	r.Plan(1)
	if !r.planned() {
		t.Fatal("expected planned() true after Plan(1)")
	}
	var nilR *reporter
	if nilR.planned() {
		t.Fatal("expected nil *reporter planned() false")
	}
}

func TestNewProgressNilOnDumbTerm(t *testing.T) {
	t.Setenv("TERM", "dumb")
	a := &app{out: &bytes.Buffer{}}
	if a.newProgress(false) != nil {
		t.Fatal("expected nil reporter on dumb terminal")
	}
}
