// Package logging is a 1-file slog wrapper. We keep it deliberately tiny
// because a bootstrap CLI shouldn't need a fancy logging framework.
package logging

import (
	"log/slog"
	"os"
)

var Default = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

// SetLevel switches the global Default logger to the given level.
func SetLevel(level slog.Level) {
	Default = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// Info forwards to Default.Info.
func Info(msg string, args ...any) { Default.Info(msg, args...) }

// Warn forwards to Default.Warn.
func Warn(msg string, args ...any) { Default.Warn(msg, args...) }

// Error forwards to Default.Error.
func Error(msg string, args ...any) { Default.Error(msg, args...) }

// Debug forwards to Default.Debug.
func Debug(msg string, args ...any) { Default.Debug(msg, args...) }
