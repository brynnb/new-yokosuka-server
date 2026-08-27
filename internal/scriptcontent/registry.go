package scriptcontent

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

var (
	registryNamePattern   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	identifierKindPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
)

//go:embed commands.v1.json
var commandRegistryJSON []byte

type CommandRegistry struct {
	Schema          string                 `json:"schema"`
	Version         string                 `json:"version"`
	IdentifierKinds []IdentifierKindSchema `json:"identifierKinds"`
	Capabilities    []IdentifierDefinition `json:"capabilities"`
	Entries         []CommandSchema        `json:"entries"`
}

type IdentifierKindSchema struct {
	Kind   string `json:"kind"`
	Policy string `json:"policy"`
}

type IdentifierDefinition struct {
	Kind        string `json:"kind"`
	Identifier  string `json:"identifier"`
	Description string `json:"description"`
}

type CommandSchema struct {
	Name         string             `json:"name"`
	Kind         string             `json:"kind"`
	Description  string             `json:"description"`
	Parameters   []CommandParameter `json:"parameters"`
	ReturnType   string             `json:"returnType,omitempty"`
	Authority    string             `json:"authority"`
	Wait         string             `json:"wait"`
	Cleanup      string             `json:"cleanup"`
	SideEffects  []string           `json:"sideEffects,omitempty"`
	Dependencies []DependencyRule   `json:"dependencies"`
}

type CommandParameter struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	IdentifierKind string `json:"identifierKind,omitempty"`
	Optional       bool   `json:"optional,omitempty"`
}

type DependencyRule struct {
	Access    string `json:"access"`
	Kind      string `json:"kind"`
	Parameter string `json:"parameter"`
}

func Registry() (CommandRegistry, error) {
	var registry CommandRegistry
	if err := json.Unmarshal(commandRegistryJSON, &registry); err != nil {
		return CommandRegistry{}, fmt.Errorf("decode command registry: %w", err)
	}
	if err := validateRegistry(registry); err != nil {
		return CommandRegistry{}, err
	}
	return registry, nil
}

func validateRegistry(registry CommandRegistry) error {
	if registry.Schema != "new-yokosuka-command-registry-v1" {
		return errors.New("unexpected command registry schema")
	}
	if registry.Version != YarnCommandSchemaVersion {
		return errors.New("command registry version does not match pinned Yarn schema")
	}
	kindPolicies := make(map[string]string, len(registry.IdentifierKinds))
	for _, kind := range registry.IdentifierKinds {
		if !identifierKindPattern.MatchString(kind.Kind) ||
			(kind.Policy != "open" && kind.Policy != "closed") ||
			kindPolicies[kind.Kind] != "" {
			return fmt.Errorf("invalid or duplicate identifier kind %q", kind.Kind)
		}
		kindPolicies[kind.Kind] = kind.Policy
	}
	capabilities := make(map[string]bool, len(registry.Capabilities))
	closedKinds := make(map[string]bool)
	for _, capability := range registry.Capabilities {
		key := capability.Kind + "\x00" + capability.Identifier
		if kindPolicies[capability.Kind] != "closed" ||
			capability.Identifier == "" || capability.Description == "" || capabilities[key] {
			return fmt.Errorf("invalid or duplicate closed capability %q/%q", capability.Kind, capability.Identifier)
		}
		capabilities[key] = true
		closedKinds[capability.Kind] = true
	}
	for kind, policy := range kindPolicies {
		if policy == "closed" && !closedKinds[kind] {
			return fmt.Errorf("closed identifier kind %q has no capabilities", kind)
		}
	}
	seen := make(map[string]bool, len(registry.Entries))
	for _, entry := range registry.Entries {
		if !registryNamePattern.MatchString(entry.Name) || seen[entry.Name] {
			return fmt.Errorf("invalid or duplicate registry entry %q", entry.Name)
		}
		seen[entry.Name] = true
		if entry.Kind != "command" && entry.Kind != "function" {
			return fmt.Errorf("%s has invalid kind %q", entry.Name, entry.Kind)
		}
		if entry.Kind == "function" && entry.ReturnType == "" {
			return fmt.Errorf("function %s has no return type", entry.Name)
		}
		if entry.Authority != "server" && entry.Authority != "server-orchestrated-presentation" {
			return fmt.Errorf("%s has invalid authority %q", entry.Name, entry.Authority)
		}
		if entry.Wait != "none" && entry.Wait != "client-ack" {
			return fmt.Errorf("%s has unsupported wait contract %q", entry.Name, entry.Wait)
		}
		if entry.Cleanup != "none" && entry.Cleanup != "transactional" &&
			entry.Cleanup != "event-owned" && entry.Cleanup != "restore-previous" {
			return fmt.Errorf("%s has invalid cleanup contract %q", entry.Name, entry.Cleanup)
		}
		if entry.Kind == "function" && (entry.Authority != "server" || entry.Wait != "none" || entry.Cleanup != "none") {
			return fmt.Errorf("function %s must be an immediate server query", entry.Name)
		}
		if entry.Authority == "server-orchestrated-presentation" &&
			(entry.Kind != "command" || entry.Wait != "client-ack" || entry.Cleanup == "none") {
			return fmt.Errorf("presentation command %s must wait for a client acknowledgment and declare cleanup", entry.Name)
		}
		parameters := map[string]bool{}
		optionalSeen := false
		for _, parameter := range entry.Parameters {
			if !registryNamePattern.MatchString(parameter.Name) || parameters[parameter.Name] {
				return fmt.Errorf("%s has invalid or duplicate parameter %q", entry.Name, parameter.Name)
			}
			parameters[parameter.Name] = true
			if parameter.Optional {
				optionalSeen = true
			} else if optionalSeen {
				return fmt.Errorf("%s has required parameter %q after an optional parameter", entry.Name, parameter.Name)
			}
			if parameter.Type != "string" && parameter.Type != "number" && parameter.Type != "bool" {
				return fmt.Errorf("%s.%s has invalid type %q", entry.Name, parameter.Name, parameter.Type)
			}
			if parameter.IdentifierKind != "" && (parameter.Type != "string" || !identifierKindPattern.MatchString(parameter.IdentifierKind)) {
				return fmt.Errorf("%s.%s has invalid identifier kind %q", entry.Name, parameter.Name, parameter.IdentifierKind)
			}
			if parameter.IdentifierKind != "" && kindPolicies[parameter.IdentifierKind] == "" {
				return fmt.Errorf("%s.%s uses undeclared identifier kind %q", entry.Name, parameter.Name, parameter.IdentifierKind)
			}
		}
		for _, dependency := range entry.Dependencies {
			if !parameters[dependency.Parameter] {
				return fmt.Errorf("%s dependency refers to missing parameter %q", entry.Name, dependency.Parameter)
			}
		}
	}
	return nil
}
