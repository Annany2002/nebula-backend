package logger

import (
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// Logger is a wrapper around logrus.Logger
type Logger struct {
	*logrus.Logger
}

// NewLogger creates a new logger instance
func NewLogger() *Logger {
	logger := logrus.New()

	// Set the log level
	logger.SetLevel(logrus.DebugLevel)

	// Set the log format
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		ForceColors:     true,
		DisableColors:   false,
		PadLevelText:    true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	// Set the output file
	logFilePath := filepath.Join("logs", "api-nebula.log")
	if err := os.MkdirAll(filepath.Dir(logFilePath), os.ModePerm); err != nil {
		logrus.Fatalf("Failed to create log directory: %v", err)
	}

	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		logrus.Fatalf("Failed to open log file: %v", err)
	}

	mw := io.MultiWriter(file, os.Stdout)

	// Set the output
	logger.SetOutput(mw)

	return &Logger{Logger: logger}
}

// Info logs an informational message
func (l *Logger) Info(args ...interface{}) {
	l.Logger.Info(args...)
}

// Infoln logs an informational message with a newline
func (l *Logger) Infoln(args ...interface{}) {
	l.Logger.Infoln(args...)
}

// Infof logs an informational message with formatting
func (l *Logger) Infof(format string, args ...interface{}) {
	l.Logger.Infof(format, args...)
}

// Error logs an error message
func (l *Logger) Error(args ...interface{}) {
	l.Logger.Error(args...)
}

// Errorln logs an error message with a newline
func (l *Logger) Errorln(args ...interface{}) {
	l.Logger.Errorln(args...)
}

// Errorf logs an error message with formatting
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.Logger.Errorf(format, args...)
}

// Debug logs a debug message
func (l *Logger) Debug(args ...interface{}) {
	l.Logger.Debug(args...)
}

// Debugln logs a debug message with a newline
func (l *Logger) Debugln(args ...interface{}) {
	l.Logger.Debugln(args...)
}

// Debugf logs a debug message with formatting
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.Logger.Debugf(format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(args ...interface{}) {
	l.Logger.Warn(args...)
}

// Warnln logs a warning message with a newline
func (l *Logger) Warnln(args ...interface{}) {
	l.Logger.Warnln(args...)
}

// Warnf logs a warning message with formatting
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.Logger.Warnf(format, args...)
}
