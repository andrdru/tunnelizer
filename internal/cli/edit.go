package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/andrdru/tunnelizer/internal/config"
	"github.com/andrdru/tunnelizer/internal/state"
)

func runEdit(paths Paths, args []string) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	flags := registerTunnelFlags(fs, false)

	positional, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("cli.edit: %w", err)
	}

	cfg, err := loadConfig(paths)
	if err != nil {
		return fmt.Errorf("cli.edit: %w", err)
	}

	alias, err := singleAlias(cfg, positional)
	if err != nil {
		return fmt.Errorf("cli.edit: %w", err)
	}

	raw, ok := cfg.Tunnels[alias]
	if !ok {
		return fmt.Errorf("cli.edit %q: %w", alias, config.ErrTunnelNotFound)
	}

	oldRT, err := cfg.Resolve(alias)
	if err != nil {
		return fmt.Errorf("cli.edit: %w", err)
	}

	patch := flags.patch()

	if !flags.anySet() {
		values, ferr := runTunnelForm(raw, cfg.Defaults, nil)
		if ferr != nil {
			return fmt.Errorf("cli.edit: %w", ferr)
		}

		patch = formPatch(cfg.Defaults, raw, values)
	}

	cfg.Set(alias, raw.With(patch))

	if verr := cfg.Validate(); verr != nil {
		return fmt.Errorf("cli.edit: %w", verr)
	}

	if serr := config.Save(paths.Config, cfg); serr != nil {
		return fmt.Errorf("cli.edit: %w", serr)
	}

	fmt.Printf("%s: updated\n", alias)

	ctx := context.Background()
	store := state.NewStore(paths.StateDir)

	if err := restartTunnel(ctx, cfg, store, alias, oldRT); err != nil {
		return fmt.Errorf("cli.edit: %w", err)
	}

	return nil
}

// formPatch отдаёт патч только по изменённым полям, чтобы наследуемые значения не материализовались в конфиге.
func formPatch(def config.Defaults, raw config.Tunnel, values *tunnelFormValues) config.TunnelPatch {
	candidate := values.tunnel()

	if raw.Interactive == nil && values.interactive == def.Interactive {
		candidate.Interactive = raw.Interactive
	}

	return raw.Diff(candidate)
}

func restartTunnel(ctx context.Context, cfg *config.Config, store *state.Store, alias string, oldRT config.ResolvedTunnel) error {
	newRT, err := cfg.Resolve(alias)
	if err != nil {
		return fmt.Errorf("cli.restartTunnel: %w", err)
	}

	if _, running := runnerPID(store, alias); !running {
		fmt.Printf("%s: not started; run tunz up %s\n", alias, alias)

		return nil
	}

	if newRT == oldRT {
		fmt.Printf("%s: running, settings unchanged\n", alias)

		return nil
	}

	fmt.Fprintf(os.Stderr, "%s: restarting (connection settings changed)\n", alias)

	if derr := downOne(ctx, cfg, store, alias); derr != nil {
		return fmt.Errorf("cli.restartTunnel: %w", derr)
	}

	if uerr := upOne(ctx, store, newRT); uerr != nil {
		return fmt.Errorf("cli.restartTunnel: %w", uerr)
	}

	if werr := waitUp(ctx, store, newRT); werr != nil {
		return fmt.Errorf("cli.restartTunnel: %w; runner keeps reconnecting, current status: tunz ls", werr)
	}

	return nil
}
