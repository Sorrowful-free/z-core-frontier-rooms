package http

import (
	"crypto/subtle"
	"strings"

	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	"github.com/gofiber/fiber/v3"
)

const (
	codeUnauthorized     = "unauthorized"
	headerAuthorization  = "Authorization"
	headerAPIKey         = "X-API-Key"
	bearerPrefix         = "Bearer "
)

// NewAPIKeyMiddleware защищает REST control plane статическим API key.
func NewAPIKeyMiddleware(cfg httpauthadapter.HTTPAuthConfig) fiber.Handler {
	expected := []byte(cfg.APIKey)
	return func(c fiber.Ctx) error {
		if cfg.Disabled {
			return c.Next()
		}

		provided := apiKeyFromRequest(c)
		if !apiKeyEqual(expected, []byte(provided)) {
			return writeAPIError(c, fiber.StatusUnauthorized, codeUnauthorized, "missing or invalid api key")
		}
		return c.Next()
	}
}

func apiKeyFromRequest(c fiber.Ctx) string {
	if auth := c.Get(headerAuthorization); strings.HasPrefix(auth, bearerPrefix) {
		return strings.TrimPrefix(auth, bearerPrefix)
	}
	return c.Get(headerAPIKey)
}

func apiKeyEqual(expected, provided []byte) bool {
	if len(expected) == 0 || len(provided) == 0 {
		return false
	}
	if len(expected) != len(provided) {
		return false
	}
	return subtle.ConstantTimeCompare(expected, provided) == 1
}
