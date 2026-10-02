package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		"add":  runAdd,
		"up":   runUp,
		"down": runDown,
		"ls":   runLs,
		"run":  runRunner,
		"help": func(Paths, []string) error { printUsage(); return nil },
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
  tunz up <alias> [--all]                            bring tunnel(s) up
  tunz down <alias> [--all]                          bring tunnel(s) down
  tunz ls                                            show tunnels status
  tunz help                                          show this help
`)
}

func pickAliases(all bool, args []string, configured []string) ([]string, error) {
	if all {
		return configured, nil
	}

	if len(args) == 0 {
		return nil, fmt.Errorf("cli.pickAliases: %w", ErrNoAlias)
	}

	return args, nil
}
