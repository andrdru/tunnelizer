package tunnel

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type execStarter struct{}

func (execStarter) Start(ctx context.Context, args []string) (Process, error) {
	cmd := exec.CommandContext(ctx, SSHBinary, args...) // #nosec G204 -- ssh args are built from user-owned config by design
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tunnel.Start: %w", err)
	}

	return &execProcess{cmd: cmd}, nil
}

type execProcess struct {
	cmd *exec.Cmd
}

func (p *execProcess) PID() int {
	return p.cmd.Process.Pid
}

func (p *execProcess) Wait() error {
	if err := p.cmd.Wait(); err != nil {
		return fmt.Errorf("tunnel.Wait: %w", err)
	}

	return nil
}

func (p *execProcess) Kill() error {
	if err := p.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("tunnel.Kill: %w", err)
	}

	return nil
}

type execMasterChecker struct{}

func (execMasterChecker) Alive(ctx context.Context, args []string) bool {
	cmd := exec.CommandContext(ctx, SSHBinary, args...) // #nosec G204 -- ssh args are built from user-owned config by design

	return cmd.Run() == nil
}
