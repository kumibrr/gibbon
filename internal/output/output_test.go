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
