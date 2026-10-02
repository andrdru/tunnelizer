package cli

import (
	"flag"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/andrdru/tunnelizer/internal/config"
)

const (
	formatCMD  = "cmd"
	formatYAML = "yaml"
	formatDSN  = "dsn"
)

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./~-]+$`)

func runExport(paths Paths, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	format := fs.String("format", formatCMD, "output format: cmd, yaml or dsn")

	positional, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("cli.export: %w", err)
	}

	cfg, err := loadConfig(paths)
	if err != nil {
		return fmt.Errorf("cli.export: %w", err)
	}

	alias, err := singleAlias(cfg, positional)
	if err != nil {
		return fmt.Errorf("cli.export: %w", err)
	}

	var out string

	switch *format {
	case formatCMD:
		out, err = exportCMD(cfg, alias)
	case formatYAML:
		out, err = exportYAML(cfg, alias)
	case formatDSN:
		out, err = cfg.DSN(alias)
	default:
		return fmt.Errorf("cli.export %q: %w", *format, ErrUsage)
	}

	if err != nil {
		return fmt.Errorf("cli.export: %w", err)
	}

	fmt.Println(out)

	return nil
}

func exportCMD(cfg *config.Config, alias string) (string, error) {
	t, err := exportTunnel(cfg, alias)
	if err != nil {
		return "", fmt.Errorf("cli.exportCMD: %w", err)
	}

	parts := []string{
		"tunz", "add",
		"--alias", shellQuote(alias),
		"--host", shellQuote(t.Host),
		"--user", shellQuote(t.User),
		"--port", strconv.Itoa(t.Port),
		"--local-port", strconv.Itoa(t.LocalPort),
		"--remote-host", shellQuote(t.RemoteHost),
		"--remote-port", strconv.Itoa(t.RemotePort),
	}

	if t.IdentityFile != "" {
		parts = append(parts, "--identity-file", shellQuote(t.IdentityFile))
	}

	if t.DSN != "" {
		parts = append(parts, "--dsn", shellQuote(t.DSN))
	}

	parts = append(parts, "--interactive="+strconv.FormatBool(t.Interactive != nil && *t.Interactive))

	return strings.Join(parts, " "), nil
}

func exportYAML(cfg *config.Config, alias string) (string, error) {
	t, err := exportTunnel(cfg, alias)
	if err != nil {
		return "", fmt.Errorf("cli.exportYAML: %w", err)
	}

	data, err := yaml.Marshal(struct {
		Tunnels map[string]config.Tunnel `yaml:"tunnels"`
	}{Tunnels: map[string]config.Tunnel{alias: t}})
	if err != nil {
		return "", fmt.Errorf("cli.exportYAML: %w", err)
	}

	return strings.TrimRight(string(data), "\n"), nil
}

func exportTunnel(cfg *config.Config, alias string) (config.Tunnel, error) {
	rt, err := cfg.Resolve(alias)
	if err != nil {
		return config.Tunnel{}, fmt.Errorf("cli.exportTunnel: %w", err)
	}

	if verr := rt.Validate(); verr != nil {
		return config.Tunnel{}, fmt.Errorf("cli.exportTunnel: %w", verr)
	}

	t, err := cfg.Effective(alias)
	if err != nil {
		return config.Tunnel{}, fmt.Errorf("cli.exportTunnel: %w", err)
	}

	return t, nil
}

func shellQuote(value string) string {
	if shellSafe.MatchString(value) {
		return value
	}

	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
