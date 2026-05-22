package zap

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"go.uber.org/zap"
)

// Logger реализует logging.Logger поверх go.uber.org/zap (SugaredLogger).
// args в методах — пары key, value, как в slog: Info("msg", "room_id", id).
type Logger struct {
	sugar *zap.SugaredLogger
}

// New создаёт production-логгер с именем компонента (поле logger).
func New(tag string) (*Logger, error) {
	z, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("zap production config: %w", err)
	}
	return &Logger{sugar: z.Named(tag).Sugar()}, nil
}

// NewDevelopment — development-конфиг (человекочитаемый вывод, DebugLevel).
func NewDevelopment(tag string) (*Logger, error) {
	z, err := zap.NewDevelopment()
	if err != nil {
		return nil, fmt.Errorf("zap development config: %w", err)
	}
	return &Logger{sugar: z.Named(tag).Sugar()}, nil
}

// NewFrom создаёт адаптер из готового *zap.Logger (например, общий root в cmd).
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

// Zap возвращает именованный *zap.Logger (для Fiber middleware и т.п.).
func (l *Logger) Zap() *zap.Logger {
	return l.sugar.Desugar()
}

// Sync сбрасывает буферы; при общем root в cmd лучше вызывать Sync на нём.
func (l *Logger) Sync() error {
	return l.sugar.Sync()
}
