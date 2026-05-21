package stdlib

import (
	"log"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

// Logger оборачивает стандартный пакет log.
type Logger struct{}

// New возвращает реализацию logging.Logger поверх log.Println.
func New() logging.Logger {
	return &Logger{}
}

func (l *Logger) Error(msg string, args ...any) {
	log.Printf(msg, args...)
}

func (l *Logger) Info(msg string, args ...any) {
	log.Printf(msg, args...)
}

func (l *Logger) Debug(msg string, args ...any) {
	log.Printf(msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	log.Printf(msg, args...)
}

func (l *Logger) Fatal(msg string, args ...any) {
	log.Fatalf(msg, args...)
}
