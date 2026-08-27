package worldstate

import (
	"testing"
	"time"
)

func TestClockUsesShenmueRateAndSkipsBedtimeToEightThirtyAM(t *testing.T) {
	epoch := time.UnixMilli(1_700_000_000_000)
	clock, err := NewClock(epoch, "summer")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		offset   time.Duration
		day      int64
		index    int
		name     string
		progress float64
	}{
		{0, 0, 1, "sunset", 0},
		{time.Minute, 0, 1, "sunset", 1.0 / 60.0},
		{2 * time.Minute, 0, 0, "day", 2.0 / 60.0},
		{40 * time.Minute, 0, 1, "sunset", 40.0 / 60.0},
		{44 * time.Minute, 0, 2, "evening", 44.0 / 60.0},
		{48 * time.Minute, 0, 3, "night", 48.0 / 60.0},
		{51 * time.Minute, 0, 3, "night", 51.0 / 60.0},
		{60 * time.Minute, 1, 1, "sunset", 0},
	}
	for _, test := range tests {
		clock.SetNowForTest(func() time.Time { return epoch.Add(test.offset) })
		state := clock.Snapshot()
		if state.DayNumber != test.day || state.TimeOfDayIndex != test.index ||
			state.TimeOfDay != test.name || state.DayProgress != test.progress {
			t.Fatalf("offset %s produced %#v", test.offset, state)
		}
		if state.DayLengthMs != ShenmueDayLength.Milliseconds() ||
			state.DayStartHour != gameDayStartHour ||
			state.DayEndHour != gameDayEndHour ||
			state.EpochMs != epoch.UnixMilli() ||
			state.Season != "summer" || state.SeasonIndex != 0 ||
			state.Weather != "clear" || state.WeatherIndex != 0 {
			t.Fatalf("unexpected stable clock fields: %#v", state)
		}
		expectedGameTime := gameCalendarStart.
			AddDate(0, 0, int(test.day)).
			Add(time.Duration(test.progress * float64(15*time.Hour)))
		if state.GameTimeMs != expectedGameTime.UnixMilli() {
			t.Fatalf(
				"offset %s produced game time %s, expected %s",
				test.offset,
				time.UnixMilli(state.GameTimeMs),
				expectedGameTime,
			)
		}
	}
}

func TestSunsetAlwaysStartsAtSixPM(t *testing.T) {
	tests := []struct {
		date time.Time
		want int
	}{
		{time.Date(1986, time.June, 9, 17, 59, 0, 0, time.UTC), 0},
		{time.Date(1986, time.June, 9, 18, 30, 0, 0, time.UTC), 1},
		{time.Date(1986, time.November, 29, 17, 59, 0, 0, time.UTC), 0},
		{time.Date(1986, time.November, 29, 18, 30, 0, 0, time.UTC), 1},
	}
	for _, test := range tests {
		if got := timeOfDayForTime(test.date); got != test.want {
			t.Fatalf("time of day for %s was %d, expected %d", test.date, got, test.want)
		}
	}
}

func TestBedtimeRolloverUsesTheNextCalendarDate(t *testing.T) {
	epoch := time.UnixMilli(1_700_000_000_000)
	clock, err := NewClock(epoch, "summer")
	if err != nil {
		t.Fatal(err)
	}
	clock.SetNowForTest(func() time.Time { return epoch.Add(60 * time.Minute) })

	got := time.UnixMilli(clock.Snapshot().GameTimeMs)
	want := time.Date(1986, time.June, 10, 8, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("bedtime rollover produced %s, expected %s", got, want)
	}
}

func TestClockRolloverCrossesMonthAndYearBoundaries(t *testing.T) {
	epoch := time.UnixMilli(1_700_000_000_000)
	clock, err := NewClock(epoch, "summer")
	if err != nil {
		t.Fatal(err)
	}
	activeHours := time.Duration(
		(gameDayEndHour - gameDayStartHour) * float64(time.Hour),
	)
	activeDayLength := time.Duration(
		float64(ShenmueDayLength) * float64(activeHours) / float64(24*time.Hour),
	)
	for _, want := range []time.Time{
		time.Date(1986, time.July, 1, 8, 30, 0, 0, time.UTC),
		time.Date(1987, time.January, 1, 8, 30, 0, 0, time.UTC),
	} {
		day := int(want.Sub(gameCalendarStart) / (24 * time.Hour))
		clock.SetNowForTest(func() time.Time {
			return epoch.Add(time.Duration(day) * activeDayLength)
		})
		got := time.UnixMilli(clock.Snapshot().GameTimeMs).UTC()
		if !got.Equal(want) {
			t.Fatalf("calendar rollover produced %s, want %s", got, want)
		}
	}
}

func TestClockSeasonValidationAndRevision(t *testing.T) {
	clock, err := newClock(time.Now(), time.Minute, "SUMMER")
	if err != nil {
		t.Fatal(err)
	}
	before := clock.Snapshot().Revision
	if err := clock.SetSeason("winter"); err != nil {
		t.Fatal(err)
	}
	after := clock.Snapshot()
	if after.Season != "winter" || after.SeasonIndex != 1 || after.Revision != before+1 {
		t.Fatalf("unexpected winter state: %#v", after)
	}
	if err := clock.SetSeason("monsoon"); err == nil {
		t.Fatal("expected invalid season to fail")
	}
	if _, err := newClock(time.Now(), 0, "summer"); err == nil {
		t.Fatal("expected invalid day length to fail")
	}
}

func TestClockSetGameSecondPreservesCalendarDay(t *testing.T) {
	epoch := time.UnixMilli(1_700_000_000_000)
	now := epoch.Add(75 * time.Minute)
	clock, err := NewClock(epoch, "summer")
	if err != nil {
		t.Fatal(err)
	}
	clock.SetNowForTest(func() time.Time { return now })
	before := clock.Snapshot()
	state, err := clock.SetGameSecond(12*60*60 + 40*60)
	if err != nil {
		t.Fatal(err)
	}
	got := time.UnixMilli(state.GameTimeMs).UTC()
	if state.DayNumber != before.DayNumber || got.Hour() != 12 || got.Minute() != 40 {
		t.Fatalf("unexpected adjusted clock: %#v (%s)", state, got)
	}
	if state.Revision != before.Revision+1 {
		t.Fatalf("expected revision %d, got %d", before.Revision+1, state.Revision)
	}
	if _, err := clock.SetGameSecond(8 * 60 * 60); err == nil {
		t.Fatal("expected time before the playable day to be rejected")
	}
}

func TestClockWeatherValidationAndRevision(t *testing.T) {
	clock, err := NewClock(time.Now(), "summer")
	if err != nil {
		t.Fatal(err)
	}
	before := clock.Snapshot().Revision
	if err := clock.SetWeather("SNOW"); err != nil {
		t.Fatal(err)
	}
	after := clock.Snapshot()
	if after.Weather != "snow" || after.WeatherIndex != 3 || after.Revision != before+1 {
		t.Fatalf("unexpected snow state: %#v", after)
	}
	if err := clock.SetWeather("monsoon"); err == nil {
		t.Fatal("expected invalid weather to fail")
	}
}
