package npc

import (
	"fmt"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
)

type ScheduleSelectorState struct {
	BaseSelector int
	StoryFlags   map[int]bool
}

func normalizedBaseSelector(selector npcdata.ScheduleSelector, requested int) int {
	pointers := make(map[int]string, len(selector.PointerSlots))
	for _, slot := range selector.PointerSlots {
		pointers[slot.SelectorIndex] = slot.RawSchedulePointer
	}
	if requested == 2 || requested == 4 {
		if pointers[0] == pointers[2] {
			if requested == 4 {
				return 1
			}
			return requested
		}
		return 2
	}
	if requested == 3 && pointers[0] == pointers[3] {
		return 1
	}
	return requested
}

func dateAtOrAfter(month, day, boundMonth, boundDay int) bool {
	if boundMonth == 0 || boundDay == 0 {
		return true
	}
	return month > boundMonth || month == boundMonth && day >= boundDay
}

func dateBefore(month, day, boundMonth, boundDay int) bool {
	if boundMonth == 0 || boundDay == 0 {
		return true
	}
	return month < boundMonth || month == boundMonth && day < boundDay
}

func conditionMatches(
	condition npcdata.ScheduleCondition,
	base, month, day int,
	flags map[int]bool,
) bool {
	if condition.RequiredBaseSelector >= 0 && condition.RequiredBaseSelector != base {
		return false
	}
	if !dateAtOrAfter(month, day, condition.StartMonth, condition.StartDay) ||
		!dateBefore(month, day, condition.EndMonth, condition.EndDay) {
		return false
	}
	for _, flag := range condition.RequiredSetFlags {
		if !flags[flag] {
			return false
		}
	}
	for _, flag := range condition.RequiredClearFlags {
		if flags[flag] {
			return false
		}
	}
	return true
}

func SelectScheduleVariant(
	actor npcdata.Actor,
	gameDate time.Time,
	state ScheduleSelectorState,
) (string, error) {
	if len(actor.ScheduleVariants) == 0 {
		if actor.ScheduleVariantID != "" {
			return actor.ScheduleVariantID, nil
		}
		return "default", nil
	}
	base := normalizedBaseSelector(actor.ScheduleSelector, state.BaseSelector)
	selectedIndex := base
	month, day := int(gameDate.UTC().Month()), gameDate.UTC().Day()
	for _, condition := range actor.ScheduleSelector.Conditions {
		if conditionMatches(condition, base, month, day, state.StoryFlags) {
			selectedIndex = condition.TargetSelectorIndex
			break
		}
	}
	for _, variant := range actor.ScheduleVariants {
		for _, selectorIndex := range variant.SelectorIndices {
			if selectorIndex == selectedIndex {
				return variant.ScheduleVariantID, nil
			}
		}
	}
	for _, variant := range actor.ScheduleVariants {
		if variant.ScheduleVariantID == actor.DefaultScheduleVariantID {
			return variant.ScheduleVariantID, nil
		}
	}
	return "", fmt.Errorf("NPC %s has no selectable schedule variant", actor.InstanceID)
}
