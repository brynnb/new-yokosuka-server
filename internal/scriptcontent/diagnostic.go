package scriptcontent

// Diagnostic is a compiler or command-schema finding tied to source text.
// Line and column are one-based when the compiler supplies a source position.
type Diagnostic struct {
	Severity  string `json:"severity"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	FileName  string `json:"fileName,omitempty"`
	Node      string `json:"node,omitempty"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	EndColumn int    `json:"endColumn,omitempty"`
}
