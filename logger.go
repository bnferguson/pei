package main

import (
	"log/slog"
	"os"
)

// initLogger initializes the global slog logger based on environment variables
func initLogger() {
	// Determine log level
	logLevel := os.Getenv("PEI_LOG_LEVEL")
	var level slog.Level
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info", "":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Determine log format
	logFormat := os.Getenv("PEI_LOG_FORMAT")

	var handler slog.Handler
	if logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
}

// Component-specific loggers
func getLogger(component string) *slog.Logger {
	return slog.With("component", component)
}

// Service-specific logging helpers
func logServiceInfo(service string, message string, args ...any) {
	slog.Info(message, append([]any{"service", service}, args...)...)
}

func logServiceError(service string, message string, args ...any) {
	slog.Error(message, append([]any{"service", service}, args...)...)
}
