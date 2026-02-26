package util

import (
	"fmt"
	"time"
)

// RelativeTime formats t as a human-readable relative time string.
// e.g. "just now", "5m ago", "2h ago", "Mon", "Jan 2", "2023".
func RelativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < 0:
		return FormatDate(t)
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	case diff < 48*time.Hour:
		return "Yesterday"
	default:
		return FormatDate(t)
	}
}

// FormatDate formats t as a compact date string relative to now:
// "Mon" (this week), "Jan 2" (this year), "Jan '23" (previous years).
func FormatDate(t time.Time) string {
	now := time.Now()

	// Same week: show weekday abbreviation
	startOfWeek := now.AddDate(0, 0, -int(now.Weekday()))
	if t.After(startOfWeek) && t.Year() == now.Year() {
		return t.Format("Mon")
	}

	// This year: "Jan 2"
	if t.Year() == now.Year() {
		return t.Format("Jan 2")
	}

	// Older: "Jan '23"
	return t.Format("Jan '06")
}

// FormatDateLong formats t as a full date string.
func FormatDateLong(t time.Time) string {
	return t.Format("Monday, January 2, 2006")
}

// FormatDateTime formats t as a date+time string.
func FormatDateTime(t time.Time) string {
	return t.Format("Jan 2, 2006 15:04")
}
