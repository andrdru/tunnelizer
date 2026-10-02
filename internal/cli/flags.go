package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/andrdru/tunnelizer/internal/config"
)

const (
	flagAlias        = "alias"
	flagHost         = "host"
	flagUser         = "user"
	flagPort         = "port"
	flagLocalPort    = "local-port"
	flagRemoteHost   = "remote-host"
	flagRemotePort   = "remote-port"
	flagIdentityFile = "identity-file"
	flagDSN          = "dsn"
	flagInteractive  = "interactive"

	doubleDash = "--"
)

type tunnelFlags struct {
	fs           *flag.FlagSet
	alias        *string
	host         *string
	user         *string
	port         *int
	localPort    *int
	remoteHost   *string
	remotePort   *int
	identityFile *string
	dsn          *string
	interactive  *bool
}

func registerTunnelFlags(fs *flag.FlagSet, withAlias bool) *tunnelFlags {
	f := &tunnelFlags{
		fs:           fs,
		host:         fs.String(flagHost, "", "ssh host"),
		user:         fs.String(flagUser, "", "ssh user"),
		port:         fs.Int(flagPort, 0, "ssh port"),
		localPort:    fs.Int(flagLocalPort, 0, "local port"),
		remoteHost:   fs.String(flagRemoteHost, "", "remote host"),
		remotePort:   fs.Int(flagRemotePort, 0, "remote port"),
		identityFile: fs.String(flagIdentityFile, "", "path to identity file"),
		dsn:          fs.String(flagDSN, "", "JDBC DSN template with {host}/{port}"),
		interactive:  fs.Bool(flagInteractive, false, "interactive auth (OTP via terminal)"),
	}

	if withAlias {
		f.alias = fs.String(flagAlias, "", "tunnel alias")
	}

	return f
}

func (f *tunnelFlags) direct() (string, config.Tunnel) {
	t := config.Tunnel{
		Host:         *f.host,
		User:         *f.user,
		Port:         *f.port,
		IdentityFile: *f.identityFile,
		LocalPort:    *f.localPort,
		RemoteHost:   *f.remoteHost,
		RemotePort:   *f.remotePort,
		DSN:          *f.dsn,
	}

	if visitedFlags(f.fs)[flagInteractive] {
		interactive := *f.interactive
		t.Interactive = &interactive
	}

	return deref(f.alias), t
}

func (f *tunnelFlags) patch() config.TunnelPatch {
	set := visitedFlags(f.fs)

	return config.TunnelPatch{
		Host:         onlyIfSet(set[flagHost], f.host),
		User:         onlyIfSet(set[flagUser], f.user),
		Port:         onlyIfSet(set[flagPort], f.port),
		IdentityFile: onlyIfSet(set[flagIdentityFile], f.identityFile),
		LocalPort:    onlyIfSet(set[flagLocalPort], f.localPort),
		RemoteHost:   onlyIfSet(set[flagRemoteHost], f.remoteHost),
		RemotePort:   onlyIfSet(set[flagRemotePort], f.remotePort),
		DSN:          onlyIfSet(set[flagDSN], f.dsn),
		Interactive:  onlyIfSet(set[flagInteractive], f.interactive),
	}
}

func (f *tunnelFlags) anySet() bool {
	return len(visitedFlags(f.fs)) > 0
}

type defaultsFlags struct {
	fs           *flag.FlagSet
	user         *string
	port         *int
	identityFile *string
	remoteHost   *string
	remotePort   *int
	interactive  *bool
}

func registerDefaultsFlags(fs *flag.FlagSet) *defaultsFlags {
	return &defaultsFlags{
		fs:           fs,
		user:         fs.String(flagUser, "", "ssh user"),
		port:         fs.Int(flagPort, 0, "ssh port"),
		identityFile: fs.String(flagIdentityFile, "", "path to identity file"),
		remoteHost:   fs.String(flagRemoteHost, "", "remote host"),
		remotePort:   fs.Int(flagRemotePort, 0, "remote port"),
		interactive:  fs.Bool(flagInteractive, false, "interactive auth (OTP via terminal)"),
	}
}

func (f *defaultsFlags) patch() config.DefaultsPatch {
	set := visitedFlags(f.fs)

	return config.DefaultsPatch{
		User:         onlyIfSet(set[flagUser], f.user),
		Port:         onlyIfSet(set[flagPort], f.port),
		IdentityFile: onlyIfSet(set[flagIdentityFile], f.identityFile),
		RemoteHost:   onlyIfSet(set[flagRemoteHost], f.remoteHost),
		RemotePort:   onlyIfSet(set[flagRemotePort], f.remotePort),
		Interactive:  onlyIfSet(set[flagInteractive], f.interactive),
	}
}

func (f *defaultsFlags) anySet() bool {
	return len(visitedFlags(f.fs)) > 0
}

// parseArgs разбирает флаги, стоящие и до, и после позиционных аргументов: stdlib flag
// останавливается на первом не-флаге, а tunz пишет alias первым.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	flagArgs := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == doubleDash {
			positional = append(positional, args[i+1:]...)

			break
		}

		if arg == "-" || !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)

			continue
		}

		flagArgs = append(flagArgs, arg)

		if strings.Contains(arg, "=") || isBoolFlag(fs, arg) {
			continue
		}

		if i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}

	if err := fs.Parse(flagArgs); err != nil {
		return nil, fmt.Errorf("cli.parseArgs: %w", err)
	}

	return positional, nil
}

// isBoolFlag повторяет правило stdlib flag: bool-флаг не съедает следующий аргумент.
func isBoolFlag(fs *flag.FlagSet, arg string) bool {
	flg := fs.Lookup(strings.TrimLeft(arg, "-"))
	if flg == nil {
		return false
	}

	value, ok := flg.Value.(interface{ IsBoolFlag() bool })

	return ok && value.IsBoolFlag()
}

func visitedFlags(fs *flag.FlagSet) map[string]bool {
	set := make(map[string]bool)

	fs.Visit(func(fl *flag.Flag) {
		set[fl.Name] = true
	})

	return set
}

func onlyIfSet[T any](set bool, value *T) *T {
	if !set {
		return nil
	}

	return value
}

func deref(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}
