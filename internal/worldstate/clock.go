package worldstate

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

// Shenmue I advances one in-game hour every four real minutes.
const ShenmueDayLength = 96 * time.Minute

const (
	gameDayStartHour = 8.5
	gameDayEndHour   = 23.5
	sunsetStartHour  = 18.0

	sunriseToDayHours    = 0.75
	dayToSunsetHours     = 1.0
	sunsetToEveningHours = 0.75
	eveningToNightHours  = 0.75
)

var timeOfDayNames = [...]string{"day", "sunset", "evening", "night"}
var weatherNames = [...]string{"clear", "overcast", "rain", "snow"}

var gameCalendarStart = time.Date(
	1986,
	time.June,
	9,
	8,
	30,
	0,
	0,
	time.UTC,
)

func timeOfDayForTime(gameTime time.Time) int {
	gameHour := float64(gameTime.Hour()) +
		float64(gameTime.Minute())/60 +
		float64(gameTime.Second())/3600
	switch {
	case gameHour < gameDayStartHour:
		return 3
	case gameHour < gameDayStartHour+sunriseToDayHours/2:
		return 1
	case gameHour < sunsetStartHour+dayToSunsetHours/2:
		return 0
	case gameHour < sunsetStartHour+dayToSunsetHours+
		sunsetToEveningHours/2:
		return 1
	case gameHour < sunsetStartHour+dayToSunsetHours+
		sunsetToEveningHours+eveningToNightHours/2:
		return 2
	default:
		return 3
	}
}

type Clock struct {
	mu        sync.RWMutex
	epoch     time.Time
	dayLength time.Duration
	season    string
	weather   string
	revision  uint64
	now       func() time.Time
}

func NewClock(epoch time.Time, season string) (*Clock, error) {
	return newClock(epoch, ShenmueDayLength, season)
}

func newClock(epoch time.Time, dayLength time.Duration, season string) (*Clock, error) {
	if dayLength <= 0 {
		return nil, fmt.Errorf("day length must be positive")
	}
	normalizedSeason := strings.ToLower(strings.TrimSpace(season))
	if normalizedSeason != "summer" && normalizedSeason != "winter" {
		return nil, fmt.Errorf("season must be summer or winter")
	}
	return &Clock{
		epoch:     epoch,
		dayLength: dayLength,
		season:    normalizedSeason,
		weather:   "clear",
		revision:  1,
		now:       time.Now,
	}, nil
}

func (c *Clock) Snapshot() protocol.WorldState {
	c.mu.RLock()
	now := c.now()
	epoch := c.epoch
	dayLength := c.dayLength
	season := c.season
	weather := c.weather
	revision := c.revision
	c.mu.RUnlock()

	elapsed := now.Sub(epoch)
	if elapsed < 0 {
		elapsed = 0
	}
	activeHours := time.Duration(
		(gameDayEndHour - gameDayStartHour) * float64(time.Hour),
	)
	activeDayLength := time.Duration(
		float64(dayLength) * float64(activeHours) / float64(24*time.Hour),
	)
	dayNumber := int64(elapsed / activeDayLength)
	withinDay := elapsed % activeDayLength
	progress := float64(withinDay) / float64(activeDayLength)
	seasonIndex := 0
	if season == "winter" {
		seasonIndex = 1
	}
	weatherIndex := 0
	for index, name := range weatherNames {
		if weather == name {
			weatherIndex = index
			break
		}
	}
	gameTime := gameCalendarStart.AddDate(0, 0, int(dayNumber)).Add(
		time.Duration(progress * float64(activeHours)),
	)
	timeIndex := timeOfDayForTime(gameTime)

	return protocol.WorldState{
		ServerTimeMs:   now.UnixMilli(),
		GameTimeMs:     gameTime.UnixMilli(),
		EpochMs:        epoch.UnixMilli(),
		DayLengthMs:    dayLength.Milliseconds(),
		DayStartHour:   gameDayStartHour,
		DayEndHour:     gameDayEndHour,
		DayNumber:      dayNumber,
		DayProgress:    progress,
		TimeOfDay:      timeOfDayNames[timeIndex],
		TimeOfDayIndex: timeIndex,
		Season:         season,
		SeasonIndex:    seasonIndex,
		Weather:        weather,
		WeatherIndex:   weatherIndex,
		Revision:       revision,
	}
}

func (c *Clock) SetNowForTest(now func() time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

func (c *Clock) SetSeason(season string) error {
	normalized := strings.ToLower(strings.TrimSpace(season))
	if normalized != "summer" && normalized != "winter" {
		return fmt.Errorf("season must be summer or winter")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.season != normalized {
		c.season = normalized
		c.revision++
	}
	return nil
}

func (c *Clock) SetGameSecond(gameSecond int) (protocol.WorldState, error) {
	dayStartSecond := int(gameDayStartHour * 60 * 60)
	dayEndSecond := int(gameDayEndHour * 60 * 60)
	if gameSecond < dayStartSecond || gameSecond >= dayEndSecond {
		return protocol.WorldState{}, fmt.Errorf(
			"game time must be between 08:30 and 23:29",
		)
	}

	c.mu.Lock()
	now := c.now()
	elapsed := now.Sub(c.epoch)
	if elapsed < 0 {
		elapsed = 0
	}
	activeHours := time.Duration(
		(gameDayEndHour - gameDayStartHour) * float64(time.Hour),
	)
	activeDayLength := time.Duration(
		float64(c.dayLength) * float64(activeHours) / float64(24*time.Hour),
	)
	dayNumber := elapsed / activeDayLength
	secondsIntoDay := time.Duration(gameSecond-dayStartSecond) * time.Second
	withinDay := time.Duration(
		float64(activeDayLength) * float64(secondsIntoDay) / float64(activeHours),
	)
	c.epoch = now.Add(-time.Duration(dayNumber)*activeDayLength - withinDay)
	c.revision++
	c.mu.Unlock()

	return c.Snapshot(), nil
}

func (c *Clock) SetWeather(weather string) error {
	normalized := strings.ToLower(strings.TrimSpace(weather))
	valid := false
	for _, name := range weatherNames {
		if normalized == name {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("weather must be clear, overcast, rain, or snow")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.weather != normalized {
		c.weather = normalized
		c.revision++
	}
	return nil
}
