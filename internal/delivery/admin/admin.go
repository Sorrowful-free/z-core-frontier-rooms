// Package admin — the process admin listener (P19): a separate Fiber instance
// outside the public API. Currently the only route is `GET /metrics`
// (prometheus text exposition via `prometheus.Gatherer`; no
// `net/http`/promhttp — manual gather + expfmt).
package admin

import (
	"bytes"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// NewMetricsApp builds the Fiber app for the admin listener:
// `GET /metrics` → exposition of all gatherer metrics. logger is for
// the gather/encode error path (P19b: errors are not swallowed silently).
func NewMetricsApp(gatherer prometheus.Gatherer, logger logging.Logger) *fiber.App {
	app := fiber.New()
	app.Get("/metrics", newMetricsHandler(gatherer, logger))
	return app
}

func newMetricsHandler(gatherer prometheus.Gatherer, logger logging.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		families, err := gatherer.Gather()
		if err != nil {
			logger.Error("metrics gather failed", "error", err)
			return fmt.Errorf("metrics gather: %w", err)
		}
		var buf bytes.Buffer
		format := expfmt.NewFormat(expfmt.TypeTextPlain)
		enc := expfmt.NewEncoder(&buf, format)
		for _, mf := range families {
			if err := enc.Encode(mf); err != nil {
				logger.Error("metrics encode failed", "error", err)
				return fmt.Errorf("metrics encode: %w", err)
			}
		}
		c.Set(fiber.HeaderContentType, string(format))
		return c.Send(buf.Bytes())
	}
}
