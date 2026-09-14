// Package output renders tables and JSON for the CLI.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// Colour decides whether ANSI colours are emitted.
type Colour bool

// DetectColour enables colour when w is a terminal and NO_COLOR is unset.
func DetectColour(w io.Writer) Colour {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
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
