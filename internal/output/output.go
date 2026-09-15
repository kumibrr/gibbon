// Package output renders tables and JSON for the CLI.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

// Colour decides whether ANSI colours are emitted.
type Colour bool

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

func (c Colour) wrap(code, s string) string {
	if !c || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// Green marks success.
func (c Colour) Green(s string) string { return c.wrap("32", s) }

// Yellow marks warnings.
func (c Colour) Yellow(s string) string { return c.wrap("33", s) }

// Red marks failures.
func (c Colour) Red(s string) string { return c.wrap("31", s) }

// Dim de-emphasises.
func (c Colour) Dim(s string) string { return c.wrap("2", s) }

// Bold emphasises headers.
func (c Colour) Bold(s string) string { return c.wrap("1", s) }

// Table writes aligned columns. Headers may be nil.
func Table(w io.Writer, c Colour, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if len(headers) > 0 {
		cells := make([]string, len(headers))
		for i, h := range headers {
			cells[i] = c.Bold(h)
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

// JSON writes v as indented JSON.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
