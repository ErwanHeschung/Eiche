package syntax

import (
	"fmt"
	"strings"
)

// Diagnostic is the one error/warning shape every compiler stage reports
// through — lexer, parser, type checker, expression checker alike, per
// the brief's "one channel across all compiler stages" rule.
type Diagnostic struct {
	File    string
	Pos     Position
	Message string
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", d.File, d.Pos.Line, d.Pos.Column, d.Message)
}

// Render produces a human-facing, multi-line report: the message, then the
// offending source line with a caret under the exact column.
func (d Diagnostic) Render(source string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:%d:%d: %s\n", d.File, d.Pos.Line, d.Pos.Column, d.Message)

	lines := strings.Split(source, "\n")
	if d.Pos.Line-1 < 0 || d.Pos.Line-1 >= len(lines) {
		return b.String()
	}
	line := lines[d.Pos.Line-1]
	fmt.Fprintf(&b, "  %s\n", line)

	col := d.Pos.Column
	if col < 1 {
		col = 1
	}
	fmt.Fprintf(&b, "  %s^\n", strings.Repeat(" ", col-1))
	return b.String()
}
