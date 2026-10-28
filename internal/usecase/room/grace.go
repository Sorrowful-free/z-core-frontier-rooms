package room

import "time"

// graceExpired — single predicate for grace expiry of an admitted slot
// (P24): true once now reaches lastSeen+ttl. ttl <= 0 → "no grace":
// lastSeen+0 is always in the past → expired (same zero semantics for the
// orphan sweep and the resume gate — both callers share the predicate).
func graceExpired(lastSeen time.Time, ttl time.Duration, now time.Time) bool {
	return !now.Before(lastSeen.Add(ttl))
}
