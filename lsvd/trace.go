package lsvd

import (
	"context"
	"log/slog"
)

const LevelTrace = slog.LevelDebug - 1

func trace(log *slog.Logger, msg string, v ...interface{}) {
	log.Log(context.TODO(), LevelTrace, msg, v...)
}
