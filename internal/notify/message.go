package notify

import (
	"fmt"
	"strings"
	"time"

	"github.com/ropslabs/x3f/internal/status"
)

// MaxMessageLength is Telegram's limit: "1-4096 characters after entities
// parsing".
const MaxMessageLength = 4096

// maxFindingLength cuts long findings (an advisory finding names three
// titles), so that one finding cannot fill a message.
const maxFindingLength = 300

// timeFormat is used for "since" times. UTC, because the reader may be in
// another time zone than the machine running x3f.
const timeFormat = "2006-01-02 15:04 UTC"

// Format renders the changes as one Telegram message in HTML: per node a
// line with its status, followed by its WARN and CRIT findings. All text
// from x3f's assessments and the config is escaped. If the message would
// be longer than limit, whole lines are left out at the end and a last line
// says so.
func Format(changes []Change, now time.Time, limit int) string {
	var lines []string
	for i, c := range changes {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, header(c, now))
		for _, ch := range c.Node.Checks {
			for _, f := range ch.Findings {
				if f.Level == status.OK {
					continue // notes stay in the report
				}
				lines = append(lines, fmt.Sprintf("• %s %s: %s",
					f.Level, Escape(ch.Name), Escape(cut(f.Message, maxFindingLength))))
			}
		}
	}

	const tail = "… (shortened, run x3f for the full report)"
	var b strings.Builder
	used := 0
	for i, l := range lines {
		n := length(l)
		if i > 0 {
			n++ // the newline before l
		}
		// Unless l is the last line, a newline and the tail must still
		// fit after it.
		reserve := 0
		if i < len(lines)-1 {
			reserve = 1 + length(tail)
		}
		if i > 0 {
			b.WriteString("\n")
		}
		if used+n+reserve > limit {
			b.WriteString(tail)
			break
		}
		b.WriteString(l)
		used += n
	}
	return b.String()
}

// header is the first line of a node's block, e.g.
// "🔴 <b>mainnet-2</b>: CRIT (was OK)".
func header(c Change, now time.Time) string {
	n := c.Node
	s := fmt.Sprintf("%s <b>%s</b>: ", icon(n.Level), Escape(n.Name))
	since := c.Since.UTC().Format(timeFormat)
	switch c.Kind {
	case Worse:
		return s + fmt.Sprintf("still %s, worse: %s", n.Level, Escape(strings.Join(c.Worse, ", ")))
	case Reminder:
		return s + fmt.Sprintf("still %s (since %s)", n.Level, since)
	}
	s += fmt.Sprintf("%s (was %s", n.Level, c.Was)
	// A since before this run means the message could not be delivered
	// at the time.
	if c.Since.Before(now) {
		s += ", since " + since
	}
	return s + ")"
}

func icon(l status.Level) string {
	switch l {
	case status.OK:
		return "✅"
	case status.Warn:
		return "🟡"
	default:
		return "🔴"
	}
}

// cut shortens s to at most limit characters.
func cut(s string, limit int) string {
	if r := []rune(s); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return s
}

// length counts conservatively: in UTF-16 code units (an emoji outside the
// Basic Multilingual Plane counts twice), and tags and entities count with
// all their characters, although Telegram counts after parsing them.
func length(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}
