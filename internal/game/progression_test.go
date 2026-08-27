package game

import "testing"

func TestProgressionThresholds(t *testing.T) {
	tests := []struct {
		experience int64
		level      int
	}{
		{0, 1},
		{99, 1},
		{100, 2},
		{249, 2},
		{250, 3},
		{3_199, 9},
		{3_200, MaxLevel},
		{1_000_000, MaxLevel},
	}
	for _, test := range tests {
		if got := LevelForExperience(test.experience); got != test.level {
			t.Fatalf("LevelForExperience(%d) = %d, want %d", test.experience, got, test.level)
		}
	}
}

func TestExperienceThresholdsRemainIncreasing(t *testing.T) {
	for level := 2; level <= MaxLevel; level++ {
		if ExperienceForLevel(level) <= ExperienceForLevel(level-1) {
			t.Fatalf("level %d threshold must exceed level %d", level, level-1)
		}
	}
	if got := ExperienceForLevel(MaxLevel + 1); got != ExperienceForLevel(MaxLevel) {
		t.Fatalf("threshold above max level = %d, want %d", got, ExperienceForLevel(MaxLevel))
	}
}
