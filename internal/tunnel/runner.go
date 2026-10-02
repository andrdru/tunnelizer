package tunnel

//go:generate go tool mockgen -source=runner.go -destination=mocks/mock_runner.go -package=mocks

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/andrdru/tunnelizer/internal/config"
	"github.com/andrdru/tunnelizer/internal/state"
)

const (
	backoffInitial     = time.Second
	backoffMax         = 30 * time.Second
	backoffResetUptime = time.Minute
	backoffFactor      = 2
)

type Process interface {
	PID() int
	Wait() error
	Kill() error
}

type Starter interface {
	Start(ctx context.Context, args []string) (Process, error)
}

type MasterChecker interface {
	Alive(ctx context.Context, args []string) bool
}

type Option func(*Runner)

func WithStarter(s Starter) Option {
	return func(r *Runner) { r.starter = s }
}

func WithMasterChecker(m MasterChecker) Option {
	return func(r *Runner) { r.master = m }
}

func WithTimeNowFunc(f func() time.Time) Option {
	return func(r *Runner) { r.timeNowFunc = f }
}

func WithSleepFunc(f func(ctx context.Context, d time.Duration) bool) Option {
	return func(r *Runner) { r.sleepFunc = f }
}

func WithLogger(l *slog.Logger) Option {
	return func(r *Runner) { r.log = l }
}

type Runner struct {
	cfg         config.ResolvedTunnel
	store       *state.Store
	sockPath    string
	starter     Starter
	master      MasterChecker
	timeNowFunc func() time.Time
	sleepFunc   func(ctx context.Context, d time.Duration) bool
	log         *slog.Logger

	startedAt time.Time
	restarts  int
}

func NewRunner(cfg config.ResolvedTunnel, store *state.Store, sockPath string, opts ...Option) *Runner {
	r := &Runner{
		cfg:         cfg,
		store:       store,
		sockPath:    sockPath,
		starter:     execStarter{},
		master:      execMasterChecker{},
		timeNowFunc: time.Now,
		sleepFunc:   sleepCtx,
		log:         slog.Default(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

func (r *Runner) Run(ctx context.Context) error {
	r.startedAt = r.timeNowFunc()

	backoff := backoffInitial

	for {
		if r.cfg.Interactive && !r.master.Alive(ctx, CheckArgs(r.cfg, r.sockPath)) {
			r.save(state.StatusAuthRequired, 0, "master connection is not alive")
			r.log.Warn("master connection is not alive, run tunz up again", "alias", r.cfg.Alias)

			return nil
		}

		started := r.timeNowFunc()

		proc, err := r.starter.Start(ctx, SlaveArgs(r.cfg, r.sockPath))
		if err == nil {
			r.save(state.StatusUp, proc.PID(), "")
			err = r.wait(ctx, proc)
		}

		if ctx.Err() != nil {
			r.save(state.StatusDown, 0, "")

			//nolint:nilerr // процесс убит из-за отмены контекста: это штатное гашение, а не сбой
			return nil
		}

		r.restarts++
		r.save(state.StatusReconnecting, 0, errText(err))
		r.log.Warn("tunnel exited, reconnecting", "alias", r.cfg.Alias, "backoff", backoff, "error", errText(err))

		if r.timeNowFunc().Sub(started) >= backoffResetUptime {
			backoff = backoffInitial
		}

		if !r.sleepFunc(ctx, backoff) {
			r.save(state.StatusDown, 0, "")

			return nil
		}

		backoff = min(backoff*backoffFactor, backoffMax)
	}
}

func (r *Runner) wait(ctx context.Context, proc Process) error {
	done := make(chan error, 1)

	go func() { done <- proc.Wait() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if err := proc.Kill(); err != nil {
			r.log.Warn("failed to kill ssh process", "alias", r.cfg.Alias, "error", err.Error())
		}

		return <-done
	}
}

func (r *Runner) save(status state.Status, sshPID int, lastErr string) {
	st := state.State{
		Alias:     r.cfg.Alias,
		Status:    status,
		PID:       os.Getpid(),
		SSHPID:    sshPID,
		StartedAt: r.startedAt,
		Restarts:  r.restarts,
		LastError: lastErr,
	}

	if err := r.store.Write(st); err != nil {
		r.log.Error("failed to write state", "alias", r.cfg.Alias, "error", err.Error())
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
