package travelaccess

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

const Schema = "new-yokosuka-timed-transition-access-v1"

//go:embed rules.json
var embeddedRules []byte

type Source struct {
	WorldID                   string     `json:"worldId"`
	Area                      string     `json:"area"`
	DoorSelector              int        `json:"doorSelector"`
	Layer                     int        `json:"layer"`
	Position                  [3]float64 `json:"position"`
	MaximumHorizontalDistance float64    `json:"maximumHorizontalDistance"`
}

type Destination struct {
	WorldID string `json:"worldId"`
	Area    string `json:"area"`
	Entry   int    `json:"entry"`
}

type OpenWindow struct {
	Kind            string `json:"kind"`
	StartMinute     int    `json:"startMinute"`
	EndMinute       int    `json:"endMinute"`
	DayBoundaryHour int    `json:"dayBoundaryHour"`
	Boundary        string `json:"boundary"`
}

type StoryFallback struct {
	Status string `json:"status"`
	Policy string `json:"policy"`
}

type Evidence struct {
	OverlayLayer            int    `json:"overlayLayer"`
	DispatchKind            string `json:"dispatchKind"`
	SourceDoorModel         string `json:"sourceDoorModel"`
	SelectorCompareOffset   string `json:"selectorCompareFileOffset"`
	CoroutineCallFileOffset string `json:"coroutineCallFileOffset"`
}

type Rule struct {
	ID                      string        `json:"id"`
	TransitionID            string        `json:"transitionId"`
	Source                  Source        `json:"source"`
	Destination             Destination   `json:"destination"`
	OpenWindow              OpenWindow    `json:"openWindow"`
	AuthorizationLifetimeMs int64         `json:"authorizationLifetimeMs"`
	DenialMessage           string        `json:"denialMessage"`
	AccessDomain            string        `json:"accessDomain"`
	PersonalStoryFallback   StoryFallback `json:"personalStoryFallback"`
	Evidence                Evidence      `json:"evidence"`
}

type GeneratedSource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type PresentationOnlyAssociation struct {
	SourceWorldID string     `json:"sourceWorldId"`
	Layer         int        `json:"layer"`
	DoorSelector  int        `json:"doorSelector"`
	DispatchKind  string     `json:"dispatchKind"`
	Destination   any        `json:"destination"`
	OpenWindow    OpenWindow `json:"openWindow"`
}

type Manifest struct {
	Schema                           string                        `json:"schema"`
	GeneratedFrom                    []GeneratedSource             `json:"generatedFrom"`
	AlwaysAccessibleInteriorWorldIDs []string                      `json:"alwaysAccessibleInteriorWorldIds"`
	AlwaysAccessibleInteriorPolicy   string                        `json:"alwaysAccessibleInteriorPolicy"`
	Rules                            []Rule                        `json:"rules"`
	PresentationOnlyAssociations     []PresentationOnlyAssociation `json:"presentationOnlyAssociations"`
	EvidenceBoundary                 string                        `json:"evidenceBoundary"`
}

type Catalog struct {
	manifest                 Manifest
	byTransitionID           map[string]Rule
	controlledWorldIDs       map[string]struct{}
	alwaysAccessibleWorldIDs map[string]struct{}
}

func Load(data []byte) (*Catalog, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode timed transition access rules: %w", err)
	}
	if manifest.Schema != Schema {
		return nil, fmt.Errorf("unsupported timed transition access schema %q", manifest.Schema)
	}
	if len(manifest.Rules) == 0 {
		return nil, fmt.Errorf("timed transition access manifest has no rules")
	}
	catalog := &Catalog{
		manifest:                 manifest,
		byTransitionID:           make(map[string]Rule, len(manifest.Rules)),
		controlledWorldIDs:       make(map[string]struct{}),
		alwaysAccessibleWorldIDs: make(map[string]struct{}, len(manifest.AlwaysAccessibleInteriorWorldIDs)),
	}
	for _, worldID := range manifest.AlwaysAccessibleInteriorWorldIDs {
		if strings.TrimSpace(worldID) == "" || strings.TrimSpace(worldID) != worldID {
			return nil, fmt.Errorf("always-accessible interior has invalid world id %q", worldID)
		}
		if _, exists := catalog.alwaysAccessibleWorldIDs[worldID]; exists {
			return nil, fmt.Errorf("duplicate always-accessible interior %q", worldID)
		}
		catalog.alwaysAccessibleWorldIDs[worldID] = struct{}{}
	}
	if len(catalog.alwaysAccessibleWorldIDs) == 0 || strings.TrimSpace(manifest.AlwaysAccessibleInteriorPolicy) == "" {
		return nil, fmt.Errorf("world access policy has no always-accessible interiors")
	}
	for _, rule := range manifest.Rules {
		if err := validateRule(rule); err != nil {
			return nil, err
		}
		if _, exists := catalog.byTransitionID[rule.TransitionID]; exists {
			return nil, fmt.Errorf("duplicate timed transition %q", rule.TransitionID)
		}
		catalog.byTransitionID[rule.TransitionID] = rule
		if !catalog.IsAlwaysAccessible(rule.Destination.WorldID) {
			catalog.controlledWorldIDs[rule.Destination.WorldID] = struct{}{}
		}
	}
	return catalog, nil
}

func MustLoad() *Catalog {
	catalog, err := Load(embeddedRules)
	if err != nil {
		panic(err)
	}
	return catalog
}

func validateRule(rule Rule) error {
	if strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.TransitionID) == "" {
		return fmt.Errorf("timed transition rule is missing an id")
	}
	if rule.Source.WorldID == "" || rule.Source.Area == "" ||
		rule.Destination.WorldID == "" || rule.Destination.Area == "" {
		return fmt.Errorf("timed transition %q has an incomplete route", rule.TransitionID)
	}
	for _, coordinate := range rule.Source.Position {
		if math.IsNaN(coordinate) || math.IsInf(coordinate, 0) {
			return fmt.Errorf("timed transition %q has a non-finite source position", rule.TransitionID)
		}
	}
	if rule.Source.MaximumHorizontalDistance <= 0 ||
		math.IsNaN(rule.Source.MaximumHorizontalDistance) ||
		math.IsInf(rule.Source.MaximumHorizontalDistance, 0) {
		return fmt.Errorf("timed transition %q has an invalid proximity limit", rule.TransitionID)
	}
	window := rule.OpenWindow
	if window.Kind != "native-clock-window" ||
		window.Boundary != "start-inclusive, end-exclusive" ||
		window.DayBoundaryHour < 0 || window.DayBoundaryHour > 23 ||
		window.StartMinute < 0 || window.EndMinute <= window.StartMinute ||
		window.EndMinute > 2*24*60 {
		return fmt.Errorf("timed transition %q has an invalid opening window", rule.TransitionID)
	}
	if rule.AuthorizationLifetimeMs <= 0 || strings.TrimSpace(rule.DenialMessage) == "" {
		return fmt.Errorf("timed transition %q has an invalid authorization policy", rule.TransitionID)
	}
	return nil
}

func (c *Catalog) Manifest() Manifest {
	return c.manifest
}

func (c *Catalog) Rule(transitionID string) (Rule, bool) {
	rule, ok := c.byTransitionID[transitionID]
	return rule, ok
}

func (c *Catalog) ControlsDestination(worldID string) bool {
	_, ok := c.controlledWorldIDs[worldID]
	return ok
}

func (c *Catalog) IsAlwaysAccessible(worldID string) bool {
	_, ok := c.alwaysAccessibleWorldIDs[worldID]
	return ok
}

func (c *Catalog) IsRuleOpen(rule Rule, gameTime time.Time) bool {
	return c.IsAlwaysAccessible(rule.Destination.WorldID) || rule.IsOpen(gameTime)
}

func (r Rule) IsOpen(gameTime time.Time) bool {
	gameTime = gameTime.UTC()
	sinceBoundary := time.Duration(gameTime.Hour())*time.Hour +
		time.Duration(gameTime.Minute())*time.Minute +
		time.Duration(gameTime.Second())*time.Second +
		time.Duration(gameTime.Nanosecond())
	if gameTime.Hour() < r.OpenWindow.DayBoundaryHour {
		sinceBoundary += 24 * time.Hour
	}
	start := time.Duration(r.OpenWindow.StartMinute) * time.Minute
	end := time.Duration(r.OpenWindow.EndMinute) * time.Minute
	return start <= sinceBoundary && sinceBoundary < end
}

func (r Rule) WithinRange(x, z float64) bool {
	dx := x - r.Source.Position[0]
	dz := z - r.Source.Position[2]
	maximum := r.Source.MaximumHorizontalDistance
	return dx*dx+dz*dz <= maximum*maximum
}

func (r Rule) AuthorizationLifetime() time.Duration {
	return time.Duration(r.AuthorizationLifetimeMs) * time.Millisecond
}
