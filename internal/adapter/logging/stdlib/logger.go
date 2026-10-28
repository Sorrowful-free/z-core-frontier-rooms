package stdlib

import (
	"fmt"
	"log"
	"strings"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

// Logger wraps the standard log package.
// args in methods are key, value pairs, as in slog/zap: Info("msg", "room_id", id).
type Logger struct {
	tag string
}

// New returns a logging.Logger implementation on top of log.Println.
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

// formatKV serializes key, value pairs into a " k=v" suffix (like zap SugaredLogger).
// An odd trailing key without a value is printed as " k".
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
