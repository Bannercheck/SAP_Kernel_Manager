package platform

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FixLocalTime makes time.Local honour a POSIX-style TZ value such as
// "TRT-3" or "EET-2EEST" (the default on many AIX hosts). Go only loads
// zoneinfo names and otherwise silently falls back to UTC, which would shift
// every "today" comparison by the zone offset around midnight. Daylight
// saving rules in the string are ignored: the standard offset is used.
func FixLocalTime() {
	tz := os.Getenv("TZ")
	if tz == "" || tz == "UTC" || time.Local.String() != "UTC" {
		return
	}
	if name, off, ok := parsePosixTZ(tz); ok {
		time.Local = time.FixedZone(name, off)
	}
}

var posixTZRe = regexp.MustCompile(`^:?([A-Za-z]{3,}|<[^>]+>)([+-]?)(\d{1,2})(?::(\d{2}))?`)

// parsePosixTZ reads "NAME[+|-]HH[:MM]..." and returns the UTC offset in
// seconds east of UTC (POSIX counts west as positive: TRT-3 is UTC+3).
func parsePosixTZ(s string) (name string, offset int, ok bool) {
	m := posixTZRe.FindStringSubmatch(s)
	if m == nil {
		return "", 0, false
	}
	h, _ := strconv.Atoi(m[3])
	mi := 0
	if m[4] != "" {
		mi, _ = strconv.Atoi(m[4])
	}
	offset = h*3600 + mi*60
	if m[2] != "-" {
		offset = -offset
	}
	return strings.Trim(m[1], "<>"), offset, true
}
