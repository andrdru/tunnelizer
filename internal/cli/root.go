package cli

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/andrdru/tunnelizer/internal/config"
)

var (
	ErrUnknownCommand = errors.New("unknown command")
	ErrUsage          = errors.New("invalid usage")
	ErrNoAlias        = errors.New("specify tunnel alias or --all")
	ErrPartialFailure = errors.New("some tunnels failed")
)

type Paths struct {
	Config   string
	StateDir string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("cli.DefaultPaths: %w", err)
	}

	return Paths{
		Config:   filepath.Join(home, ".config", "tunnelizer", "config.yaml"),
		StateDir: filepath.Join(home, ".local", "state", "tunnelizer"),
	}, nil
}

func Execute(args []string) error {
	paths, err := DefaultPaths()
	if err != nil {
		return err
	}

	commands := map[string]func(Paths, []string) error{
		"add":      runAdd,
		"edit":     runEdit,
		"defaults": runDefaults,
		"export":   runExport,
		"up":       runUp,
		"down":     runDown,
		"ls":       runLs,
		"run":      runRunner,
		"help":     func(Paths, []string) error { printUsage(); return nil },
	}

	if len(args) == 0 {
		printUsage()

		return nil
	}

	cmd, ok := commands[args[0]]
	if !ok {
		printUsage()

		return fmt.Errorf("cli.Execute %q: %w", args[0], ErrUnknownCommand)
	}

	return cmd(paths, args[1:])
}

func printUsage() {
	fmt.Print(`tunz — SSH tunnel manager

Usage:
  tunz add [--alias A --host H --local-port N ...]   add tunnel to config (wizard if flags missing)
  tunz edit [<alias>] [--local-port N ...]           edit tunnel (picker and wizard if called bare)
  tunz defaults [--port N ...]                       edit defaults section (wizard if no flags)
  tunz export [<alias>] [--format cmd|yaml|dsn]      print tunnel as a command line, yaml block or JDBC DSN
  tunz up [<alias> ...] [--all]                      bring tunnel(s) up (picker if alias omitted)
  tunz down [<alias> ...] [--all]                    bring tunnel(s) down (picker if alias omitted)
  tunz ls                                            show tunnels status and JDBC DSN
  tunz help                                          show this help
`)
}

func loadConfig(paths Paths) (*config.Config, error) {
	seed := config.Defaults{User: currentUsername(), Port: config.DefaultSSHPort}

	cfg, err := config.LoadOrCreate(paths.Config, seed)
	if err != nil {
		return nil, fmt.Errorf("cli.loadConfig: %w", err)
	}

	return cfg, nil
}

func currentUsername() string {
	current, err := user.Current()
	if err == nil && current.Username != "" {
		return current.Username
	}

	return os.Getenv("USER")
}

func pickAliases(all bool, args []string, configured []string) ([]string, error) {
	if all {
		return configured, nil
	}

	if len(args) > 0 {
		return args, nil
	}

	alias, err := pickAlias(configured)
	if err != nil {
		return nil, fmt.Errorf("cli.pickAliases: %w", err)
	}

	return []string{alias}, nil
}

// singleAlias отдаёт алиас из аргументов, а при их отсутствии спрашивает в терминале.
// Вне терминала отвечает ErrUsage — так edit и export вели себя до появления пикера.
func singleAlias(cfg *config.Config, positional []string) (string, error) {
	if len(positional) == 1 {
		return positional[0], nil
	}

	if len(positional) > 1 {
		return "", fmt.Errorf("cli.singleAlias: %w", ErrUsage)
	}

	alias, err := pickAlias(cfg.Aliases())
	if errors.Is(err, ErrNoAlias) {
		return "", fmt.Errorf("cli.singleAlias: %w", ErrUsage)
	}

	if err != nil {
		return "", fmt.Errorf("cli.singleAlias: %w", err)
	}

	return alias, nil
}
