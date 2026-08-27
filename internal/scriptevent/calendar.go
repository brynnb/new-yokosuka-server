package scriptevent

import (
	"fmt"
	"time"
)

// CalendarDate is the authoritative in-game calendar date, kept separate from
// wall-clock zones and time-of-day so script comparisons remain deterministic.
type CalendarDate struct {
	Year  int
	Month int
	Day   int
}

func CalendarDateFromTime(value time.Time) CalendarDate {
	value = value.UTC()
	return CalendarDate{Year: value.Year(), Month: int(value.Month()), Day: value.Day()}
}

func ParseCalendarDate(value string) (CalendarDate, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return CalendarDate{}, fmt.Errorf("game date must use YYYY-MM-DD: %w", err)
	}
	return CalendarDateFromTime(parsed), nil
}

func (date CalendarDate) Validate() error {
	if date.Year < 1 || date.Year > 9999 {
		return invalidCalendarDate(date)
	}
	resolved := time.Date(date.Year, time.Month(date.Month), date.Day, 0, 0, 0, 0, time.UTC)
	if resolved.Year() != date.Year || int(resolved.Month()) != date.Month || resolved.Day() != date.Day {
		return invalidCalendarDate(date)
	}
	return nil
}

func validateMonthDay(month, day int64) error {
	// Year 2000 deliberately permits February 29 while still rejecting every
	// impossible month/day pair.
	resolved := time.Date(2000, time.Month(month), int(day), 0, 0, 0, 0, time.UTC)
	if month < 1 || month > 12 || day < 1 ||
		int(resolved.Month()) != int(month) || resolved.Day() != int(day) {
		return fmt.Errorf("game_date_on_or_after requires a valid month and day")
	}
	return nil
}

func invalidCalendarDate(date CalendarDate) error {
	return fmt.Errorf("invalid authoritative game date %04d-%02d-%02d", date.Year, date.Month, date.Day)
}
