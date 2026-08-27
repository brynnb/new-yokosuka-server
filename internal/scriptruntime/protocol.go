package scriptruntime

import "github.com/brynnb/new-yokosuka-server/internal/scriptcontent"

const ProtocolVersion = "new-yokosuka-yarn-runtime-v1"

type Value struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type Variable struct {
	Name  string `json:"name"`
	Value Value  `json:"value"`
}

type StartRequest struct {
	Program   []byte
	StartNode string
	Variables []Variable
}

type startMessage struct {
	ProtocolVersion string               `json:"protocolVersion"`
	ProgramBase64   string               `json:"programBase64"`
	StartNode       string               `json:"startNode"`
	Functions       []functionDefinition `json:"functions"`
	Variables       []Variable           `json:"variables"`
}

type functionDefinition struct {
	Name           string   `json:"name"`
	ReturnType     string   `json:"returnType"`
	ParameterTypes []string `json:"parameterTypes"`
}

type Input struct {
	Type     string `json:"type"`
	QueryID  *int   `json:"queryId,omitempty"`
	OptionID *int   `json:"optionId,omitempty"`
	Value    *Value `json:"value,omitempty"`
}

type Event struct {
	Type          string                           `json:"type"`
	Sequence      int                              `json:"sequence"`
	QueryID       *int                             `json:"queryId,omitempty"`
	Name          string                           `json:"name,omitempty"`
	Node          string                           `json:"node,omitempty"`
	LineID        string                           `json:"lineId,omitempty"`
	Substitutions []string                         `json:"substitutions,omitempty"`
	Arguments     []scriptcontent.CompiledArgument `json:"arguments,omitempty"`
	Options       []Option                         `json:"options,omitempty"`
	Message       string                           `json:"message,omitempty"`
}

type Option struct {
	ID            int      `json:"id"`
	LineID        string   `json:"lineId"`
	Substitutions []string `json:"substitutions"`
	IsAvailable   bool     `json:"isAvailable"`
}

func Continue() Input { return Input{Type: "continue"} }

func Cancel() Input { return Input{Type: "cancel"} }

func Select(optionID int) Input { return Input{Type: "select", OptionID: &optionID} }

func QueryResult(queryID int, value Value) Input {
	return Input{Type: "queryResult", QueryID: &queryID, Value: &value}
}
