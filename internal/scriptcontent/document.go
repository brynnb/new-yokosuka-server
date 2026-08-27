package scriptcontent

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const Schema = "new-yokosuka-script-v1"

const (
	MaxNodes = 20000
	MaxEdges = 80000
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,159}$`)

var nodeTypes = map[string]struct{}{
	"trigger": {}, "condition": {}, "dialogue": {}, "choice": {},
	"action": {}, "wait": {}, "call": {}, "end": {},
	"native_function": {}, "native_operation": {}, "unresolved_native": {},
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Node struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Label    string         `json:"label"`
	Position Position       `json:"position"`
	Config   map[string]any `json:"config"`
}

type Edge struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	Port string `json:"port,omitempty"`
}

type Document struct {
	Schema      string `json:"schema"`
	EntryNodeID string `json:"entryNodeId"`
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges"`
}

type Dependency struct {
	Access     string `json:"access"`
	Kind       string `json:"kind"`
	Identifier string `json:"identifier"`
}

// IdentifierUsage is a compiler-proven static identifier argument. It is
// separate from Dependency because presentation resources such as cameras and
// sounds are useful authoring symbols without being durable state reads/writes.
type IdentifierUsage struct {
	Kind       string `json:"kind"`
	Identifier string `json:"identifier"`
}

type Trigger struct {
	NodeID        string         `json:"nodeId"`
	Kind          string         `json:"kind"`
	Area          string         `json:"area,omitempty"`
	Actor         string         `json:"actor,omitempty"`
	Object        string         `json:"object,omitempty"`
	Activity      string         `json:"activity,omitempty"`
	Priority      int            `json:"priority"`
	Configuration map[string]any `json:"configuration"`
}

type Analysis struct {
	Dependencies []Dependency      `json:"dependencies"`
	Identifiers  []IdentifierUsage `json:"identifiers"`
	Triggers     []Trigger         `json:"triggers"`
	Warnings     []string          `json:"warnings"`
}

func Decode(raw json.RawMessage) (Document, error) {
	var document Document
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("invalid script document: %w", err)
	}
	return document, nil
}

func Validate(document Document, publishing bool) (Analysis, error) {
	if document.Schema != Schema {
		return Analysis{}, fmt.Errorf("unsupported script schema %q", document.Schema)
	}
	if len(document.Nodes) == 0 || len(document.Nodes) > MaxNodes {
		return Analysis{}, fmt.Errorf("script must contain between 1 and %d nodes", MaxNodes)
	}
	if len(document.Edges) > MaxEdges {
		return Analysis{}, fmt.Errorf("script contains more than %d edges", MaxEdges)
	}
	nodes := make(map[string]Node, len(document.Nodes))
	triggerCount := 0
	endCount := 0
	unresolvedCount := 0
	for _, node := range document.Nodes {
		if !identifierPattern.MatchString(node.ID) {
			return Analysis{}, fmt.Errorf("invalid node id %q", node.ID)
		}
		if _, duplicate := nodes[node.ID]; duplicate {
			return Analysis{}, fmt.Errorf("duplicate node id %q", node.ID)
		}
		if _, ok := nodeTypes[node.Type]; !ok {
			return Analysis{}, fmt.Errorf("unsupported node type %q", node.Type)
		}
		if len(strings.TrimSpace(node.Label)) == 0 || len(node.Label) > 160 {
			return Analysis{}, fmt.Errorf("node %q has an invalid label", node.ID)
		}
		if node.Config == nil {
			return Analysis{}, fmt.Errorf("node %q config must be an object", node.ID)
		}
		nodes[node.ID] = node
		switch node.Type {
		case "trigger":
			triggerCount++
		case "end":
			endCount++
		case "unresolved_native":
			unresolvedCount++
		}
	}
	if _, ok := nodes[document.EntryNodeID]; !ok {
		return Analysis{}, errors.New("entryNodeId does not identify a node")
	}
	edges := make(map[string]struct{}, len(document.Edges))
	for _, edge := range document.Edges {
		if !identifierPattern.MatchString(edge.ID) {
			return Analysis{}, fmt.Errorf("invalid edge id %q", edge.ID)
		}
		if _, duplicate := edges[edge.ID]; duplicate {
			return Analysis{}, fmt.Errorf("duplicate edge id %q", edge.ID)
		}
		if _, ok := nodes[edge.From]; !ok {
			return Analysis{}, fmt.Errorf("edge %q has unknown source %q", edge.ID, edge.From)
		}
		if _, ok := nodes[edge.To]; !ok {
			return Analysis{}, fmt.Errorf("edge %q has unknown target %q", edge.ID, edge.To)
		}
		edges[edge.ID] = struct{}{}
	}
	analysis := analyze(document)
	if triggerCount == 0 {
		analysis.Warnings = append(analysis.Warnings, "script has no trigger node")
	}
	if endCount == 0 {
		analysis.Warnings = append(analysis.Warnings, "script has no end node")
	}
	if unresolvedCount > 0 {
		analysis.Warnings = append(
			analysis.Warnings,
			fmt.Sprintf("script contains %d unresolved native nodes", unresolvedCount),
		)
	}
	if publishing && (triggerCount == 0 || endCount == 0 || unresolvedCount > 0) {
		return analysis, errors.New("script is not publishable while structural warnings remain")
	}
	return analysis, nil
}

func analyze(document Document) Analysis {
	dependencyMap := map[string]Dependency{}
	triggers := make([]Trigger, 0)
	for _, node := range document.Nodes {
		kind, _ := node.Config["kind"].(string)
		key, _ := node.Config["key"].(string)
		if identifierPattern.MatchString(key) {
			var access string
			var dependencyKind string
			switch {
			case node.Type == "condition" && kind == "flag":
				access, dependencyKind = "read", "flag"
			case node.Type == "condition" && kind == "item":
				access, dependencyKind = "read", "item"
			case node.Type == "action" && kind == "set_flag":
				access, dependencyKind = "write", "flag"
			case node.Type == "action" && kind == "give_item":
				access, dependencyKind = "write", "item"
			case node.Type == "call":
				access, dependencyKind = "read", "script"
			}
			if access != "" {
				dependency := Dependency{Access: access, Kind: dependencyKind, Identifier: key}
				dependencyMap[access+"\x00"+dependencyKind+"\x00"+key] = dependency
			}
		}
		if node.Type == "trigger" {
			area, _ := node.Config["area"].(string)
			actor, _ := node.Config["actor"].(string)
			triggers = append(triggers, Trigger{
				NodeID: node.ID, Kind: kind, Area: area, Actor: actor,
				Configuration: node.Config,
			})
		}
	}
	dependencies := make([]Dependency, 0, len(dependencyMap))
	for _, dependency := range dependencyMap {
		dependencies = append(dependencies, dependency)
	}
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].Kind != dependencies[j].Kind {
			return dependencies[i].Kind < dependencies[j].Kind
		}
		if dependencies[i].Identifier != dependencies[j].Identifier {
			return dependencies[i].Identifier < dependencies[j].Identifier
		}
		return dependencies[i].Access < dependencies[j].Access
	})
	identifierMap := map[string]IdentifierUsage{}
	for _, dependency := range dependencies {
		identifier := IdentifierUsage{Kind: dependency.Kind, Identifier: dependency.Identifier}
		identifierMap[identifier.Kind+"\x00"+identifier.Identifier] = identifier
	}
	for _, trigger := range triggers {
		for _, identifier := range []IdentifierUsage{
			{Kind: "scene", Identifier: trigger.Area},
			{Kind: "actor", Identifier: trigger.Actor},
		} {
			if identifierPattern.MatchString(identifier.Identifier) {
				identifierMap[identifier.Kind+"\x00"+identifier.Identifier] = identifier
			}
		}
	}
	identifiers := make([]IdentifierUsage, 0, len(identifierMap))
	for _, identifier := range identifierMap {
		identifiers = append(identifiers, identifier)
	}
	sort.Slice(identifiers, func(i, j int) bool {
		if identifiers[i].Kind != identifiers[j].Kind {
			return identifiers[i].Kind < identifiers[j].Kind
		}
		return identifiers[i].Identifier < identifiers[j].Identifier
	})
	return Analysis{Dependencies: dependencies, Identifiers: identifiers, Triggers: triggers, Warnings: []string{}}
}
