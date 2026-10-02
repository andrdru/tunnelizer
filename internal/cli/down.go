package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/andrdru/tunnelizer/internal/config"
	"github.com/andrdru/tunnelizer/internal/state"
	"github.com/andrdru/tunnelizer/internal/tunnel"
)

const (
	downTimeout  = 5 * time.Second
	downPollStep = 100 * time.Millisecond
	dirPerm      = 0o700
)

func runDown(paths Paths, args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	all := fs.Bool("all", false, "bring down all tunnels from config and state")

	positional, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("cli.down: %w", err)
	}

	cfg, err := loadConfig(paths)
	if err != nil {
		return fmt.Errorf("cli.down: %w", err)
	}

	ctx := context.Background()
	store := state.NewStore(paths.StateDir)

	states, err := store.List()
	if err != nil {
		return fmt.Errorf("cli.down: %w", err)
	}

	aliases, err := pickAliases(*all, positional, unionAliases(cfg, states))
	if err != nil {
		return fmt.Errorf("cli.down: %w", err)
	}

	failed := false

	for _, alias := range aliases {
		if derr := downOne(ctx, cfg, store, alias); derr != nil {
			fmt.Fprintf(os.Stderr, "tunz down %s: %v\n", alias, derr)

			failed = true

			continue
		}
	}

	if failed {
		return fmt.Errorf("cli.down: %w", ErrPartialFailure)
	}

	return nil
}

func downOne(ctx context.Context, cfg *config.Config, store *state.Store, alias string) error {
	pid, err := store.ReadPID(alias)

	switch {
	case errors.Is(err, state.ErrStateNotFound):
		fmt.Printf("%s: not running\n", alias)
	case err != nil:
		return fmt.Errorf("cli.downOne: %w", err)
	default:
		terminate(pid)
		fmt.Printf("%s: down\n", alias)
	}

	killOrphanSSH(store, alias)

	rt, rerr := cfg.Resolve(alias)
	if rerr == nil && (rt.Interactive || fileExists(store.SockPath(alias))) {
		sockPath := store.SockPath(alias)
		stopMaster(ctx, rt, sockPath)
		removeSock(sockPath)
	}

	if err := store.Remove(alias); err != nil {
		return fmt.Errorf("cli.downOne: %w", err)
	}

	return nil
}

// killOrphanSSH добивает ssh-процесс, оставшийся от умершего раннера: иначе локальный порт
// остаётся занятым и следующий up не поднимется.
func killOrphanSSH(store *state.Store, alias string) {
	st, err := store.Read(alias)
	if err != nil || st.SSHPID == 0 || !state.PIDAlive(st.SSHPID) {
		return
	}

	if state.ProcessName(st.SSHPID) != tunnel.SSHBinary {
		fmt.Fprintf(os.Stderr, "tunz: skip pid %d of %s: not an ssh process\n", st.SSHPID, alias)

		return
	}

	terminate(st.SSHPID)
}

func terminate(pid int) {
	if !state.PIDAlive(pid) {
		return
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "tunz: failed to send SIGTERM to %d: %v\n", pid, err)

		return
	}

	deadline := time.Now().Add(downTimeout)

	for time.Now().Before(deadline) {
		if !state.PIDAlive(pid) {
			return
		}

		time.Sleep(downPollStep)
	}

	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		fmt.Fprintf(os.Stderr, "tunz: failed to kill %d: %v\n", pid, err)
	}
}

func stopMaster(ctx context.Context, rt config.ResolvedTunnel, sockPath string) {
	cmd := exec.CommandContext(ctx, tunnel.SSHBinary, tunnel.ExitArgs(rt, sockPath)...) // #nosec G204 -- ssh args come from user config

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tunz: failed to stop master for %s: %v\n", rt.Alias, err)
	}
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

// removeSock удаляет оставшийся файл мастер-сокета: сокет лежит в нашем state-каталоге
// и создан под наш ssh -S, поэтому файл заведомо наш. Не сокет — не трогаем.
func removeSock(path string) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "tunz: failed to inspect %s: %v\n", path, err)

		return
	}

	if info.Mode()&os.ModeSocket == 0 {
		fmt.Fprintf(os.Stderr, "tunz: skip %s: not a socket\n", path)

		return
	}

	if err := os.Remove(path); err != nil {
		fmt.Fprintf(os.Stderr, "tunz: failed to remove %s: %v\n", path, err)
	}
}
