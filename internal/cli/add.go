package cli

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/andrdru/tunnelizer/internal/config"
)

var (
	errValueRequired = errors.New("value is required")
	errInvalidPort   = errors.New("port must be 1-65535")
)

func runAdd(paths Paths, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	alias := fs.String("alias", "", "tunnel alias")
	host := fs.String("host", "", "ssh host")
	user := fs.String("user", "", "ssh user")
	port := fs.Int("port", 0, "ssh port")
	localPort := fs.Int("local-port", 0, "local port")
	remoteHost := fs.String("remote-host", "", "remote host")
	remotePort := fs.Int("remote-port", 0, "remote port")
	identityFile := fs.String("identity-file", "", "path to identity file")
	interactive := fs.Bool("interactive", false, "interactive auth (OTP via terminal)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli.add: %w", err)
	}

	cfg, err := config.LoadOrCreate(paths.Config)
	if err != nil {
		return fmt.Errorf("cli.add: %w", err)
	}

	aliasValue := *alias

	t := config.Tunnel{
		Host:         *host,
		User:         *user,
		Port:         *port,
		LocalPort:    *localPort,
		RemoteHost:   *remoteHost,
		RemotePort:   *remotePort,
		IdentityFile: *identityFile,
	}

	if *interactive {
		enabled := true
		t.Interactive = &enabled
	}

	if aliasValue == "" || t.Host == "" || t.LocalPort == 0 {
		if werr := fillWizard(cfg.Defaults, &aliasValue, &t); werr != nil {
			return fmt.Errorf("cli.add: %w", werr)
		}
	}

	if aerr := cfg.Add(aliasValue, t); aerr != nil {
		return fmt.Errorf("cli.add: %w", aerr)
	}

	rt, err := cfg.Resolve(aliasValue)
	if err != nil {
		return fmt.Errorf("cli.add: %w", err)
	}

	if verr := rt.Validate(); verr != nil {
		return fmt.Errorf("cli.add: %w", verr)
	}

	if serr := config.Save(paths.Config, cfg); serr != nil {
		return fmt.Errorf("cli.add: %w", serr)
	}

	fmt.Printf("%s: added\n", aliasValue)

	return nil
}

func fillWizard(def config.Defaults, alias *string, t *config.Tunnel) error {
	user := cmp.Or(t.User, def.User)
	remoteHost := cmp.Or(t.RemoteHost, def.RemoteHost)
	identityFile := cmp.Or(t.IdentityFile, def.IdentityFile)

	port := portString(cmp.Or(t.Port, def.Port))
	localPort := portString(t.LocalPort)
	remotePort := portString(cmp.Or(t.RemotePort, def.RemotePort))

	interactive := def.Interactive
	if t.Interactive != nil {
		interactive = *t.Interactive
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Alias").Value(alias).Validate(notEmpty),
			huh.NewInput().Title("SSH host").Value(&t.Host).Validate(notEmpty),
			huh.NewInput().Title("SSH user").Value(&user),
			huh.NewInput().Title("SSH port").Value(&port).Validate(portValue(false)),
			huh.NewInput().Title("Local port").Value(&localPort).Validate(portValue(true)),
			huh.NewInput().Title("Remote host").Value(&remoteHost),
			huh.NewInput().Title("Remote port").Value(&remotePort).Validate(portValue(false)),
			huh.NewInput().Title("Identity file").Value(&identityFile),
			huh.NewConfirm().Title("Interactive auth (OTP via terminal)?").Value(&interactive),
		),
	)

	if err := form.Run(); err != nil {
		return fmt.Errorf("cli.fillWizard: %w", err)
	}

	t.User = user
	t.Port = atoiOrZero(port)
	t.LocalPort = atoiOrZero(localPort)
	t.RemoteHost = remoteHost
	t.RemotePort = atoiOrZero(remotePort)
	t.IdentityFile = identityFile
	t.Interactive = &interactive

	return nil
}

func notEmpty(value string) error {
	if strings.TrimSpace(value) == "" {
		return errValueRequired
	}

	return nil
}

func portValue(required bool) func(string) error {
	return func(value string) error {
		value = strings.TrimSpace(value)
		if value == "" {
			if required {
				return errValueRequired
			}

			return nil
		}

		number, err := strconv.Atoi(value)
		if err != nil || number < config.MinPort || number > config.MaxPort {
			return errInvalidPort
		}

		return nil
	}
}

func portString(port int) string {
	if port == 0 {
		return ""
	}

	return strconv.Itoa(port)
}

func atoiOrZero(value string) int {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}

	return number
}
