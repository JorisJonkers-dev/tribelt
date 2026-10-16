package stats

import "time"

// now is the moment every stats test reads its figures at.
func now() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, amsterdam()) }

// at is daysAgo days before now.
func at(daysAgo int) time.Time { return now().AddDate(0, 0, -daysAgo) }
