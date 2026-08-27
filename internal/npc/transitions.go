package npc

import "math"

// ClassifyDiscontinuity names a visible position jump so protocol consumers
// can distinguish authored relocations from ordinary route projection.
func ClassifyDiscontinuity(previous, current State) (string, float64) {
	if previous.ID == "" ||
		!previous.Visible() || !current.Visible() ||
		previous.WorldID != current.WorldID {
		return "", 0
	}
	distance := previous.Position.horizontalDistance(current.Position)
	effectiveDelta := current.EffectiveSecond - previous.EffectiveSecond
	if effectiveDelta < 0 || effectiveDelta > gameDaySeconds {
		effectiveDelta = 0
	}
	expected := math.Max(
		previous.SpeedPerGameSecond,
		current.SpeedPerGameSecond,
	)*math.Max(1, effectiveDelta) + 0.05
	if distance <= expected {
		return "", 0
	}
	switch {
	case previous.Operation == 0x1c && current.Operation == 0x1c &&
		previous.Visual.SecondaryObjectCode != current.Visual.SecondaryObjectCode:
		return "authored-secondary-object-switch", distance
	case previous.Operation == 0x1c && current.Operation == 0x1c:
		return "native-secondary-controller-snap", distance
	default:
		return "authored-schedule-discontinuity", distance
	}
}
