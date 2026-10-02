package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrdru/tunnelizer/internal/config"
)

const (
	configFilePerm = 0o600
	testAlias      = "db"

	validConfigBody = `
tunnels:
  db:
    host: bastion.example.com
    local_port: 5432
    remote_host: db.internal
    remote_port: 5432
`
	brokenConfigBody = "tunnels: ["
)

func boolPtr(value bool) *bool {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func strPtr(value string) *string {
	return &value
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), configFilePerm))

	return path
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   string
		expErr error
	}{
		{
			name: "valid config with defaults",
			body: `
defaults:
  user: deploy
  port: 22
  remote_host: db.internal
  remote_port: 5432
tunnels:
  db:
    host: bastion.example.com
    local_port: 5432
`,
		},
		{
			name: "required fields missing after merge",
			body: `
tunnels:
  db:
    host: bastion.example.com
`,
			expErr: config.ErrInvalidConfig,
		},
		{
			name: "invalid port range",
			body: `
tunnels:
  db:
    host: bastion.example.com
    local_port: 70000
    remote_host: db.internal
    remote_port: 5432
`,
			expErr: config.ErrInvalidConfig,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			cfg, err := config.Load(writeConfig(tt, tc.body))
			if tc.expErr != nil {
				require.ErrorIs(tt, err, tc.expErr)

				return
			}

			require.NoError(tt, err)
			assert.NotNil(tt, cfg)
		})
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing file", func(tt *testing.T) {
		tt.Parallel()

		_, err := config.Load(filepath.Join(tt.TempDir(), "absent.yaml"))

		require.ErrorIs(tt, err, os.ErrNotExist)
	})

	t.Run("broken yaml", func(tt *testing.T) {
		tt.Parallel()

		_, err := config.Load(writeConfig(tt, "tunnels: ["))

		require.Error(tt, err)
	})
}

func TestLoadOrCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(tt *testing.T) string
		seed        config.Defaults
		expErr      bool
		expTunnels  int
		expDefaults config.Defaults
		expBody     string
	}{
		{
			name: "creates missing file with parent dirs",
			setup: func(tt *testing.T) string {
				return filepath.Join(tt.TempDir(), "nested", "config.yaml")
			},
		},
		{
			name: "writes seed defaults on create",
			setup: func(tt *testing.T) string {
				return filepath.Join(tt.TempDir(), "config.yaml")
			},
			seed:        config.Defaults{User: "vmp", Port: config.DefaultSSHPort},
			expDefaults: config.Defaults{User: "vmp", Port: config.DefaultSSHPort},
		},
		{
			name: "keeps defaults of existing config",
			setup: func(tt *testing.T) string {
				return writeConfig(tt, "defaults:\n  user: deploy\n  port: 2222\n")
			},
			seed:        config.Defaults{User: "vmp", Port: config.DefaultSSHPort},
			expDefaults: config.Defaults{User: "deploy", Port: 2222},
		},
		{
			name: "loads existing config",
			setup: func(tt *testing.T) string {
				return writeConfig(tt, validConfigBody)
			},
			expTunnels: 1,
		},
		{
			name: "keeps broken yaml untouched",
			setup: func(tt *testing.T) string {
				return writeConfig(tt, brokenConfigBody)
			},
			expErr:  true,
			expBody: brokenConfigBody,
		},
		{
			name: "reports save failure",
			setup: func(tt *testing.T) string {
				blocker := filepath.Join(tt.TempDir(), "blocker")
				require.NoError(tt, os.WriteFile(blocker, nil, configFilePerm))

				return filepath.Join(blocker, "config.yaml")
			},
			expErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			path := tc.setup(tt)

			cfg, err := config.LoadOrCreate(path, tc.seed)
			if tc.expErr {
				require.Error(tt, err)

				if tc.expBody != "" {
					data, rerr := os.ReadFile(path) // #nosec G304 -- path points to a file created by the test
					require.NoError(tt, rerr)
					assert.Equal(tt, tc.expBody, string(data))
				}

				return
			}

			require.NoError(tt, err)
			assert.Len(tt, cfg.Tunnels, tc.expTunnels)
			assert.Equal(tt, tc.expDefaults, cfg.Defaults)

			reloaded, rerr := config.Load(path)
			require.NoError(tt, rerr)
			assert.Equal(tt, cfg.Aliases(), reloaded.Aliases())
			assert.Equal(tt, tc.expDefaults, reloaded.Defaults)
		})
	}
}

func TestLoadAggregatesProblems(t *testing.T) {
	t.Parallel()

	_, err := config.Load(writeConfig(t, `
tunnels:
  first:
    host: bastion
  second:
    local_port: 1000
`))
	require.ErrorIs(t, err, config.ErrInvalidConfig)

	var validationErr config.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Len(t, validationErr.Problems, 6)
}

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cfg    *config.Config
		alias  string
		exp    config.ResolvedTunnel
		expErr error
	}{
		{
			name: "inherits defaults",
			cfg: &config.Config{
				Defaults: config.Defaults{
					User: "deploy", Port: 22, RemoteHost: "db.internal", RemotePort: 5432, Interactive: true,
				},
				Tunnels: map[string]config.Tunnel{
					testAlias: {Host: "bastion.example.com", LocalPort: 5432},
				},
			},
			alias: testAlias,
			exp: config.ResolvedTunnel{
				Alias: testAlias, Host: "bastion.example.com", User: "deploy", Port: 22,
				LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432, Interactive: true,
			},
		},
		{
			name: "tunnel overrides defaults",
			cfg: &config.Config{
				Defaults: config.Defaults{User: "deploy", Port: 22, RemoteHost: "db.internal", RemotePort: 5432},
				Tunnels: map[string]config.Tunnel{
					testAlias: {
						Host: "bastion", User: "root", Port: 2222, IdentityFile: "/keys/id",
						LocalPort: 15432, RemoteHost: "other.internal", RemotePort: 6432,
					},
				},
			},
			alias: testAlias,
			exp: config.ResolvedTunnel{
				Alias: testAlias, Host: "bastion", User: "root", Port: 2222, IdentityFile: "/keys/id",
				LocalPort: 15432, RemoteHost: "other.internal", RemotePort: 6432,
			},
		},
		{
			name: "interactive explicitly disabled",
			cfg: &config.Config{
				Defaults: config.Defaults{RemoteHost: "db.internal", RemotePort: 5432, Interactive: true},
				Tunnels: map[string]config.Tunnel{
					testAlias: {Host: "bastion", LocalPort: 5432, Interactive: boolPtr(false)},
				},
			},
			alias: testAlias,
			exp: config.ResolvedTunnel{
				Alias: testAlias, Host: "bastion", LocalPort: 5432,
				RemoteHost: "db.internal", RemotePort: 5432, Interactive: false,
			},
		},
		{
			name: "dsn does not change resolved tunnel",
			cfg: &config.Config{
				Tunnels: map[string]config.Tunnel{
					testAlias: {
						Host: "bastion", LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432,
						DSN: "jdbc:mysql://{host}:{port}/app",
					},
				},
			},
			alias: testAlias,
			exp: config.ResolvedTunnel{
				Alias: testAlias, Host: "bastion", LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432,
			},
		},
		{
			name:   "unknown alias",
			cfg:    &config.Config{Tunnels: map[string]config.Tunnel{}},
			alias:  "absent",
			expErr: config.ErrTunnelNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			rt, err := tc.cfg.Resolve(tc.alias)
			if tc.expErr != nil {
				require.ErrorIs(tt, err, tc.expErr)

				return
			}

			require.NoError(tt, err)
			assert.Equal(tt, tc.exp, rt)
		})
	}
}

func TestAdd(t *testing.T) {
	t.Parallel()

	t.Run("adds new alias", func(tt *testing.T) {
		tt.Parallel()

		cfg := &config.Config{}
		require.NoError(tt, cfg.Add(testAlias, config.Tunnel{Host: "bastion", LocalPort: 5432}))
		assert.Equal(tt, []string{testAlias}, cfg.Aliases())
	})

	t.Run("rejects duplicate", func(tt *testing.T) {
		tt.Parallel()

		cfg := &config.Config{Tunnels: map[string]config.Tunnel{testAlias: {Host: "bastion"}}}
		err := cfg.Add(testAlias, config.Tunnel{Host: "other"})

		require.ErrorIs(tt, err, config.ErrExists)
	})
}

func TestSaveLoadRoundtrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg := &config.Config{
		Defaults: config.Defaults{User: "deploy", Port: 22, RemoteHost: "db.internal", RemotePort: 5432},
		Tunnels: map[string]config.Tunnel{
			testAlias: {Host: "bastion.example.com", LocalPort: 5432, Interactive: boolPtr(true)},
		},
	}

	require.NoError(t, config.Save(path, cfg))

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, cfg, loaded)
}

func TestResolvedTunnelValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rt     config.ResolvedTunnel
		expErr error
	}{
		{
			name: "valid",
			rt: config.ResolvedTunnel{
				Alias: testAlias, Host: "bastion", LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432, Port: 22,
			},
		},
		{
			name: "port out of range",
			rt: config.ResolvedTunnel{
				Alias: testAlias, Host: "bastion", LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432, Port: 70000,
			},
			expErr: config.ErrInvalidConfig,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			err := tc.rt.Validate()
			if tc.expErr != nil {
				require.ErrorIs(tt, err, tc.expErr)

				return
			}

			require.NoError(tt, err)
		})
	}
}

func TestTunnelWith(t *testing.T) {
	t.Parallel()

	base := config.Tunnel{
		Host: "bastion", User: "deploy", Port: 22, IdentityFile: "/keys/id",
		LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432,
		DSN: "jdbc:postgresql://{host}:{port}/app", Interactive: boolPtr(true),
	}

	tests := []struct {
		name  string
		patch config.TunnelPatch
		exp   config.Tunnel
	}{
		{
			name:  "empty patch keeps tunnel",
			patch: config.TunnelPatch{},
			exp: config.Tunnel{
				Host: "bastion", User: "deploy", Port: 22, IdentityFile: "/keys/id",
				LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432,
				DSN: "jdbc:postgresql://{host}:{port}/app", Interactive: boolPtr(true),
			},
		},
		{
			name:  "patches only given fields",
			patch: config.TunnelPatch{LocalPort: intPtr(15432), DSN: strPtr("jdbc:mysql://{host}:{port}/app")},
			exp: config.Tunnel{
				Host: "bastion", User: "deploy", Port: 22, IdentityFile: "/keys/id",
				LocalPort: 15432, RemoteHost: "db.internal", RemotePort: 5432,
				DSN: "jdbc:mysql://{host}:{port}/app", Interactive: boolPtr(true),
			},
		},
		{
			name:  "empty values clear fields",
			patch: config.TunnelPatch{User: strPtr(""), Port: intPtr(0), DSN: strPtr("")},
			exp: config.Tunnel{
				Host: "bastion", IdentityFile: "/keys/id",
				LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432,
				Interactive: boolPtr(true),
			},
		},
		{
			name:  "interactive disabled",
			patch: config.TunnelPatch{Interactive: boolPtr(false)},
			exp: config.Tunnel{
				Host: "bastion", User: "deploy", Port: 22, IdentityFile: "/keys/id",
				LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432,
				DSN: "jdbc:postgresql://{host}:{port}/app", Interactive: boolPtr(false),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			assert.Equal(tt, tc.exp, base.With(tc.patch))
		})
	}
}

func TestDefaultsWith(t *testing.T) {
	t.Parallel()

	base := config.Defaults{
		User: "deploy", Port: 22, IdentityFile: "~/.ssh/id_ed25519",
		RemoteHost: "db.internal", RemotePort: 5432, Interactive: true,
	}

	tests := []struct {
		name  string
		patch config.DefaultsPatch
		exp   config.Defaults
	}{
		{
			name:  "empty patch keeps defaults",
			patch: config.DefaultsPatch{},
			exp: config.Defaults{
				User: "deploy", Port: 22, IdentityFile: "~/.ssh/id_ed25519",
				RemoteHost: "db.internal", RemotePort: 5432, Interactive: true,
			},
		},
		{
			name:  "patches port and interactive",
			patch: config.DefaultsPatch{Port: intPtr(2222), Interactive: boolPtr(false)},
			exp: config.Defaults{
				User: "deploy", Port: 2222, IdentityFile: "~/.ssh/id_ed25519",
				RemoteHost: "db.internal", RemotePort: 5432,
			},
		},
		{
			name:  "clears user and remote host",
			patch: config.DefaultsPatch{User: strPtr(""), RemoteHost: strPtr("")},
			exp: config.Defaults{
				Port: 22, IdentityFile: "~/.ssh/id_ed25519", RemotePort: 5432, Interactive: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			assert.Equal(tt, tc.exp, base.With(tc.patch))
		})
	}
}

func TestTunnelDiff(t *testing.T) {
	t.Parallel()

	base := config.Tunnel{
		Host: "bastion", User: "deploy", Port: 22, IdentityFile: "/keys/id",
		LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432,
		DSN: "jdbc:postgresql://{host}:{port}/app", Interactive: boolPtr(true),
	}

	tests := []struct {
		name   string
		tun    config.Tunnel
		mutate func(t *config.Tunnel)
		exp    config.TunnelPatch
	}{
		{
			name:   "unchanged tunnel gives empty patch",
			tun:    base,
			mutate: func(*config.Tunnel) {},
			exp:    config.TunnelPatch{},
		},
		{
			name:   "patches changed local port only",
			tun:    base,
			mutate: func(t *config.Tunnel) { t.LocalPort = 15432 },
			exp:    config.TunnelPatch{LocalPort: intPtr(15432)},
		},
		{
			name: "patches changed host and dsn",
			tun:  base,
			mutate: func(t *config.Tunnel) {
				t.Host = "other.example.com"
				t.DSN = "jdbc:mysql://{host}:{port}/app"
			},
			exp: config.TunnelPatch{Host: strPtr("other.example.com"), DSN: strPtr("jdbc:mysql://{host}:{port}/app")},
		},
		{
			name:   "cleared user gives empty string pointer",
			tun:    base,
			mutate: func(t *config.Tunnel) { t.User = "" },
			exp:    config.TunnelPatch{User: strPtr("")},
		},
		{
			name:   "interactive enabled over nil",
			tun:    config.Tunnel{Host: "bastion"},
			mutate: func(t *config.Tunnel) { t.Interactive = boolPtr(true) },
			exp:    config.TunnelPatch{Interactive: boolPtr(true)},
		},
		{
			name:   "interactive disabled",
			tun:    base,
			mutate: func(t *config.Tunnel) { t.Interactive = boolPtr(false) },
			exp:    config.TunnelPatch{Interactive: boolPtr(false)},
		},
		{
			name:   "interactive cleared to nil is not expressible by patch",
			tun:    base,
			mutate: func(t *config.Tunnel) { t.Interactive = nil },
			exp:    config.TunnelPatch{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			next := tc.tun
			tc.mutate(&next)

			assert.Equal(tt, tc.exp, tc.tun.Diff(next))
		})
	}
}

func TestSet(t *testing.T) {
	t.Parallel()

	t.Run("inserts into nil map", func(tt *testing.T) {
		tt.Parallel()

		cfg := &config.Config{}
		cfg.Set(testAlias, config.Tunnel{Host: "bastion", LocalPort: 5432})

		assert.Equal(tt, []string{testAlias}, cfg.Aliases())
	})

	t.Run("replaces existing alias", func(tt *testing.T) {
		tt.Parallel()

		cfg := &config.Config{Tunnels: map[string]config.Tunnel{testAlias: {Host: "bastion", LocalPort: 5432}}}
		cfg.Set(testAlias, config.Tunnel{Host: "other", LocalPort: 15432})

		assert.Equal(tt, map[string]config.Tunnel{testAlias: {Host: "other", LocalPort: 15432}}, cfg.Tunnels)
	})
}

func TestEffective(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cfg    *config.Config
		alias  string
		exp    config.Tunnel
		expErr error
	}{
		{
			name: "merges defaults without expanding home",
			cfg: &config.Config{
				Defaults: config.Defaults{
					User: "deploy", Port: config.DefaultSSHPort, IdentityFile: "~/.ssh/id_ed25519",
					RemoteHost: "db.internal", RemotePort: 5432,
				},
				Tunnels: map[string]config.Tunnel{
					testAlias: {Host: "bastion.example.com", LocalPort: 5432},
				},
			},
			alias: testAlias,
			exp: config.Tunnel{
				Host: "bastion.example.com", User: "deploy", Port: config.DefaultSSHPort,
				IdentityFile: "~/.ssh/id_ed25519", LocalPort: 5432,
				RemoteHost: "db.internal", RemotePort: 5432,
			},
		},
		{
			name: "keeps tunnel values and dsn",
			cfg: &config.Config{
				Defaults: config.Defaults{
					User: "deploy", Port: config.DefaultSSHPort, RemoteHost: "db.internal", RemotePort: 5432,
				},
				Tunnels: map[string]config.Tunnel{
					testAlias: {
						Host: "bastion.example.com", User: "root", Port: 2222, IdentityFile: "~/.ssh/id_root",
						LocalPort: 15432, DSN: "jdbc:mysql://{host}:{port}/app",
					},
				},
			},
			alias: testAlias,
			exp: config.Tunnel{
				Host: "bastion.example.com", User: "root", Port: 2222, IdentityFile: "~/.ssh/id_root",
				LocalPort: 15432, RemoteHost: "db.internal", RemotePort: 5432,
				DSN: "jdbc:mysql://{host}:{port}/app",
			},
		},
		{
			name: "interactive inherited from defaults",
			cfg: &config.Config{
				Defaults: config.Defaults{Interactive: true},
				Tunnels:  map[string]config.Tunnel{testAlias: {Host: "bastion", LocalPort: 5432}},
			},
			alias: testAlias,
			exp:   config.Tunnel{Host: "bastion", LocalPort: 5432, Interactive: boolPtr(true)},
		},
		{
			name: "interactive disabled explicitly",
			cfg: &config.Config{
				Defaults: config.Defaults{Interactive: true},
				Tunnels: map[string]config.Tunnel{
					testAlias: {Host: "bastion", LocalPort: 5432, Interactive: boolPtr(false)},
				},
			},
			alias: testAlias,
			exp:   config.Tunnel{Host: "bastion", LocalPort: 5432},
		},
		{
			name:   "unknown alias",
			cfg:    &config.Config{Tunnels: map[string]config.Tunnel{}},
			alias:  "absent",
			expErr: config.ErrTunnelNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			eff, err := tc.cfg.Effective(tc.alias)
			if tc.expErr != nil {
				require.ErrorIs(tt, err, tc.expErr)

				return
			}

			require.NoError(tt, err)
			assert.Equal(tt, tc.exp, eff)
		})
	}
}

func TestDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tun    config.Tunnel
		alias  string
		exp    string
		expErr error
	}{
		{
			name:  "default template",
			tun:   config.Tunnel{Host: "bastion", LocalPort: 5433},
			alias: testAlias,
			exp:   "jdbc:postgresql://127.0.0.1:5433/postgres",
		},
		{
			name:  "custom template replaces both placeholders",
			tun:   config.Tunnel{Host: "bastion", LocalPort: 5433, DSN: "jdbc:mysql://{host}:{port}/app?ssl=false"},
			alias: testAlias,
			exp:   "jdbc:mysql://127.0.0.1:5433/app?ssl=false",
		},
		{
			name:  "template without placeholders is returned as is",
			tun:   config.Tunnel{Host: "bastion", LocalPort: 5433, DSN: "jdbc:postgresql://db.internal:5432/prod"},
			alias: testAlias,
			exp:   "jdbc:postgresql://db.internal:5432/prod",
		},
		{
			name:   "unknown alias",
			alias:  "absent",
			expErr: config.ErrTunnelNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			cfg := &config.Config{Tunnels: map[string]config.Tunnel{testAlias: tc.tun}}

			dsn, err := cfg.DSN(tc.alias)
			if tc.expErr != nil {
				require.ErrorIs(tt, err, tc.expErr)

				return
			}

			require.NoError(tt, err)
			assert.Equal(tt, tc.exp, dsn)
		})
	}
}
