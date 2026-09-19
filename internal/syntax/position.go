package syntax

// Position is a 1-indexed line/column location in a single source file.
// Kept file-relative (no filename) because the file name is attached once,
// at the Diagnostic level, rather than repeated on every token.
type Position struct {
	Line   int
	Column int
	Offset int // byte offset into the source, for slicing excerpts
}

func (p Position) IsValid() bool {
	return p.Line > 0
}
