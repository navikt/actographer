package logger

import (
	"log/slog"
	"os"
)

var ProgramLevel = new(slog.LevelVar)

func SetupLogger() {
	ProgramLevel.Set(slog.LevelInfo)

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     ProgramLevel,
		AddSource: false,
	})
	slog.SetDefault(slog.New(handler))
}

func SetDebug(debug bool) {
	if debug {
		ProgramLevel.Set(slog.LevelDebug)
	}
}
