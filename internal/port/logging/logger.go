package logging

// Logger — порт для записи диагностических сообщений (адаптеры: stdlib, slog, …).
type Logger interface {
	Error(msg string, args ...any)
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
	Fatal(msg string, args ...any)
}
