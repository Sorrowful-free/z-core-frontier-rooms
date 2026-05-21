package stdlib

import (
	"fmt"
	"log"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

// Logger оборачивает стандартный пакет log.
type Logger struct {
	tag string
}

// New возвращает реализацию logging.Logger поверх log.Println.
func New(tag string) logging.Logger {
	return &Logger{
		tag: tag,
	}
}

func (l *Logger) Error(msg string, args ...any) {
	log.Printf("%s: %s", l.tag, fmt.Sprintf(msg, args...))
}

func (l *Logger) Info(msg string, args ...any) {
	log.Printf("%s: %s", l.tag, fmt.Sprintf(msg, args...))
}

func (l *Logger) Debug(msg string, args ...any) {
	log.Printf("%s: %s", l.tag, fmt.Sprintf(msg, args...))
}

func (l *Logger) Warn(msg string, args ...any) {
	log.Printf("%s: %s", l.tag, fmt.Sprintf(msg, args...))
}

func (l *Logger) Fatal(msg string, args ...any) {
	log.Fatalf(msg, args...)
}
