package http

import (
	"errors"
	"time"

	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/gofiber/fiber/v3"
)

// NewMetricsMiddleware — сигнал duration/status/count по route для
// публичного Fiber-приложения (P19b). Label `route` — шаблон маршрута
// (`c.Route().Path`), не raw path: кардинальность ограничена роутером;
// неразрешённые запросы идут в bucket "-". Upgrade-запросы (/ws)
// пропускаются: hijacked-соединение живёт длину сессии, а не запроса,
// и отдало бы ложные duration-выбросы в histogram.
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
		// ErrorHandler ещё не исполнялся: при ошибке handler'а статус на
		// этом этапе - не финальный; угадываем его как default handler
		// (fiber.Error -> свой код, прочее -> 500).
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
