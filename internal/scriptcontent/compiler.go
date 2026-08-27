package scriptcontent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	compilerProtocolVersion = "new-yokosuka-yarn-compiler-v1"
	compilerTimeout         = 5 * time.Second
	maxCompilerOutputBytes  = 16 * 1024 * 1024
)

type Compiler interface {
	Compile(context.Context, string, string) (Compilation, error)
}

type Compilation struct {
	Valid       bool           `json:"valid"`
	Program     []byte         `json:"-"`
	Diagnostics []Diagnostic   `json:"diagnostics"`
	Lines       []CompiledLine `json:"lines"`
	Nodes       []CompiledNode `json:"nodes"`
	Analysis    Analysis       `json:"analysis"`
}

type CompiledLine struct {
	ID            string   `json:"id"`
	Text          *string  `json:"text"`
	FileName      string   `json:"fileName"`
	NodeName      string   `json:"nodeName"`
	LineNumber    int      `json:"lineNumber"`
	HasImplicitID bool     `json:"hasImplicitId"`
	Metadata      []string `json:"metadata"`
	ShadowLineID  *string  `json:"shadowLineId"`
}

// PresentedOption joins a runtime option ID to its immutable compiled line.
// The runtime emits only line IDs; clients must never guess option text.
type PresentedOption struct {
	ID            int          `json:"id"`
	IsAvailable   bool         `json:"isAvailable"`
	Substitutions []string     `json:"substitutions,omitempty"`
	Line          CompiledLine `json:"line"`
}

type CompiledNode struct {
	Title              string   `json:"title"`
	SourceTitle        *string  `json:"sourceTitle"`
	UniqueTitle        *string  `json:"uniqueTitle"`
	Group              *string  `json:"group"`
	FunctionCalls      []string `json:"functionCalls"`
	CommandCalls       []string `json:"commandCalls"`
	VariableReferences []string `json:"variableReferences"`
	CharacterNames     []string `json:"characterNames"`
	Tags               []string `json:"tags"`
	OptionCount        int      `json:"optionCount"`
	HeaderStartLine    int      `json:"headerStartLine"`
	TitleLine          int      `json:"titleLine"`
	BodyStartLine      int      `json:"bodyStartLine"`
	BodyEndLine        int      `json:"bodyEndLine"`
}

type CompiledCall struct {
	Kind        string             `json:"kind"`
	Name        string             `json:"name"`
	Node        string             `json:"node"`
	FileName    string             `json:"fileName"`
	StartLine   int                `json:"startLine"`
	StartColumn int                `json:"startColumn"`
	EndLine     int                `json:"endLine"`
	EndColumn   int                `json:"endColumn"`
	ParseError  *string            `json:"parseError"`
	Arguments   []CompiledArgument `json:"arguments"`
}

type CompiledArgument struct {
	Type     string  `json:"type"`
	IsStatic bool    `json:"isStatic"`
	Value    *string `json:"value"`
}

type processCompiler struct {
	executable string
}

func NewProcessCompiler(executable string) (Compiler, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return nil, errors.New("Yarn compiler executable is required")
	}
	return &processCompiler{executable: executable}, nil
}

type compilerFunction struct {
	Name           string   `json:"name"`
	ReturnType     string   `json:"returnType"`
	ParameterTypes []string `json:"parameterTypes"`
}

type compilerRequest struct {
	FileName  string             `json:"fileName"`
	Source    string             `json:"source"`
	Functions []compilerFunction `json:"functions"`
}

type compilerResponse struct {
	ProtocolVersion string  `json:"protocolVersion"`
	CompilerVersion string  `json:"compilerVersion"`
	Valid           bool    `json:"valid"`
	ProgramBase64   *string `json:"programBase64"`
	Diagnostics     []struct {
		Code        string `json:"code"`
		Severity    string `json:"severity"`
		Message     string `json:"message"`
		FileName    string `json:"fileName"`
		StartLine   int    `json:"startLine"`
		StartColumn int    `json:"startColumn"`
		EndLine     int    `json:"endLine"`
		EndColumn   int    `json:"endColumn"`
	} `json:"diagnostics"`
	Lines []CompiledLine `json:"lines"`
	Nodes []CompiledNode `json:"nodes"`
	Calls []CompiledCall `json:"calls"`
}

func (c *processCompiler) Compile(ctx context.Context, fileName, source string) (Compilation, error) {
	registry, err := Registry()
	if err != nil {
		return Compilation{}, err
	}
	request := compilerRequest{FileName: fileName, Source: source}
	for _, entry := range registry.Entries {
		if entry.Kind != "function" {
			continue
		}
		function := compilerFunction{Name: entry.Name, ReturnType: entry.ReturnType}
		for _, parameter := range entry.Parameters {
			function.ParameterTypes = append(function.ParameterTypes, parameter.Type)
		}
		request.Functions = append(request.Functions, function)
	}
	input, err := json.Marshal(request)
	if err != nil {
		return Compilation{}, fmt.Errorf("encode Yarn compile request: %w", err)
	}
	compileCtx, cancel := context.WithTimeout(ctx, compilerTimeout)
	defer cancel()
	command := exec.CommandContext(compileCtx, c.executable)
	command.Stdin = bytes.NewReader(input)
	stdout, stderr := &boundedBuffer{limit: maxCompilerOutputBytes}, &boundedBuffer{limit: 64 * 1024}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		if errors.Is(compileCtx.Err(), context.DeadlineExceeded) {
			return Compilation{}, errors.New("Yarn compiler timed out")
		}
		return Compilation{}, fmt.Errorf("Yarn compiler failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.exceeded {
		return Compilation{}, errors.New("Yarn compiler output exceeded 16 MiB")
	}
	return decodeCompilation(stdout.Bytes(), registry)
}

func decodeCompilation(body []byte, registry CommandRegistry) (Compilation, error) {
	var response compilerResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return Compilation{}, fmt.Errorf("decode Yarn compiler response: %w", err)
	}
	if response.ProtocolVersion != compilerProtocolVersion || response.CompilerVersion != YarnCompilerVersion {
		return Compilation{}, fmt.Errorf("unsupported Yarn compiler identity %q/%q", response.ProtocolVersion, response.CompilerVersion)
	}
	compilation := Compilation{Valid: response.Valid, Lines: response.Lines, Nodes: response.Nodes}
	for _, diagnostic := range response.Diagnostics {
		compilation.Diagnostics = append(compilation.Diagnostics, Diagnostic{
			Severity: diagnostic.Severity, Code: diagnostic.Code, Message: diagnostic.Message,
			FileName: diagnostic.FileName, Line: diagnostic.StartLine + 1,
			Column: diagnostic.StartColumn + 1, EndLine: diagnostic.EndLine + 1,
			EndColumn: diagnostic.EndColumn + 1,
		})
	}
	if response.ProgramBase64 != nil {
		program, err := base64.StdEncoding.DecodeString(*response.ProgramBase64)
		if err != nil {
			return Compilation{}, fmt.Errorf("decode compiled Yarn program: %w", err)
		}
		compilation.Program = program
	}
	analysis, schemaDiagnostics := AnalyzeCalls(response.Calls, registry)
	compilation.Analysis = analysis
	compilation.Diagnostics = append(compilation.Diagnostics, schemaDiagnostics...)
	if len(schemaDiagnostics) > 0 {
		compilation.Valid = false
	}
	if compilation.Valid && len(compilation.Program) == 0 {
		return Compilation{}, errors.New("valid Yarn compiler response has no program")
	}
	if !compilation.Valid {
		compilation.Program = nil
	}
	return compilation, nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	originalLength := len(value)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.exceeded = true
		return originalLength, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.exceeded = true
	}
	_, _ = b.Buffer.Write(value)
	return originalLength, nil
}
