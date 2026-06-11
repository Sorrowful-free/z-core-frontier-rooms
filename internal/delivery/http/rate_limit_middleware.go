package http

import (
	porthttplimits "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/httplimits"
	"github.com/gofiber/fiber/v3"
)

func newCreateRateLimitMiddleware(limits porthttplimits.Limits) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !limits.AllowHTTPCreate(c.IP()) {
			return writeAPIError(c, fiber.StatusTooManyRequests, codeRateLimited, "rate limit exceeded")
		}
		return c.Next()
	}
}

func newIssueRateLimitMiddleware(limits porthttplimits.Limits) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !limits.AllowHTTPIssueTicket(c.IP()) {
			return writeAPIError(c, fiber.StatusTooManyRequests, codeRateLimited, "rate limit exceeded")
		}
		return c.Next()
	}
}
