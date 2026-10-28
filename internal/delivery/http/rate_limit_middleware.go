package http

import (
	porthttplimits "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/httplimits"
	"github.com/gofiber/fiber/v3"
)

// newRateLimitMiddleware — shared constructor for per-IP rate limit middleware;
// check is one of the limits.AllowHTTP* methods.
func newRateLimitMiddleware(check func(clientIP string) bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !check(c.IP()) {
			return writeAPIError(c, fiber.StatusTooManyRequests, codeRateLimited, "rate limit exceeded")
		}
		return c.Next()
	}
}

func newCreateRateLimitMiddleware(limits porthttplimits.Limits) fiber.Handler {
	return newRateLimitMiddleware(limits.AllowHTTPCreate)
}

func newIssueRateLimitMiddleware(limits porthttplimits.Limits) fiber.Handler {
	return newRateLimitMiddleware(limits.AllowHTTPIssueTicket)
}

func newDeleteRateLimitMiddleware(limits porthttplimits.Limits) fiber.Handler {
	return newRateLimitMiddleware(limits.AllowHTTPDelete)
}

func newListRateLimitMiddleware(limits porthttplimits.Limits) fiber.Handler {
	return newRateLimitMiddleware(limits.AllowHTTPList)
}
