package logging

// Logger — port for writing diagnostic messages (adapters: stdlib, zap, …).
type Logger interface {
	Error(msg string, args ...any)
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
	Fatal(msg string, args ...any)
}
