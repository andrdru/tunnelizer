package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/andrdru/tunnelizer/internal/config"
	"github.com/andrdru/tunnelizer/internal/state"
	"github.com/andrdru/tunnelizer/internal/tunnel"
)

func runRunner(paths Paths, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli.run: %w", err)
	}

	if fs.NArg() != 1 {
		return fmt.Errorf("cli.run: %w", ErrUsage)
	}

	alias := fs.Arg(0)

	cfg, err := config.Load(paths.Config)
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
