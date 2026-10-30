package logger

import (
	"log/slog"
	"os"
)

func New(level string, format string) *slog.Logger {
	h := handler(format, stringToLevel(level))
	return slog.New(h)
}

func stringToLevel(level string) slog.Level {
	switch level {
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "debug":
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

func handler(format string, level slog.Level) slog.Handler {
	switch format {
	case "text":
		return slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: level == slog.LevelDebug,
			Level:     level,
		})
	default:
		return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: level == slog.LevelDebug,
			Level:     level,
		})
	}
}
