package tui

import (
	"fmt"
	"strings"
	"time"
)

func ago(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	days := int(d.Hours() / 24)
	switch {
	case d < 0:
		return "just now"
	case days == 0 && now.YearDay() == t.YearDay():
		return "today"
	case days <= 1:
		return "yesterday"
	case days < 14:
		return fmt.Sprintf("%d days ago", days)
	case days < 60:
		return fmt.Sprintf("%d weeks ago", days/7)
	case days < 365:
		return fmt.Sprintf("%d months ago", days/30)
	case days < 730:
		return "a year ago"
	}
	return fmt.Sprintf("%d years ago", days/365)
}

func eta(done, total int64, elapsed time.Duration) string {
	if done <= 0 || total <= 0 || done >= total || elapsed < 2*time.Second {
		return ""
	}
	left := time.Duration(float64(elapsed) * float64(total-done) / float64(done))
	switch {
	case left < 10*time.Second:
		return "almost done"
	case left < 55*time.Second:
		return fmt.Sprintf("about %d seconds left", int(left.Seconds()+5)/10*10)
	case left < 2*time.Minute:
		return "about a minute left"
	}
	return fmt.Sprintf("about %d minutes left", int(left.Minutes()+0.5))
}

func mb(n int64) string {
	if n < 1<<30 {
		return fmt.Sprintf("%d MB", n/1_000_000)
	}
	return fmt.Sprintf("%.1f GB", float64(n)/1e9)
}

func num(n int64) string {
	s := fmt.Sprint(n)
	var out []byte
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func hostOf(u string) string {
	u = strings.TrimPrefix(u, "https://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		return u[:i]
	}
	return u
}
