package scheduler

import "time"

// NextRunAt computes the next scheduled run time after from for the given frequency.
func NextRunAt(from time.Time, frequency string) time.Time {
	from = from.UTC()
	switch frequency {
	case "weekly":
		return from.AddDate(0, 0, 7)
	case "monthly":
		return addOneMonth(from)
	default:
		return from
	}
}

func addOneMonth(t time.Time) time.Time {
	year, month, day := t.Date()
	hour, min, sec := t.Clock()
	loc := t.Location()

	month++
	if month > 12 {
		month = 1
		year++
	}

	lastDay := daysInMonth(year, time.Month(month))
	if day > lastDay {
		day = lastDay
	}

	return time.Date(year, time.Month(month), day, hour, min, sec, t.Nanosecond(), loc)
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
