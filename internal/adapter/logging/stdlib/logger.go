package stdlib

import (
	"fmt"
	"log"
	"strings"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

// Logger оборачивает стандартный пакет log.
// args в методах — пары key, value, как в slog/zap: Info("msg", "room_id", id).
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
	log.Printf("%s: %s%s", l.tag, msg, formatKV(args))
}

func (l *Logger) Info(msg string, args ...any) {
	log.Printf("%s: %s%s", l.tag, msg, formatKV(args))
}

func (l *Logger) Debug(msg string, args ...any) {
	log.Printf("%s: %s%s", l.tag, msg, formatKV(args))
}

func (l *Logger) Warn(msg string, args ...any) {
	log.Printf("%s: %s%s", l.tag, msg, formatKV(args))
}

func (l *Logger) Fatal(msg string, args ...any) {
	log.Fatalf("%s: %s%s", l.tag, msg, formatKV(args))
}

// formatKV сериализует пары key, value в суффикс " k=v" (как zap SugaredLogger).
// Нечётный хвост без значения выводится как " k".
func formatKV(args []any) string {
	var b strings.Builder
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			fmt.Fprintf(&b, " %v=%v", args[i], args[i+1])
		} else {
			fmt.Fprintf(&b, " %v", args[i])
		}
	}
	return b.String()
}
