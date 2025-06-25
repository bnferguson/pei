package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
)

// ServiceOutputCapture manages capturing and logging service output
type ServiceOutputCapture struct {
	service    Service
	stdoutPipe io.ReadCloser
	stderrPipe io.ReadCloser
	stopChan   chan struct{}
	logger     *slog.Logger
	pid        int
}

// NewServiceOutputCapture creates a new output capture for a service
func NewServiceOutputCapture(service Service, stdoutPipe, stderrPipe io.ReadCloser, pid int) *ServiceOutputCapture {
	return &ServiceOutputCapture{
		service:    service,
		stdoutPipe: stdoutPipe,
		stderrPipe: stderrPipe,
		stopChan:   make(chan struct{}),
		logger:     slog.With("component", "service-output", "service", service.Name),
		pid:        pid,
	}
}

// Start begins capturing service output in separate goroutines
func (s *ServiceOutputCapture) Start() {
	if s.stdoutPipe != nil {
		go s.captureOutput(s.stdoutPipe, "stdout")
	}
	if s.stderrPipe != nil {
		go s.captureOutput(s.stderrPipe, "stderr")
	}
}

// Stop signals the capture goroutines to stop
func (s *ServiceOutputCapture) Stop() {
	close(s.stopChan)
}

// captureOutput reads from a pipe and logs each line with service context
func (s *ServiceOutputCapture) captureOutput(pipe io.ReadCloser, stream string) {
	defer pipe.Close()

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		select {
		case <-s.stopChan:
			return
		default:
			line := scanner.Text()
			if line != "" {
				s.logServiceOutput(line, stream)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		s.logger.Error("Error reading service output",
			"stream", stream,
			"error", err)
	}
}

// logServiceOutput intelligently handles service output, detecting and preserving structured logs
func (s *ServiceOutputCapture) logServiceOutput(line, stream string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	// Check if service is configured for JSON logs
	if s.service.JSONLogs {
		s.logStructuredServiceOutput(line, stream)
	} else {
		// Log as plain text with service context
		s.logger.Info("Service output",
			"stream", stream,
			"pid", s.pid,
			"user", s.service.User,
			"output", line)
	}
}

// logStructuredServiceOutput handles service output that is already structured JSON
func (s *ServiceOutputCapture) logStructuredServiceOutput(jsonLine, stream string) {
	// Parse the service's JSON log
	var serviceLog map[string]interface{}
	if err := json.Unmarshal([]byte(jsonLine), &serviceLog); err != nil {
		// If parsing fails, fall back to plain text logging
		s.logger.Info("Service output",
			"stream", stream,
			"pid", s.pid,
			"user", s.service.User,
			"output", jsonLine)
		return
	}

	// Extract level from service log, defaulting to INFO if not found or invalid
	level := extractLogLevel(serviceLog)

	// Extract message from service log
	message := extractLogMessage(serviceLog)

	// Create enhanced log entry that preserves service structure but adds pei context
	attrs := []slog.Attr{
		slog.String("stream", stream),
		slog.Int("pid", s.pid),
		slog.String("user", s.service.User),
		slog.String("service_log_format", "json"),
	}

	// Add all fields from the service log as nested attributes
	for key, value := range serviceLog {
		// Skip fields we've already handled
		if key == "level" || key == "severity" || key == "msg" || key == "message" {
			continue
		}

		// Convert value to string for logging
		attrs = append(attrs, slog.Any("service_"+key, value))
	}

	// Log at the same level as the service used
	s.logger.LogAttrs(context.Background(), level, message, attrs...)
}

// extractLogLevel extracts and converts log level from service JSON
func extractLogLevel(serviceLog map[string]interface{}) slog.Level {
	// Check common level field names
	levelFields := []string{"level", "severity", "lvl"}

	for _, field := range levelFields {
		if levelValue, exists := serviceLog[field]; exists {
			if levelStr, ok := levelValue.(string); ok {
				return parseLogLevel(strings.ToUpper(levelStr))
			}
		}
	}

	// Default to INFO if no level found
	return slog.LevelInfo
}

// extractLogMessage extracts message from service JSON
func extractLogMessage(serviceLog map[string]interface{}) string {
	// Check common message field names
	messageFields := []string{"msg", "message", "text", "content"}

	for _, field := range messageFields {
		if msgValue, exists := serviceLog[field]; exists {
			if msgStr, ok := msgValue.(string); ok {
				return msgStr
			}
		}
	}

	// If no message field found, use a generic message
	return "Service structured log"
}

// parseLogLevel converts string level to slog.Level
func parseLogLevel(levelStr string) slog.Level {
	switch levelStr {
	case "DEBUG", "DBG", "TRACE":
		return slog.LevelDebug
	case "INFO", "INFORMATION":
		return slog.LevelInfo
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR", "ERR", "FATAL", "CRITICAL":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
