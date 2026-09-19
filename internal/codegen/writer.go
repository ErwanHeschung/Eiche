// Package codegen holds what the Java and TS emitters share: a small
// line-based output builder. Per .context/CLAUDE.md: "Emitters build
// output as line slices with an indent counter, not templates."
package codegen

import (
	"fmt"
	"strings"
)

const indentUnit = "    "

type Writer struct {
	lines  []string
	indent int
}

func (w *Writer) Line(format string, args ...any) {
	w.lines = append(w.lines, strings.Repeat(indentUnit, w.indent)+fmt.Sprintf(format, args...))
}

func (w *Writer) Blank() {
	w.lines = append(w.lines, "")
}

func (w *Writer) Indent() {
	w.indent++
}

func (w *Writer) Dedent() {
	if w.indent > 0 {
		w.indent--
	}
}

func (w *Writer) String() string {
	return strings.Join(w.lines, "\n") + "\n"
}
