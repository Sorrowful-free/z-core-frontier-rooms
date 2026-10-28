package http

import (
	"errors"
	"time"

	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/gofiber/fiber/v3"
)

// NewMetricsMiddleware — emits duration/status/count signals per route for
// the public Fiber app (P19b). The `route` label is the route template
// (`c.Route().Path`), not the raw path: cardinality is bounded by the router;
// unmatched requests go into the "-" bucket. Upgrade requests (/ws)
// are skipped: a hijacked connection lives for the session duration, not the
// request duration, and would feed false duration outliers into the histogram.
func NewMetricsMiddleware(m portmetrics.Metrics) fiber.Handler {
	metrics := portmetrics.OrNoop(m)
	return func(c fiber.Ctx) error {
		if c.Get(fiber.HeaderUpgrade) != "" {
			return c.Next()
		}
		started := time.Now()
		err := c.Next()
		route := c.Route().Path
		if route == "" {
			route = "-"
		}
		// ErrorHandler has not run yet: on a handler error the status at
		// this stage is not final; we guess it like the default handler does
		// (fiber.Error -> its own code, anything else -> 500).
		status := c.Response().StatusCode()
		if err != nil {
			status = fiber.StatusInternalServerError
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) {
				status = fiberErr.Code
			}
		}
		metrics.HTTPRequestObserved(route, c.Method(), status, time.Since(started))
		return err
	}
}
