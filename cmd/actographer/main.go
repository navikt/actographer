package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"

	"github.com/navikt/actographer/internal/config"
	"github.com/navikt/actographer/internal/fetcher"
	"github.com/navikt/actographer/internal/logger"
	"github.com/navikt/actographer/internal/resolver"
	"github.com/navikt/actographer/internal/runner"
)

func main() {
	ctx := context.Background()

	logger.SetupLogger()

	cfg, err := config.NewConfig(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		slog.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}

	logger.SetDebug(cfg.Debug)

	slog.Info("Starting actographer", "org", cfg.Org)

	f := fetcher.NewFetcher(cfg)
	r := resolver.NewResolver(f, cfg.FetchSBOM)
	app := runner.NewApp(cfg, f, r)

	if err := app.Run(ctx); err != nil {
		slog.Error("Run failed", "error", err)
		os.Exit(1)
	}
}
