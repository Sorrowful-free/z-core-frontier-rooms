package server

import (
	"errors"
)

const DefaultHTTPAddr = ":3000"

// ServerConfig — HTTP/Fiber listen и прочие параметры процесса.
type ServerConfig struct {
	HTTPAddr string
}

func (c ServerConfig) Validate() error {
	if c.HTTPAddr == "" {
		return errors.New("server config: http addr is required")
	}
	return nil
}
