package slogrus

import (
	"context"
	"io"
	"log/slog"

	clog "github.com/containerd/log"
	"github.com/sirupsen/logrus"
)

func OverrideGlobal(log *slog.Logger) {
	logrus.SetOutput(io.Discard)
	logrus.AddHook(&slogrusHook{log: log})
}

func WithLogger(ctx context.Context, slog *slog.Logger) context.Context {
	return clog.WithLogger(ctx, logrus.NewEntry(NewLogger(slog)))
}

func NewLogger(slog *slog.Logger) *logrus.Logger {
	log := &logrus.Logger{
		Out:       io.Discard,
		Hooks:     make(logrus.LevelHooks),
		Level:     logrus.DebugLevel,
		Formatter: &logrus.TextFormatter{},
	}

	log.AddHook(&slogrusHook{log: slog})

	return log
}

type slogrusHook struct {
	log *slog.Logger
}

func (h *slogrusHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

var logrusLevels = map[logrus.Level]slog.Level{
	logrus.PanicLevel: slog.LevelError,
	logrus.FatalLevel: slog.LevelError,
	logrus.ErrorLevel: slog.LevelError,
	logrus.WarnLevel:  slog.LevelWarn,
	logrus.InfoLevel:  slog.LevelInfo,
	logrus.DebugLevel: slog.LevelDebug,
	logrus.TraceLevel: slog.LevelDebug,
}

func (h *slogrusHook) Fire(e *logrus.Entry) error {
	level := logrusLevels[e.Level]

	var args []any

	for k, v := range e.Data {
		args = append(args, k, v)
	}

	h.log.Log(e.Context, level, e.Message, args...)

	return nil
}
