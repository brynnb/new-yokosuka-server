package scriptcontent

import (
	"fmt"
	"regexp"
	"strings"
)

const MaxYarnTriggers = 32

var triggerSelectorPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,159}$`)

type TriggerSelector struct {
	Kind     string `json:"kind"`
	Area     string `json:"area,omitempty"`
	Actor    string `json:"actor,omitempty"`
	Object   string `json:"object,omitempty"`
	Activity string `json:"activity,omitempty"`
}

func NormalizeTriggerSelector(selector TriggerSelector) (TriggerSelector, error) {
	selector.Kind = strings.TrimSpace(selector.Kind)
	selector.Area = strings.TrimSpace(selector.Area)
	selector.Actor = strings.TrimSpace(selector.Actor)
	selector.Object = strings.TrimSpace(selector.Object)
	selector.Activity = strings.TrimSpace(selector.Activity)
	for label, value := range map[string]string{
		"area": selector.Area, "actor": selector.Actor,
		"object": selector.Object, "activity": selector.Activity,
	} {
		if value != "" && !triggerSelectorPattern.MatchString(value) {
			return TriggerSelector{}, fmt.Errorf("trigger %s %q is not a valid catalog identifier", label, value)
		}
	}
	if message := validateTriggerShape(Trigger{
		Kind: selector.Kind, Area: selector.Area, Actor: selector.Actor,
		Object: selector.Object, Activity: selector.Activity,
	}); message != "" {
		return TriggerSelector{}, fmt.Errorf("%s", message)
	}
	return selector, nil
}

func ValidateYarnTriggers(triggers []Trigger, nodes []CompiledNode) ([]Trigger, []Diagnostic) {
	nodeNames := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		nodeNames[node.Title] = true
	}
	diagnostics := make([]Diagnostic, 0)
	if len(triggers) > MaxYarnTriggers {
		diagnostics = append(diagnostics, triggerDiagnostic("", fmt.Sprintf("A script version may define at most %d triggers", MaxYarnTriggers)))
		return nil, diagnostics
	}
	normalized := make([]Trigger, 0, len(triggers))
	seenNodes := make(map[string]bool, len(triggers))
	for _, trigger := range triggers {
		trigger.NodeID = strings.TrimSpace(trigger.NodeID)
		selector, selectorErr := NormalizeTriggerSelector(TriggerSelector{
			Kind: trigger.Kind, Area: trigger.Area, Actor: trigger.Actor,
			Object: trigger.Object, Activity: trigger.Activity,
		})
		trigger.Kind, trigger.Area, trigger.Actor = selector.Kind, selector.Area, selector.Actor
		trigger.Object, trigger.Activity = selector.Object, selector.Activity
		trigger.Configuration = map[string]any{}
		if trigger.NodeID == "" || seenNodes[trigger.NodeID] {
			diagnostics = append(diagnostics, triggerDiagnostic(trigger.NodeID, "Each trigger must have a unique Yarn entry node"))
			continue
		}
		seenNodes[trigger.NodeID] = true
		if !nodeNames[trigger.NodeID] {
			diagnostics = append(diagnostics, triggerDiagnostic(trigger.NodeID, fmt.Sprintf("Trigger entry node %q does not exist in the compiled Yarn program", trigger.NodeID)))
		}
		if trigger.Priority < -1000 || trigger.Priority > 1000 {
			diagnostics = append(diagnostics, triggerDiagnostic(trigger.NodeID, "Trigger priority must be between -1000 and 1000"))
		}
		if selectorErr != nil {
			diagnostics = append(diagnostics, triggerDiagnostic(trigger.NodeID, selectorErr.Error()))
		}
		normalized = append(normalized, trigger)
	}
	return normalized, diagnostics
}

func validateTriggerShape(trigger Trigger) string {
	switch trigger.Kind {
	case "talk":
		if trigger.Area == "" || trigger.Actor == "" || trigger.Object != "" || trigger.Activity != "" {
			return "A talk trigger requires area and actor selectors only"
		}
	case "use", "inspect":
		if trigger.Area == "" || trigger.Object == "" || trigger.Actor != "" || trigger.Activity != "" {
			return fmt.Sprintf("A %s trigger requires area and object selectors only", trigger.Kind)
		}
	case "enter", "automatic":
		if trigger.Area == "" || trigger.Actor != "" || trigger.Object != "" || trigger.Activity != "" {
			return fmt.Sprintf("An %s trigger requires an area selector only", trigger.Kind)
		}
	case "activity":
		if trigger.Activity == "" || trigger.Area != "" || trigger.Actor != "" || trigger.Object != "" {
			return "An activity trigger requires an activity selector only"
		}
	default:
		return fmt.Sprintf("Unsupported trigger kind %q", trigger.Kind)
	}
	return ""
}

func triggerDiagnostic(node, message string) Diagnostic {
	return Diagnostic{Severity: "error", Code: "NYT0001", Message: message, Node: node}
}
