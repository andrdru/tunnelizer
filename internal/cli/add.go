package cli

import (
	"flag"
	"fmt"

	"github.com/andrdru/tunnelizer/internal/config"
)

func runAdd(paths Paths, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	flags := registerTunnelFlags(fs, true)

	if _, err := parseArgs(fs, args); err != nil {
		return fmt.Errorf("cli.add: %w", err)
	}

	cfg, err := loadConfig(paths)
	if err != nil {
		return fmt.Errorf("cli.add: %w", err)
	}

	alias, t := flags.direct()

	if alias == "" || t.Host == "" || t.LocalPort == 0 {
		values, ferr := runTunnelForm(prefillDefaults(t, cfg.Defaults), cfg.Defaults, &alias)
		if ferr != nil {
			return fmt.Errorf("cli.add: %w", ferr)
		}

		alias = values.alias
		t = values.tunnel()
	}

	if aerr := cfg.Add(alias, t); aerr != nil {
		return fmt.Errorf("cli.add: %w", aerr)
	}

	rt, err := cfg.Resolve(alias)
	if err != nil {
		return fmt.Errorf("cli.add: %w", err)
	}

	if verr := rt.Validate(); verr != nil {
		return fmt.Errorf("cli.add: %w", verr)
	}

	if serr := config.Save(paths.Config, cfg); serr != nil {
		return fmt.Errorf("cli.add: %w", serr)
	}

	fmt.Printf("%s: added\n", alias)

	return nil
}
