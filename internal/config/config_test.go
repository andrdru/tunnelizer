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
		name       string
		setup      func(tt *testing.T) string
		expErr     bool
		expTunnels int
		expBody    string
	}{
		{
			name: "creates missing file with parent dirs",
			setup: func(tt *testing.T) string {
				return filepath.Join(tt.TempDir(), "nested", "config.yaml")
			},
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

			cfg, err := config.LoadOrCreate(path)
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

			reloaded, rerr := config.Load(path)
			require.NoError(tt, rerr)
			assert.Equal(tt, cfg.Aliases(), reloaded.Aliases())
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
