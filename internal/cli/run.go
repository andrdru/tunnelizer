package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/andrdru/tunnelizer/internal/state"
	"github.com/andrdru/tunnelizer/internal/tunnel"
)

func runRunner(paths Paths, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)

	positional, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("cli.run: %w", err)
	}

	if len(positional) != 1 {
		return fmt.Errorf("cli.run: %w", ErrUsage)
	}

	alias := positional[0]

	cfg, err := loadConfig(paths)
	if err != nil {
		return fmt.Errorf("cli.run: %w", err)
	}

	rt, err := cfg.Resolve(alias)
	if err != nil {
		return fmt.Errorf("cli.run: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	store := state.NewStore(paths.StateDir)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	runner := tunnel.NewRunner(rt, store, store.SockPath(alias), tunnel.WithLogger(logger))

	if err := runner.Run(ctx); err != nil {
		return fmt.Errorf("cli.run: %w", err)
	}

	return nil
}
