package game

const (
	MaxLevel      = 10
	StartingMaxHP = 100
)

// levelExperience contains New Yokosuka's initial progression curve. Keeping
// the thresholds explicit makes them easy to rebalance without importing an
// RPG ruleset from another game.
var levelExperience = [...]int64{
	0,
	100,
	250,
	450,
	700,
	1_000,
	1_400,
	1_900,
	2_500,
	3_200,
}

func ExperienceForLevel(level int) int64 {
	if level <= 1 {
		return levelExperience[0]
	}
	if level > MaxLevel {
		level = MaxLevel
	}
	return levelExperience[level-1]
}

func LevelForExperience(experience int64) int {
	if experience <= 0 {
		return 1
	}
	for level := 2; level <= MaxLevel; level++ {
		if experience < ExperienceForLevel(level) {
			return level - 1
		}
	}
	return MaxLevel
}
