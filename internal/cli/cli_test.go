package cli_test

import (
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrdru/tunnelizer/internal/cli"
	"github.com/andrdru/tunnelizer/internal/config"
)

const (
	testDirPerm  = 0o700
	testFilePerm = 0o600
)

func boolPtr(value bool) *bool {
	return &value
}

func configPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(os.Getenv("HOME"), ".config", "tunnelizer", "config.yaml")
}

func writeTestConfig(t *testing.T, body string) {
	t.Helper()

	path := configPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), testDirPerm))
	require.NoError(t, os.WriteFile(path, []byte(body), testFilePerm))
}

func captureStdout(t *testing.T, f func() error) (string, error) {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	original := os.Stdout
	os.Stdout = w

	runErr := f()

	require.NoError(t, w.Close())

	os.Stdout = original

	data, err := io.ReadAll(r)
	require.NoError(t, err)

	return string(data), runErr
}

func TestAddWithFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // подмена HOME: t.Parallel() нельзя

	require.NoError(t, cli.Execute([]string{
		"add",
		"--alias", "db",
		"--host", "bastion.example.com",
		"--user", "deploy",
		"--port", "2222",
		"--local-port", "5433",
		"--remote-host", "db.internal",
		"--remote-port", "5432",
		"--identity-file", "~/.ssh/id_ed25519",
		"--dsn", "jdbc:postgresql://{host}:{port}/app",
		"--interactive=false",
	}))

	cfg, err := config.Load(configPath(t))
	require.NoError(t, err)

	assert.Equal(t, config.Tunnel{
		Host: "bastion.example.com", User: "deploy", Port: 2222,
		IdentityFile: "~/.ssh/id_ed25519", LocalPort: 5433,
		RemoteHost: "db.internal", RemotePort: 5432,
		DSN: "jdbc:postgresql://{host}:{port}/app", Interactive: boolPtr(false),
	}, cfg.Tunnels["db"])
}

func TestEdit(t *testing.T) {
	baseBody := `
defaults:
  user: deploy
  port: 22
  remote_host: db.internal
  remote_port: 5432
tunnels:
  db:
    host: bastion.example.com
    local_port: 5433
`

	t.Run("single flag patch keeps inherited fields raw", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir()) // подмена HOME: t.Parallel() нельзя
		writeTestConfig(t, baseBody)

		require.NoError(t, cli.Execute([]string{"edit", "db", "--local-port", "5434"}))

		cfg, err := config.Load(configPath(t))
		require.NoError(t, err)
		assert.Equal(t, config.Tunnel{Host: "bastion.example.com", LocalPort: 5434}, cfg.Tunnels["db"])
	})

	t.Run("unknown alias", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeTestConfig(t, baseBody)

		err := cli.Execute([]string{"edit", "nosuch", "--local-port", "5434"})

		require.ErrorIs(t, err, config.ErrTunnelNotFound)
	})

	t.Run("invalid result leaves file untouched", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeTestConfig(t, baseBody)

		before, err := os.ReadFile(configPath(t)) // #nosec G304 -- path points to a file created by the test
		require.NoError(t, err)

		err = cli.Execute([]string{"edit", "db", "--local-port", "70000"})
		require.ErrorIs(t, err, config.ErrInvalidConfig)

		after, rerr := os.ReadFile(configPath(t)) // #nosec G304 -- path points to a file created by the test
		require.NoError(t, rerr)
		assert.Equal(t, string(before), string(after))
	})
}

func TestDefaults(t *testing.T) {
	t.Run("patch and clear", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeTestConfig(t, `
defaults:
  user: deploy
  port: 22
tunnels:
  db:
    host: bastion.example.com
    local_port: 5433
    remote_host: db.internal
    remote_port: 5432
`)

		require.NoError(t, cli.Execute([]string{"defaults", "--port", "2222", "--user", ""}))

		cfg, err := config.Load(configPath(t))
		require.NoError(t, err)
		assert.Equal(t, config.Defaults{Port: 2222}, cfg.Defaults)
	})

	t.Run("breaking clear rejected", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeTestConfig(t, `
defaults:
  remote_host: db.internal
  remote_port: 5432
tunnels:
  db:
    host: bastion.example.com
    local_port: 5433
`)

		before, err := os.ReadFile(configPath(t)) // #nosec G304 -- path points to a file created by the test
		require.NoError(t, err)

		err = cli.Execute([]string{"defaults", "--remote-port", "0"})
		require.ErrorIs(t, err, config.ErrInvalidConfig)

		after, rerr := os.ReadFile(configPath(t)) // #nosec G304 -- path points to a file created by the test
		require.NoError(t, rerr)
		assert.Equal(t, string(before), string(after))
	})
}

func TestExportCMD(t *testing.T) {
	tests := []struct {
		name string
		body string
		exp  string
	}{
		{
			name: "full tunnel",
			body: `
defaults:
  user: deploy
  port: 22
tunnels:
  db:
    host: bastion.example.com
    local_port: 5433
    remote_host: db.internal
    remote_port: 5432
    identity_file: ~/.ssh/id_ed25519
    dsn: jdbc:postgresql://{host}:{port}/app
    interactive: true
`,
			exp: "tunz add --alias db --host bastion.example.com --user deploy --port 22 --local-port 5433" +
				" --remote-host db.internal --remote-port 5432 --identity-file ~/.ssh/id_ed25519" +
				" --dsn 'jdbc:postgresql://{host}:{port}/app' --interactive=true",
		},
		{
			name: "quoting and explicit false",
			body: `
defaults:
  user: deploy
  port: 22
tunnels:
  db:
    host: bastion
    user: my user
    local_port: 5433
    remote_host: db.internal
    remote_port: 5432
`,
			exp: "tunz add --alias db --host bastion --user 'my user' --port 22 --local-port 5433" +
				" --remote-host db.internal --remote-port 5432 --interactive=false",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir()) // подмена HOME: t.Parallel() нельзя
			writeTestConfig(t, tc.body)

			out, err := captureStdout(t, func() error {
				return cli.Execute([]string{"export", "db"})
			})
			require.NoError(t, err)
			assert.Equal(t, tc.exp+"\n", out)
		})
	}
}

func TestExportYAML(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // подмена HOME: t.Parallel() нельзя
	writeTestConfig(t, `
defaults:
  user: deploy
  port: 22
tunnels:
  db:
    host: bastion.example.com
    local_port: 5433
    remote_host: db.internal
    remote_port: 5432
    identity_file: ~/.ssh/id_ed25519
    dsn: jdbc:postgresql://{host}:{port}/app
    interactive: true
`)

	out, err := captureStdout(t, func() error {
		return cli.Execute([]string{"export", "db", "--format", "yaml"})
	})
	require.NoError(t, err)

	expBody := `tunnels:
    db:
        host: bastion.example.com
        user: deploy
        port: 22
        identity_file: ~/.ssh/id_ed25519
        local_port: 5433
        remote_host: db.internal
        remote_port: 5432
        dsn: jdbc:postgresql://{host}:{port}/app
        interactive: true
`
	assert.Equal(t, expBody, out)

	roundtrip := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(roundtrip, []byte(out), testFilePerm))

	reloaded, err := config.Load(roundtrip)
	require.NoError(t, err)
	assert.Equal(t, config.Tunnel{
		Host: "bastion.example.com", User: "deploy", Port: 22,
		IdentityFile: "~/.ssh/id_ed25519", LocalPort: 5433,
		RemoteHost: "db.internal", RemotePort: 5432,
		DSN: "jdbc:postgresql://{host}:{port}/app", Interactive: boolPtr(true),
	}, reloaded.Tunnels["db"])
}

func TestExportDSN(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // подмена HOME: t.Parallel() нельзя
	writeTestConfig(t, `
tunnels:
  db:
    host: bastion.example.com
    local_port: 5433
    remote_host: db.internal
    remote_port: 5432
`)

	out, err := captureStdout(t, func() error {
		return cli.Execute([]string{"export", "db", "--format", "dsn"})
	})
	require.NoError(t, err)
	assert.Equal(t, "jdbc:postgresql://127.0.0.1:5433/postgres\n", out)
}

func TestExportUnknownAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // подмена HOME: t.Parallel() нельзя
	writeTestConfig(t, `
tunnels:
  db:
    host: bastion.example.com
    local_port: 5433
    remote_host: db.internal
    remote_port: 5432
`)

	err := cli.Execute([]string{"export", "nosuch"})

	require.ErrorIs(t, err, config.ErrTunnelNotFound)
}

func TestLsRendersTable(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // подмена HOME: t.Parallel() нельзя
	writeTestConfig(t, `
tunnels:
  db:
    host: bastion.example.com
    local_port: 5433
    remote_host: db.internal
    remote_port: 5432
    dsn: jdbc:postgresql://{host}:{port}/app
  mq:
    host: bastion.example.com
    local_port: 5672
    remote_host: mq.internal
    remote_port: 5672
`)

	out, err := captureStdout(t, func() error {
		return cli.Execute([]string{"ls"})
	})
	require.NoError(t, err)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 7)

	for _, line := range lines {
		assert.Equal(t, utf8.RuneCountInString(lines[0]), utf8.RuneCountInString(line), "ragged table: %q", line)
	}

	assert.Contains(t, lines[1], "ALIAS")
	assert.Contains(t, lines[3], "127.0.0.1:5433")
	assert.Contains(t, lines[5], "127.0.0.1:5672")
	assert.NotContains(t, out, "jdbc:")
}

func TestAliasRequiredWithoutTTY(t *testing.T) {
	body := `
tunnels:
  db:
    host: bastion.example.com
    local_port: 5433
    remote_host: db.internal
    remote_port: 5432
`

	tests := []struct {
		name string
		args []string
		exp  error
	}{
		{name: "up", args: []string{"up"}, exp: cli.ErrNoAlias},
		{name: "down", args: []string{"down"}, exp: cli.ErrNoAlias},
		{name: "edit", args: []string{"edit"}, exp: cli.ErrUsage},
		{name: "export", args: []string{"export"}, exp: cli.ErrUsage},
		{name: "export two aliases", args: []string{"export", "db", "mq"}, exp: cli.ErrUsage},
	}

	// stdin теста — /dev/null, а это тоже символьное устройство: проверка на терминал обязана
	// исключать его, иначе пикер откроется вне терминала и тест зависнет.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir()) // подмена HOME: t.Parallel() нельзя
			writeTestConfig(t, body)

			err := cli.Execute(tc.args)

			require.ErrorIs(t, err, tc.exp)
		})
	}
}

func TestAliasRequiredEmptyConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // подмена HOME: t.Parallel() нельзя

	err := cli.Execute([]string{"up"})

	require.ErrorIs(t, err, cli.ErrNoAlias)
}

func TestSeedDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // подмена HOME: t.Parallel() нельзя

	require.NoError(t, cli.Execute([]string{"ls"}))

	cfg, err := config.Load(configPath(t))
	require.NoError(t, err)

	want := os.Getenv("USER")
	if current, uerr := user.Current(); uerr == nil && current.Username != "" {
		want = current.Username
	}

	assert.Equal(t, want, cfg.Defaults.User)
	assert.Equal(t, config.DefaultSSHPort, cfg.Defaults.Port)
}
