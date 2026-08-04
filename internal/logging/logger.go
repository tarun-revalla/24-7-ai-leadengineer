package logging

import (
	"fmt"
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap logger with structured logging capabilities.
type Logger struct {
	z *zap.Logger
	mu sync.RWMutex
}

// New creates a new logger with the specified configuration.
func New(level string, format string, output string) (*Logger, error) {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	var encoder zapcore.Encoder
	if format == "json" {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	logLevel := stringToLevel(level)

	// Create file sink if output path is specified
	var sink zapcore.WriteSyncer

	if output == "" || output == "stdout" {
		sink = zapcore.AddSync(NewConsoleWriter())
	} else {
		file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		sink = zapcore.AddSync(file)
	}

	core := zapcore.NewCore(encoder, sink, logLevel)
	zapLogger := zap.New(core, zap.AddCallerSkip(1))

	return &Logger{z: zapLogger}, nil
}

// Debug logs a debug-level message.
func (l *Logger) Debug(msg string, fields ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	l.z.Debug(msg, l.fieldsToZap(fields...)...)
}

// Info logs an info-level message.
func (l *Logger) Info(msg string, fields ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	l.z.Info(msg, l.fieldsToZap(fields...)...)
}

// Warn logs a warning-level message.
func (l *Logger) Warn(msg string, fields ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	l.z.Warn(msg, l.fieldsToZap(fields...)...)
}

// Error logs an error-level message.
func (l *Logger) Error(msg string, fields ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	l.z.Error(msg, l.fieldsToZap(fields...)...)
}

// Fatal logs a fatal-level message and exits.
func (l *Logger) Fatal(msg string, fields ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	l.z.Fatal(msg, l.fieldsToZap(fields...)...)
}

// WithFields returns a logger with additional fields.
func (l *Logger) WithFields(fields ...interface{}) *Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return &Logger{
		z: l.z.With(l.fieldsToZap(fields...)...),
	}
}

// Sync flushes any buffered log entries.
func (l *Logger) Sync() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.z.Sync()
}

// fieldsToZap converts field pairs to zap fields.
func (l *Logger) fieldsToZap(fields ...interface{}) []zap.Field {
	if len(fields)%2 != 0 {
		return []zap.Field{zap.String("_error", "odd number of fields")}
	}

	zapFields := make([]zap.Field, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		key := fmt.Sprintf("%v", fields[i])
		val := fields[i+1]
		zapFields[i/2] = zap.Any(key, val)
	}
	return zapFields
}

// stringToLevel converts string to zapcore level.
func stringToLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// NoOpLogger is a logger that discards all messages.
type NoOpLogger struct{}

// Debug does nothing.
func (l *NoOpLogger) Debug(msg string, fields ...interface{}) {}

// Info does nothing.
func (l *NoOpLogger) Info(msg string, fields ...interface{}) {}

// Warn does nothing.
func (l *NoOpLogger) Warn(msg string, fields ...interface{}) {}

// Error does nothing.
func (l *NoOpLogger) Error(msg string, fields ...interface{}) {}

// Fatal does nothing.
func (l *NoOpLogger) Fatal(msg string, fields ...interface{}) {}

// WithFields returns self.
func (l *NoOpLogger) WithFields(fields ...interface{}) *NoOpLogger {
	return l
}

// Sync does nothing.
func (l *NoOpLogger) Sync() error {
	return nil
}
