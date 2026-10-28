package zap

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"go.uber.org/zap"
)

// Logger implements logging.Logger on top of go.uber.org/zap (SugaredLogger).
// args in methods are key, value pairs, as in slog: Info("msg", "room_id", id).
type Logger struct {
	sugar *zap.SugaredLogger
}

// New creates a production logger with the component name (logger field).
func New(tag string) (*Logger, error) {
	z, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("zap production config: %w", err)
	}
	return &Logger{sugar: z.Named(tag).Sugar()}, nil
}

// NewDevelopment — development config (human-readable output, DebugLevel).
func NewDevelopment(tag string) (*Logger, error) {
	z, err := zap.NewDevelopment()
	if err != nil {
		return nil, fmt.Errorf("zap development config: %w", err)
	}
	return &Logger{sugar: z.Named(tag).Sugar()}, nil
}

// NewFrom creates an adapter from a ready *zap.Logger (e.g., the shared root in cmd).
func NewFrom(z *zap.Logger, tag string) logging.Logger {
	return &Logger{sugar: z.Named(tag).Sugar()}
}

func (l *Logger) Error(msg string, args ...any) {
	l.sugar.Errorw(msg, args...)
}

func (l *Logger) Info(msg string, args ...any) {
	l.sugar.Infow(msg, args...)
}

func (l *Logger) Debug(msg string, args ...any) {
	l.sugar.Debugw(msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.sugar.Warnw(msg, args...)
}

func (l *Logger) Fatal(msg string, args ...any) {
	l.sugar.Fatalw(msg, args...)
}

// Zap returns the named *zap.Logger (for Fiber middleware and the like).
func (l *Logger) Zap() *zap.Logger {
	return l.sugar.Desugar()
}

// Sync flushes buffers; with a shared root in cmd it is better to call Sync on that root.
func (l *Logger) Sync() error {
	return l.sugar.Sync()
}
