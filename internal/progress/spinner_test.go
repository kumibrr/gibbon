package progress

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
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

func TestLineTruncatedToWidthMultiByte(t *testing.T) {
	s := New(&bytes.Buffer{}, 20)
	s.Begin(strings.Repeat("é", 40))
	got := s.line()
	if !utf8.ValidString(got) {
		t.Fatalf("got invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 19 {
		t.Fatalf("got rune count %d, want 19 (%q)", n, got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("got %q, want suffix \"...\"", got)
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
