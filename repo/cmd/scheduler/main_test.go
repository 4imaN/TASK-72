package main

import (
	"testing"
	"time"
)

// TestDurationUntilNextUTCHour locks in the cron-like behaviour the README
// promises ("scheduler job at 02:00 UTC rebuilds all search_documents").
func TestDurationUntilNextUTCHour(t *testing.T) {
	cases := []struct {
		name     string
		now      time.Time
		hour     int
		expected time.Duration
	}{
		{
			name:     "before target → wait until today 02:00",
			now:      time.Date(2026, 4, 13, 1, 30, 0, 0, time.UTC),
			hour:     2,
			expected: 30 * time.Minute,
		},
		{
			name:     "exactly at target → roll to tomorrow",
			now:      time.Date(2026, 4, 13, 2, 0, 0, 0, time.UTC),
			hour:     2,
			expected: 24 * time.Hour,
		},
		{
			name:     "after target → roll to tomorrow",
			now:      time.Date(2026, 4, 13, 14, 30, 0, 0, time.UTC),
			hour:     2,
			expected: 11*time.Hour + 30*time.Minute,
		},
		{
			name:     "midnight UTC, hour=0 → roll to tomorrow",
			now:      time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
			hour:     0,
			expected: 24 * time.Hour,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := durationUntilNextUTCHour(tc.now, tc.hour)
			if got != tc.expected {
				t.Errorf("durationUntilNextUTCHour(%v, %d) = %v; want %v",
					tc.now, tc.hour, got, tc.expected)
			}
		})
	}
}
