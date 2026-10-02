package tunnel_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/andrdru/tunnelizer/internal/config"
	"github.com/andrdru/tunnelizer/internal/tunnel"
)

const testSock = "/run/tunz/db.sock"

func plainTunnel() config.ResolvedTunnel {
	return config.ResolvedTunnel{
		Alias:      "db",
		Host:       "bastion.example.com",
		LocalPort:  5432,
		RemoteHost: "db.internal",
		RemotePort: 5432,
	}
}

func fullTunnel() config.ResolvedTunnel {
	return config.ResolvedTunnel{
		Alias:        "db",
		Host:         "bastion.example.com",
		User:         "deploy",
		Port:         2222,
		IdentityFile: "/keys/id_ed25519",
		LocalPort:    5432,
		RemoteHost:   "db.internal",
		RemotePort:   5432,
	}
}

func TestSlaveArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.ResolvedTunnel
		exp  []string
	}{
		{
			name: "minimal tunnel",
			cfg:  plainTunnel(),
			exp: []string{
				"-N",
				"-L", "127.0.0.1:5432:db.internal:5432",
				"-o", "ExitOnForwardFailure=yes",
				"-o", "ServerAliveInterval=15",
				"-o", "ServerAliveCountMax=3",
				"-o", "BatchMode=yes",
				"bastion.example.com",
			},
		},
		{
			name: "full tunnel",
			cfg:  fullTunnel(),
			exp: []string{
				"-N",
				"-L", "127.0.0.1:5432:db.internal:5432",
				"-o", "ExitOnForwardFailure=yes",
				"-o", "ServerAliveInterval=15",
				"-o", "ServerAliveCountMax=3",
				"-o", "BatchMode=yes",
				"-i", "/keys/id_ed25519",
				"-p", "2222",
				"deploy@bastion.example.com",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			assert.Equal(tt, tc.exp, tunnel.SlaveArgs(tc.cfg, testSock))
		})
	}
}

func TestSlaveArgsInteractive(t *testing.T) {
	t.Parallel()

	cfg := fullTunnel()
	cfg.Interactive = true

	exp := []string{
		"-N",
		"-L", "127.0.0.1:5432:db.internal:5432",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "BatchMode=yes",
		"-S", testSock,
		"-o", "ControlMaster=no",
		"-i", "/keys/id_ed25519",
		"-p", "2222",
		"deploy@bastion.example.com",
	}

	assert.Equal(t, exp, tunnel.SlaveArgs(cfg, testSock))
}

func TestMasterArgs(t *testing.T) {
	t.Parallel()

	exp := []string{
		"-f", "-N", "-M",
		"-S", testSock,
		"-o", "ControlPersist=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-i", "/keys/id_ed25519",
		"-p", "2222",
		"deploy@bastion.example.com",
	}

	assert.Equal(t, exp, tunnel.MasterArgs(fullTunnel(), testSock))
}

func TestControlArgs(t *testing.T) {
	t.Parallel()

	cfg := fullTunnel()

	t.Run("check", func(tt *testing.T) {
		tt.Parallel()

		exp := []string{
			"-S", testSock, "-O", "check",
			"-i", "/keys/id_ed25519",
			"-p", "2222",
			"deploy@bastion.example.com",
		}
		assert.Equal(tt, exp, tunnel.CheckArgs(cfg, testSock))
	})

	t.Run("exit", func(tt *testing.T) {
		tt.Parallel()

		exp := []string{
			"-S", testSock, "-O", "exit",
			"-i", "/keys/id_ed25519",
			"-p", "2222",
			"deploy@bastion.example.com",
		}
		assert.Equal(tt, exp, tunnel.ExitArgs(cfg, testSock))
	})
}
