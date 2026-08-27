package scriptcontent

import (
	"fmt"
	"sort"
	"strings"
)

// AnalyzeCalls validates official Yarn parse results against the pinned game
// registry. Resource identifiers must be static string literals so publication,
// dependency indexing, and runtime dispatch all agree before execution starts.
func AnalyzeCalls(calls []CompiledCall, registry CommandRegistry) (Analysis, []Diagnostic) {
	entries := make(map[string]CommandSchema, len(registry.Entries))
	for _, entry := range registry.Entries {
		entries[entry.Name] = entry
	}
	identifierPolicies := make(map[string]string, len(registry.IdentifierKinds))
	for _, kind := range registry.IdentifierKinds {
		identifierPolicies[kind.Kind] = kind.Policy
	}
	capabilities := make(map[string]bool, len(registry.Capabilities))
	for _, capability := range registry.Capabilities {
		capabilities[capability.Kind+"\x00"+capability.Identifier] = true
	}
	dependencies := make(map[string]Dependency)
	identifiers := make(map[string]IdentifierUsage)
	diagnostics := make([]Diagnostic, 0)
	for _, call := range calls {
		entry, known := entries[call.Name]
		if !known {
			diagnostics = append(diagnostics, callDiagnostic(call, "NYC0001", fmt.Sprintf("Unknown New Yokosuka %s %q", call.Kind, call.Name)))
			continue
		}
		if entry.Kind != call.Kind {
			diagnostics = append(diagnostics, callDiagnostic(call, "NYC0002", fmt.Sprintf("%q is registered as a %s, not a %s", call.Name, entry.Kind, call.Kind)))
			continue
		}
		if call.ParseError != nil {
			diagnostics = append(diagnostics, callDiagnostic(call, "NYC0003", "Invalid command arguments: "+*call.ParseError))
			continue
		}
		minimumArguments := len(entry.Parameters)
		for minimumArguments > 0 && entry.Parameters[minimumArguments-1].Optional {
			minimumArguments--
		}
		if len(call.Arguments) < minimumArguments || len(call.Arguments) > len(entry.Parameters) {
			diagnostics = append(diagnostics, callDiagnostic(call, "NYC0004", fmt.Sprintf(
				"%s expects %s but received %d",
				call.Name, argumentCountDescription(minimumArguments, len(entry.Parameters)), len(call.Arguments),
			)))
			continue
		}
		validArguments := true
		for index, argument := range call.Arguments {
			parameter := entry.Parameters[index]
			if call.Kind == "command" && !argument.IsStatic {
				diagnostics = append(diagnostics, callDiagnostic(call, "NYC0005", fmt.Sprintf(
					"Command parameter %s must be a literal %s value", parameter.Name, parameter.Type,
				)))
				validArguments = false
				continue
			}
			if argument.Type != parameter.Type {
				diagnostics = append(diagnostics, callDiagnostic(call, "NYC0006", fmt.Sprintf(
					"Parameter %s expects %s, not %s", parameter.Name, parameter.Type, argument.Type,
				)))
				validArguments = false
				continue
			}
			if parameter.IdentifierKind != "" && (!argument.IsStatic || argument.Value == nil || strings.TrimSpace(*argument.Value) == "") {
				diagnostics = append(diagnostics, callDiagnostic(call, "NYC0007", fmt.Sprintf(
					"Identifier parameter %s must be a non-empty string literal", parameter.Name,
				)))
				validArguments = false
				continue
			}
			if parameter.IdentifierKind != "" && argument.Value != nil &&
				identifierPolicies[parameter.IdentifierKind] == "closed" {
				identifier := strings.TrimSpace(*argument.Value)
				if !capabilities[parameter.IdentifierKind+"\x00"+identifier] {
					diagnostics = append(diagnostics, callDiagnostic(call, "NYC0008", fmt.Sprintf(
						"Unknown %s capability %q", parameter.IdentifierKind, identifier,
					)))
					validArguments = false
				}
			}
		}
		if !validArguments {
			continue
		}
		for index, parameter := range entry.Parameters {
			if parameter.IdentifierKind == "" || index >= len(call.Arguments) {
				continue
			}
			argument := call.Arguments[index]
			if !argument.IsStatic || argument.Value == nil {
				continue
			}
			identifier := strings.TrimSpace(*argument.Value)
			usage := IdentifierUsage{Kind: parameter.IdentifierKind, Identifier: identifier}
			identifiers[usage.Kind+"\x00"+usage.Identifier] = usage
		}
		parameterIndexes := make(map[string]int, len(entry.Parameters))
		for index, parameter := range entry.Parameters {
			parameterIndexes[parameter.Name] = index
		}
		for _, rule := range entry.Dependencies {
			index := parameterIndexes[rule.Parameter]
			if index >= len(call.Arguments) {
				continue
			}
			argument := call.Arguments[index]
			if !argument.IsStatic || argument.Value == nil {
				continue
			}
			identifier := strings.TrimSpace(*argument.Value)
			dependency := Dependency{Access: rule.Access, Kind: rule.Kind, Identifier: identifier}
			dependencies[rule.Access+"\x00"+rule.Kind+"\x00"+identifier] = dependency
		}
	}
	result := make([]Dependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		result = append(result, dependency)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Kind != result[right].Kind {
			return result[left].Kind < result[right].Kind
		}
		if result[left].Identifier != result[right].Identifier {
			return result[left].Identifier < result[right].Identifier
		}
		return result[left].Access < result[right].Access
	})
	identifierResult := make([]IdentifierUsage, 0, len(identifiers))
	for _, identifier := range identifiers {
		identifierResult = append(identifierResult, identifier)
	}
	sort.Slice(identifierResult, func(left, right int) bool {
		if identifierResult[left].Kind != identifierResult[right].Kind {
			return identifierResult[left].Kind < identifierResult[right].Kind
		}
		return identifierResult[left].Identifier < identifierResult[right].Identifier
	})
	return Analysis{Dependencies: result, Identifiers: identifierResult, Triggers: []Trigger{}, Warnings: []string{}}, diagnostics
}

func callDiagnostic(call CompiledCall, code, message string) Diagnostic {
	return Diagnostic{
		Severity: "error", Code: code, Message: message, FileName: call.FileName,
		Node: call.Node, Line: call.StartLine + 1, Column: call.StartColumn + 1,
		EndLine: call.EndLine + 1, EndColumn: call.EndColumn + 1,
	}
}

func argumentCountDescription(minimum, maximum int) string {
	if minimum == maximum {
		return fmt.Sprintf("%d arguments", maximum)
	}
	return fmt.Sprintf("between %d and %d arguments", minimum, maximum)
}
