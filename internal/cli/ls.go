package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/olekukonko/tablewriter"

	"github.com/andrdru/tunnelizer/internal/config"
	"github.com/andrdru/tunnelizer/internal/state"
)

const notAvailable = "-"

func runLs(paths Paths, args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli.ls: %w", err)
	}

	cfg, err := config.Load(paths.Config)
	if err != nil {
		return fmt.Errorf("cli.ls: %w", err)
	}

	store := state.NewStore(paths.StateDir)

	states, err := store.List()
	if err != nil {
		return fmt.Errorf("cli.ls: %w", err)
	}

	byAlias := make(map[string]state.State, len(states))
	for _, st := range states {
		byAlias[st.Alias] = st
	}

	ctx := context.Background()
	table := tablewriter.NewWriter(os.Stdout)
	table.Header("ALIAS", "LOCAL", "REMOTE", "HOST", "STATUS", "PORT", "UPTIME", "RESTARTS")

	for _, alias := range unionAliases(cfg, states) {
		if err := table.Append(lsRow(ctx, cfg, byAlias, alias)); err != nil {
			return fmt.Errorf("cli.ls: %w", err)
		}
	}

	if err := table.Render(); err != nil {
		return fmt.Errorf("cli.ls: %w", err)
	}

	return nil
}

func lsRow(ctx context.Context, cfg *config.Config, byAlias map[string]state.State, alias string) []string {
	port := notAvailable
	uptime := notAvailable
	restarts := notAvailable

	st, ok := byAlias[alias]
	if ok {
		restarts = strconv.Itoa(st.Restarts)

		if status := state.EffectiveStatus(st, state.PIDAlive); status == state.StatusUp || status == state.StatusReconnecting {
			uptime = time.Since(st.StartedAt).Round(time.Second).String()
		}
	}

	rt, err := cfg.Resolve(alias)
	if err != nil {
		return []string{alias, notAvailable, notAvailable, notAvailable, lsStatus(st, ok), port, uptime, restarts}
	}

	row := []string{
		alias,
		fmt.Sprintf("127.0.0.1:%d", rt.LocalPort),
		fmt.Sprintf("%s:%d", rt.RemoteHost, rt.RemotePort),
		rt.Host,
		lsStatus(st, ok),
		port,
		uptime,
		restarts,
	}

	if row[4] == string(state.StatusUp) {
		row[5] = portState(ctx, rt.LocalPort)
	}

	return row
}

func lsStatus(st state.State, ok bool) string {
	if !ok {
		return string(state.StatusDown)
	}

	return string(state.EffectiveStatus(st, state.PIDAlive))
}

func portState(ctx context.Context, localPort int) string {
	if state.Probe(ctx, localPort) {
		return "open"
	}

	return "closed"
}

func unionAliases(cfg *config.Config, states []state.State) []string {
	seen := make(map[string]struct{}, len(cfg.Tunnels)+len(states))
	aliases := make([]string, 0, len(cfg.Tunnels)+len(states))

	for _, alias := range cfg.Aliases() {
		seen[alias] = struct{}{}
		aliases = append(aliases, alias)
	}

	for _, st := range states {
		if _, ok := seen[st.Alias]; ok {
			continue
		}

		seen[st.Alias] = struct{}{}
		aliases = append(aliases, st.Alias)
	}

	return aliases
}
