package room

import "time"

// graceExpired — единый предикат истечения grace admitted-слота (P24):
// true, когда now достигла lastSeen+ttl. ttl <= 0 → «grace нет»:
// lastSeen+0 всегда в прошлом → expired (тот же ноль-семантизм у
// orphan sweep и у resume-гейта — оба caller'а делят предикат).
func graceExpired(lastSeen time.Time, ttl time.Duration, now time.Time) bool {
	return !now.Before(lastSeen.Add(ttl))
}
