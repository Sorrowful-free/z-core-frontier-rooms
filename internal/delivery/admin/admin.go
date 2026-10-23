// Package admin — admin listener процесса (P19): отдельный Fiber-инстанс
// вне публичного API. Сейчас единственный маршрут — `GET /metrics`
// (prometheus exposition текста через `prometheus.Gatherer`; без
// `net/http`/promhttp — ручной gather + expfmt).
package admin

import (
	"bytes"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// NewMetricsApp собирает Fiber-приложение admin listener'а:
// `GET /metrics` → exposition всех метрик gatherer'а. logger — для
// error-path gather/encode (P19b: ошибки не глотаются без следа).
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
