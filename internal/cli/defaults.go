package cli

import (
	"flag"
	"fmt"

	"github.com/andrdru/tunnelizer/internal/config"
)

func runDefaults(paths Paths, args []string) error {
	fs := flag.NewFlagSet("defaults", flag.ContinueOnError)
	flags := registerDefaultsFlags(fs)

	positional, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("cli.defaults: %w", err)
	}

	if len(positional) != 0 {
		return fmt.Errorf("cli.defaults: %w", ErrUsage)
	}

	cfg, err := loadConfig(paths)
	if err != nil {
		return fmt.Errorf("cli.defaults: %w", err)
	}

	if flags.anySet() {
		cfg.Defaults = cfg.Defaults.With(flags.patch())
	} else {
		values, ferr := runDefaultsForm(cfg.Defaults)
		if ferr != nil {
			return fmt.Errorf("cli.defaults: %w", ferr)
		}

		cfg.Defaults = values.defaults()
	}

	if verr := cfg.Validate(); verr != nil {
		return fmt.Errorf("cli.defaults: %w", verr)
	}

	if serr := config.Save(paths.Config, cfg); serr != nil {
		return fmt.Errorf("cli.defaults: %w", serr)
	}

	fmt.Println("defaults: updated")

	return nil
}
