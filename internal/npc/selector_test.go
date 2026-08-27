package npc

import (
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
)

func selectorTestActor() npcdata.Actor {
	slots := make([]npcdata.SelectorSlot, 16)
	for index := range slots {
		slots[index] = npcdata.SelectorSlot{
			SelectorIndex:      index,
			RawSchedulePointer: "base",
		}
	}
	return npcdata.Actor{
		InstanceID:               "TEST:selector",
		DefaultScheduleVariantID: "base",
		ScheduleSelector: npcdata.ScheduleSelector{
			PointerSlots: slots,
			Conditions: []npcdata.ScheduleCondition{{
				RequiredSetFlags:     []int{20},
				RequiredClearFlags:   []int{100},
				StartMonth:           12,
				StartDay:             26,
				RequiredBaseSelector: -1,
				TargetSelectorIndex:  4,
			}},
		},
		ScheduleVariants: []npcdata.ScheduleVariant{
			{ScheduleVariantID: "base", SelectorIndices: []int{1}},
			{ScheduleVariantID: "winter", SelectorIndices: []int{4}},
		},
	}
}

func TestSelectScheduleVariantUsesCalendarAndStoryConditions(t *testing.T) {
	actor := selectorTestActor()
	tests := []struct {
		date  time.Time
		flags map[int]bool
		want  string
	}{
		{time.Date(1986, 12, 25, 8, 30, 0, 0, time.UTC), map[int]bool{20: true}, "base"},
		{time.Date(1986, 12, 26, 8, 30, 0, 0, time.UTC), map[int]bool{20: true}, "winter"},
		{time.Date(1986, 12, 26, 8, 30, 0, 0, time.UTC), map[int]bool{}, "base"},
		{time.Date(1986, 12, 26, 8, 30, 0, 0, time.UTC), map[int]bool{20: true, 100: true}, "base"},
	}
	for _, test := range tests {
		got, err := SelectScheduleVariant(actor, test.date, ScheduleSelectorState{
			BaseSelector: 1,
			StoryFlags:   test.flags,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("%s flags %#v selected %s, want %s", test.date, test.flags, got, test.want)
		}
	}
}

func TestSelectScheduleVariantNormalizesAliasedBaseSlots(t *testing.T) {
	actor := selectorTestActor()
	actor.ScheduleVariants[0].SelectorIndices = []int{1, 2, 3}
	for _, base := range []int{2, 3, 4} {
		got, err := SelectScheduleVariant(
			actor,
			time.Date(1986, 6, 9, 8, 30, 0, 0, time.UTC),
			ScheduleSelectorState{BaseSelector: base, StoryFlags: map[int]bool{}},
		)
		if err != nil {
			t.Fatal(err)
		}
		if got != "base" {
			t.Fatalf("base selector %d selected %s", base, got)
		}
	}
}

func TestScheduleDateBoundsUseMonthDayTuples(t *testing.T) {
	if dateBefore(5, 1, 4, 15) {
		t.Fatal("May 1 incorrectly matched an April 15 end bound")
	}
	if !dateBefore(4, 14, 4, 15) {
		t.Fatal("April 14 did not match an April 15 end bound")
	}
}
