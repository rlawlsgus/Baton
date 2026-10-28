// Package classify turns a StopFailure hook payload into a decision.
package classify

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Windows has no zoneinfo; limit messages name IANA zones
)

type Kind int

const (
	Ignore     Kind = iota // overload, server error, bad request...: stay on this account
	Transient              // a short throttle, not this account's limit: stay
	UsageLimit             // this account hit its usage limit: switch
	NeedsLogin             // authentication failed: mark and switch
	Blocked                // on hold / billing / org not allowed: stop, never auto-switch
)

func (k Kind) String() string {
	return [...]string{"ignore", "transient", "usage_limit", "needs_login", "blocked"}[k]
}

// Transient throttles also end as error "rate_limit"; only the text differs.
var transientMarkers = []string{
	"not your usage limit",
	"server is temporarily limiting requests",
}

// Error maps StopFailure's error (and the message claude showed) to a Kind.
func Error(errType, message string) Kind {
	low := strings.ToLower(message)
	switch errType {
	case "rate_limit":
		for _, m := range transientMarkers {
			if strings.Contains(low, m) {
				return Transient
			}
		}
		return UsageLimit
	case "authentication_failed":
		if strings.Contains(low, "revoked") {
			// A revoked token can be an enforcement action, not an expiry.
			return Blocked
		}
		return NeedsLogin
	case "account_on_hold", "billing_error", "oauth_org_not_allowed", "verification_required":
		// Switching here would look like evading an enforcement action.
		return Blocked
	default: // overloaded, server_error, invalid_request, model_not_found, unknown, ...
		return Ignore
	}
}

// resetRx matches the tail of claude's limit message, e.g.
// "You've hit your session limit · resets 3pm (Asia/Seoul)",
// "... · resets Oct 27, 3:30pm (Asia/Seoul)" or, across a year boundary,
// "... · resets Jan 2, 2027, 9am (Asia/Seoul)".
var resetRx = regexp.MustCompile(`(?i)resets\s+(?:([A-Z][a-z]{2})\s+(\d{1,2}),?\s+(?:(\d{4}),?\s+)?)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)\s*\(([^)]+)\)`)

var months = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

// JustPassedRetry is how soon an account is tried again when its stated reset
// has just passed but it is still limited (a clock a little ahead, or the
// quota freed a little late).
const JustPassedRetry = 2 * time.Minute

// ResetTime extracts when the limit resets, if the message says. A reset
// within the last hour means "any moment now": retry shortly. An older one is
// unknown (the caller then uses its own default).
func ResetTime(message string, now time.Time) (time.Time, bool) {
	m := resetRx.FindStringSubmatch(message)
	if m == nil {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(strings.TrimSpace(m[7]))
	if err != nil {
		return time.Time{}, false
	}
	hour, _ := strconv.Atoi(m[4])
	minute := 0
	if m[5] != "" {
		minute, _ = strconv.Atoi(m[5])
	}
	if hour < 1 || hour > 12 || minute > 59 {
		return time.Time{}, false
	}
	hour %= 12
	if strings.EqualFold(m[6], "pm") {
		hour += 12
	}
	local := now.In(loc)
	var t time.Time
	if m[1] != "" {
		mon, ok := months[strings.ToLower(m[1])]
		day, _ := strconv.Atoi(m[2])
		if !ok || day < 1 || day > 31 {
			return time.Time{}, false
		}
		year := local.Year()
		if m[3] != "" {
			year, _ = strconv.Atoi(m[3])
		}
		t = time.Date(year, mon, day, hour, minute, 0, 0, loc)
		if m[3] == "" && t.Before(local.Add(-24*time.Hour)) { // "Jan 2" seen in late December
			t = t.AddDate(1, 0, 0)
		}
	} else {
		// claude drops the date when the reset is less than 24 h away, so a
		// time more than a few minutes past means tomorrow (e.g. a weekly limit).
		t = time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
		if later := t.Add(time.Hour); local.Sub(t) > 5*time.Minute && later.Hour() == hour && later.Minute() == minute {
			t = later // the repeated hour of a DST fall-back: time.Date may pick either one
		}
		if local.Sub(t) > 5*time.Minute {
			t = t.AddDate(0, 0, 1)
		}
	}
	if !t.After(now) {
		if now.Sub(t) < time.Hour {
			return now.Add(JustPassedRetry), true // "resets 3pm" read at 3:00:30pm
		}
		return time.Time{}, false
	}
	return t, true
}
