package httplimits

import (
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	porthttplimits "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/httplimits"
)

const rateLimitWindow = time.Minute

type limits struct {
	cfg  HTTPLimitsConfig
	rate *rateLimiter
}

// New собирает Limits из конфигурации (rate + max rooms).
func New(cfg HTTPLimitsConfig) (porthttplimits.Limits, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &limits{
		cfg:  cfg,
		rate: newRateLimiter(),
	}, nil
}

func (l *limits) AllowCreateRoom(currentRoomCount int) error {
	if l.cfg.MaxRooms <= 0 || currentRoomCount < l.cfg.MaxRooms {
		return nil
	}
	return domain.ErrRoomsLimitReached
}

func (l *limits) AllowHTTPCreate(clientIP string) bool {
	return l.rate.allow("create:"+clientIP, l.cfg.CreateRoomsPerMinute, rateLimitWindow)
}

func (l *limits) AllowHTTPIssueTicket(clientIP string) bool {
	return l.rate.allow("issue:"+clientIP, l.cfg.IssueTicketsPerMinute, rateLimitWindow)
}

var _ porthttplimits.Limits = (*limits)(nil)
