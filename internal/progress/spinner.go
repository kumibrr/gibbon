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
