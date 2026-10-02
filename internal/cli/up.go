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
	"time"

	"github.com/andrdru/tunnelizer/internal/config"
	"github.com/andrdru/tunnelizer/internal/state"
	"github.com/andrdru/tunnelizer/internal/tunnel"
)

var (
	ErrAlreadyRunning = errors.New("tunnel is already running")
	ErrNotConnected   = errors.New("tunnel is not connected")
)

const (
	logFilePerm = 0o600
	logExt      = ".log"

	upTimeout  = 10 * time.Second
	upPollStep = 200 * time.Millisecond
)

func runUp(paths Paths, args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	all := fs.Bool("all", false, "bring up all tunnels from config")

	positional, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("cli.up: %w", err)
	}

	cfg, err := loadConfig(paths)
	if err != nil {
		return fmt.Errorf("cli.up: %w", err)
	}

	aliases, err := pickAliases(*all, positional, cfg.Aliases())
	if err != nil {
		return fmt.Errorf("cli.up: %w", err)
	}

	ctx := context.Background()
	store := state.NewStore(paths.StateDir)
	failed := false

	for _, alias := range aliases {
		if uerr := upAndWait(ctx, cfg, store, alias); uerr != nil {
			fmt.Fprintf(os.Stderr, "tunz up %s: %v\n", alias, uerr)

			failed = true
		}
	}

	if failed {
		return fmt.Errorf("cli.up: %w", ErrPartialFailure)
	}

	return nil
}

func upAndWait(ctx context.Context, cfg *config.Config, store *state.Store, alias string) error {
	rt, err := cfg.Resolve(alias)
	if err != nil {
		return fmt.Errorf("cli.upAndWait: %w", err)
	}

	if err := upOne(ctx, store, rt); err != nil {
		return fmt.Errorf("cli.upAndWait: %w", err)
	}

	if err := waitUp(ctx, store, rt); err != nil {
		return fmt.Errorf("cli.upAndWait: %w", err)
	}

	return nil
}

func upOne(ctx context.Context, store *state.Store, rt config.ResolvedTunnel) error {
	if pid, perr := store.ReadPID(rt.Alias); perr == nil && state.PIDAlive(pid) {
		return fmt.Errorf("cli.upOne %q: %w", rt.Alias, ErrAlreadyRunning)
	}

	if rt.Interactive {
		if merr := startMaster(ctx, rt, store.SockPath(rt.Alias)); merr != nil {
			return fmt.Errorf("cli.upOne: %w", merr)
		}
	}

	pid, err := spawnRunner(ctx, store, rt.Alias)
	if err != nil {
		return fmt.Errorf("cli.upOne: %w", err)
	}

	if err := store.WritePID(rt.Alias, pid); err != nil {
		return fmt.Errorf("cli.upOne: %w", err)
	}

	return nil
}

// waitUp ждёт, пока локальный порт туннеля начнёт принимать соединения, либо раннер сдастся.
func waitUp(ctx context.Context, store *state.Store, rt config.ResolvedTunnel) error {
	pid, ok := runnerPID(store, rt.Alias)
	if !ok {
		return notConnectedError(store, rt.Alias, 0, "runner is not running")
	}

	deadline := time.Now().Add(upTimeout)

	for time.Now().Before(deadline) {
		if state.Probe(ctx, rt.LocalPort) {
			fmt.Printf("%s: connected, local port %d is open\n", rt.Alias, rt.LocalPort)

			return nil
		}

		if !state.PIDAlive(pid) {
			return notConnectedError(store, rt.Alias, pid, "runner is not running")
		}

		if reason := authRequiredReason(store, rt.Alias, pid); reason != "" {
			return notConnectedError(store, rt.Alias, pid, reason)
		}

		time.Sleep(upPollStep)
	}

	return notConnectedError(store, rt.Alias, pid, fmt.Sprintf("local port %d did not open in %s", rt.LocalPort, upTimeout))
}

// runnerPID возвращает pid живого раннера.
func runnerPID(store *state.Store, alias string) (int, bool) {
	pid, err := store.ReadPID(alias)
	if err != nil || !state.PIDAlive(pid) {
		return 0, false
	}

	return pid, true
}

// authRequiredReason смотрит только состояние, принадлежащее текущему раннеру:
// старый state-файл от предыдущего запуска не должен ронять свежий up.
func authRequiredReason(store *state.Store, alias string, pid int) string {
	st, err := store.Read(alias)
	if err != nil || st.PID != pid {
		return ""
	}

	if state.EffectiveStatus(st, state.PIDAlive) == state.StatusAuthRequired {
		return "ssh asked for interactive auth again"
	}

	return ""
}

func notConnectedError(store *state.Store, alias string, pid int, reason string) error {
	st, err := store.Read(alias)
	if err == nil && st.PID == pid && st.LastError != "" {
		reason = fmt.Sprintf("%s: %s", reason, st.LastError)
	}

	return fmt.Errorf("cli.waitUp: %w: %s (log: %s)", ErrNotConnected, reason, logPath(store, alias))
}

func logPath(store *state.Store, alias string) string {
	return filepath.Join(store.Dir(), alias+logExt)
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

	path := logPath(store, alias)

	logFile, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFilePerm) // #nosec G304 -- own state dir
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
