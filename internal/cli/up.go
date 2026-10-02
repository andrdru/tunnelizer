package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/andrdru/tunnelizer/internal/config"
	"github.com/andrdru/tunnelizer/internal/state"
	"github.com/andrdru/tunnelizer/internal/tunnel"
)

var ErrAlreadyRunning = errors.New("tunnel is already running")

const logFilePerm = 0o600

func runUp(paths Paths, args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	all := fs.Bool("all", false, "bring up all tunnels from config")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli.up: %w", err)
	}

	cfg, err := config.Load(paths.Config)
	if err != nil {
		return fmt.Errorf("cli.up: %w", err)
	}

	aliases, err := pickAliases(*all, fs.Args(), cfg.Aliases())
	if err != nil {
		return fmt.Errorf("cli.up: %w", err)
	}

	ctx := context.Background()
	store := state.NewStore(paths.StateDir)
	failed := false

	for _, alias := range aliases {
		if uerr := upOne(ctx, cfg, store, alias); uerr != nil {
			fmt.Fprintf(os.Stderr, "tunz up %s: %v\n", alias, uerr)

			failed = true

			continue
		}

		fmt.Printf("%s: up\n", alias)
	}

	if failed {
		return fmt.Errorf("cli.up: %w", ErrPartialFailure)
	}

	return nil
}

func upOne(ctx context.Context, cfg *config.Config, store *state.Store, alias string) error {
	rt, err := cfg.Resolve(alias)
	if err != nil {
		return fmt.Errorf("cli.upOne: %w", err)
	}

	if pid, perr := store.ReadPID(alias); perr == nil && state.PIDAlive(pid) {
		return fmt.Errorf("cli.upOne %q: %w", alias, ErrAlreadyRunning)
	}

	if rt.Interactive {
		if merr := startMaster(ctx, rt, store.SockPath(alias)); merr != nil {
			return fmt.Errorf("cli.upOne: %w", merr)
		}
	}

	pid, err := spawnRunner(ctx, store, alias)
	if err != nil {
		return fmt.Errorf("cli.upOne: %w", err)
	}

	if err := store.WritePID(alias, pid); err != nil {
		return fmt.Errorf("cli.upOne: %w", err)
	}

	return nil
}

func startMaster(ctx context.Context, rt config.ResolvedTunnel, sockPath string) error {
	cmd := exec.CommandContext(ctx, tunnel.SSHBinary, tunnel.MasterArgs(rt, sockPath)...) // #nosec G204 -- ssh args come from user config
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cli.startMaster: %w", err)
	}

	return nil
}

func spawnRunner(ctx context.Context, store *state.Store, alias string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("cli.spawnRunner: %w", err)
	}

	if merr := os.MkdirAll(store.Dir(), dirPerm); merr != nil {
		return 0, fmt.Errorf("cli.spawnRunner: %w", merr)
	}

	logPath := filepath.Join(store.Dir(), alias+".log")

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFilePerm) // #nosec G304 -- path is built from own state dir
	if err != nil {
		return 0, fmt.Errorf("cli.spawnRunner: %w", err)
	}

	defer func() {
		if cerr := logFile.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "tunz: failed to close log file: %v\n", cerr)
		}
	}()

	cmd := exec.CommandContext(ctx, exe, "run", alias) // #nosec G204 -- re-executes own binary with fixed args
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("cli.spawnRunner: %w", err)
	}

	return cmd.Process.Pid, nil
}
