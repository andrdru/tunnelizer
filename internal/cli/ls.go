package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/andrdru/tunnelizer/internal/config"
	"github.com/andrdru/tunnelizer/internal/state"
)

const (
	notAvailable = "-"

	cellBorder  = "│"
	cellSpace   = " "
	cellPadding = 1
	borderFill  = "─"

	centerSplit = 2
)

type tableRow struct {
	cells []string
	dsn   string
}

func runLs(paths Paths, args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)

	if _, err := parseArgs(fs, args); err != nil {
		return fmt.Errorf("cli.ls: %w", err)
	}

	cfg, err := loadConfig(paths)
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

	aliases := unionAliases(cfg, states)
	rows := make([]tableRow, 0, len(aliases)+1)
	rows = append(rows, tableRow{cells: []string{"ALIAS", "LOCAL", "REMOTE", "HOST", "STATUS", "PORT", "UPTIME", "RESTARTS"}})

	for _, alias := range aliases {
		rows = append(rows, lsRow(ctx, cfg, byAlias, alias))
	}

	if err := renderRows(os.Stdout, rows); err != nil {
		return fmt.Errorf("cli.ls: %w", err)
	}

	return nil
}

func lsRow(ctx context.Context, cfg *config.Config, byAlias map[string]state.State, alias string) tableRow {
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

	status := lsStatus(st, ok)

	rt, err := cfg.Resolve(alias)
	if err != nil {
		return tableRow{cells: []string{alias, notAvailable, notAvailable, notAvailable, status, port, uptime, restarts}}
	}

	if status == string(state.StatusUp) {
		port = portState(ctx, rt.LocalPort)
	}

	row := tableRow{
		cells: []string{
			alias,
			fmt.Sprintf("%s:%d", config.LocalHost, rt.LocalPort),
			fmt.Sprintf("%s:%d", rt.RemoteHost, rt.RemotePort),
			rt.Host,
			status,
			port,
			uptime,
			restarts,
		},
	}

	if dsn, derr := cfg.DSN(alias); derr == nil {
		row.dsn = dsn
	}

	return row
}

// renderRows печатает таблицу в рамке: ширины колонок — максимум по строкам, заголовок по центру.
// Строка DSN идёт после строки туннеля во всю ширину таблицы, чтобы URL копировался одной строкой.
func renderRows(w io.Writer, rows []tableRow) error {
	widths := columnWidths(rows)

	for _, line := range tableLines(rows, widths) {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return fmt.Errorf("cli.renderRows: %w", err)
		}
	}

	return nil
}

func columnWidths(rows []tableRow) []int {
	widths := make([]int, len(rows[0].cells))

	for _, row := range rows {
		for i, cell := range row.cells {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}

	inner := innerWidth(rows[0].cells, widths)
	deficit := 0

	for _, row := range rows {
		if row.dsn == "" {
			continue
		}

		deficit = max(deficit, utf8.RuneCountInString(cellSpace+row.dsn+cellSpace)-inner)
	}

	widths[len(widths)-1] += deficit

	return widths
}

func innerWidth(cells []string, widths []int) int {
	return utf8.RuneCountInString(formatCells(cells, widths)) - 2*utf8.RuneCountInString(cellBorder)
}

func tableLines(rows []tableRow, widths []int) []string {
	lines := []string{
		borderLine(widths, "┌", "┬", "┐"),
		formatHeader(rows[0].cells, widths),
		borderLine(widths, "├", "┼", "┤"),
	}

	for i, row := range rows[1:] {
		lines = append(lines, formatCells(row.cells, widths))

		if row.dsn != "" {
			lines = append(lines, formatDSNRow(row.dsn, innerWidth(rows[0].cells, widths)))
		}

		if i == len(rows)-2 {
			lines = append(lines, borderLine(widths, "└", "┴", "┘"))

			continue
		}

		lines = append(lines, borderLine(widths, "├", "┼", "┤"))
	}

	return lines
}

func borderLine(widths []int, left, mid, right string) string {
	segments := make([]string, len(widths))

	for i, width := range widths {
		segments[i] = strings.Repeat(borderFill, width+2*cellPadding)
	}

	return left + strings.Join(segments, mid) + right
}

func formatHeader(cells []string, widths []int) string {
	centered := make([]string, len(cells))

	for i, cell := range cells {
		length := utf8.RuneCountInString(cell)
		left := (widths[i] - length) / centerSplit
		centered[i] = strings.Repeat(cellSpace, left) + cell + strings.Repeat(cellSpace, widths[i]-length-left)
	}

	return formatCells(centered, widths)
}

func formatCells(cells []string, widths []int) string {
	parts := make([]string, len(cells))

	for i, cell := range cells {
		parts[i] = cellSpace + pad(cell, widths[i]) + cellSpace
	}

	return cellBorder + strings.Join(parts, cellBorder) + cellBorder
}

func formatDSNRow(dsn string, inner int) string {
	padding := inner - utf8.RuneCountInString(dsn) - cellPadding

	return cellBorder + cellSpace + dsn + strings.Repeat(cellSpace, padding) + cellBorder
}

func pad(cell string, width int) string {
	return cell + strings.Repeat(cellSpace, width-utf8.RuneCountInString(cell))
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
