package cli

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"

	"github.com/andrdru/tunnelizer/internal/config"
)

const (
	titleRequired = "Required"
	titleOptional = "Optional"
	hintPrefix    = "default="
)

var (
	errValueRequired = errors.New("value is required")
	errInvalidPort   = errors.New("port must be 1-65535")
)

type tunnelFormValues struct {
	alias        string
	host         string
	user         string
	port         string
	localPort    string
	remoteHost   string
	remotePort   string
	identityFile string
	dsn          string // формой не спрашивается: приходит из флага --dsn или из конфига
	interactive  bool
}

func (v *tunnelFormValues) tunnel() config.Tunnel {
	interactive := v.interactive

	return config.Tunnel{
		Host:         v.host,
		User:         v.user,
		Port:         atoiOrZero(v.port),
		IdentityFile: v.identityFile,
		LocalPort:    atoiOrZero(v.localPort),
		RemoteHost:   v.remoteHost,
		RemotePort:   atoiOrZero(v.remotePort),
		DSN:          v.dsn,
		Interactive:  &interactive,
	}
}

func runTunnelForm(t config.Tunnel, def config.Defaults, alias *string) (*tunnelFormValues, error) {
	form, values := newTunnelForm(t, def, alias)

	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("cli.runTunnelForm: %w", err)
	}

	return values, nil
}

func newTunnelForm(t config.Tunnel, def config.Defaults, alias *string) (*huh.Form, *tunnelFormValues) {
	interactive := def.Interactive
	if t.Interactive != nil {
		interactive = *t.Interactive
	}

	values := &tunnelFormValues{
		host:         t.Host,
		user:         t.User,
		port:         portString(t.Port),
		localPort:    portString(t.LocalPort),
		remoteHost:   t.RemoteHost,
		remotePort:   portString(t.RemotePort),
		identityFile: t.IdentityFile,
		dsn:          t.DSN,
		interactive:  interactive,
	}

	fields := []huh.Field{huh.NewNote().Title(titleRequired)}

	if alias != nil {
		values.alias = *alias
		fields = append(fields, huh.NewInput().Title("Alias").Value(&values.alias).Validate(notEmpty))
	}

	remoteHost := huh.NewInput().Title("Remote host").Value(&values.remoteHost).
		Description(defaultHint(values.remoteHost, def.RemoteHost))

	if def.RemoteHost == "" {
		remoteHost.Validate(notEmpty)
	}

	fields = append(fields,
		huh.NewInput().Title("SSH host").Value(&values.host).Validate(notEmpty),
		huh.NewInput().Title("Local port").Value(&values.localPort).Validate(portValue(true)),
		huh.NewInput().Title("Remote port").Value(&values.remotePort).
			Validate(portValue(def.RemotePort == 0)).
			Description(defaultHint(values.remotePort, portString(def.RemotePort))),
		huh.NewNote().Title(titleOptional),
		remoteHost,
		huh.NewInput().Title("SSH user").Value(&values.user).
			Description(defaultHint(values.user, def.User)),
		huh.NewInput().Title("SSH port").Value(&values.port).
			Validate(portValue(false)).
			Description(defaultHint(values.port, portString(def.Port))),
		huh.NewInput().Title("Identity file").Value(&values.identityFile).
			Description(defaultHint(values.identityFile, def.IdentityFile)),
		huh.NewConfirm().Title("Interactive auth (OTP via terminal)?").Value(&values.interactive).
			Description(hintPrefix+strconv.FormatBool(def.Interactive)),
	)

	form := huh.NewForm(huh.NewGroup(fields...)).WithKeyMap(formKeyMap())

	return form, values
}

type defaultsFormValues struct {
	user         string
	port         string
	identityFile string
	remoteHost   string
	remotePort   string
	interactive  bool
}

func (v *defaultsFormValues) defaults() config.Defaults {
	return config.Defaults{
		User:         v.user,
		Port:         atoiOrZero(v.port),
		IdentityFile: v.identityFile,
		RemoteHost:   v.remoteHost,
		RemotePort:   atoiOrZero(v.remotePort),
		Interactive:  v.interactive,
	}
}

func runDefaultsForm(def config.Defaults) (*defaultsFormValues, error) {
	values := &defaultsFormValues{
		user:         cmp.Or(def.User, currentUsername()),
		port:         portString(cmp.Or(def.Port, config.DefaultSSHPort)),
		identityFile: def.IdentityFile,
		remoteHost:   def.RemoteHost,
		remotePort:   portString(def.RemotePort),
		interactive:  def.Interactive,
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("SSH user").Value(&values.user),
			huh.NewInput().Title("SSH port").Value(&values.port).Validate(portValue(false)),
			huh.NewInput().Title("Identity file").Value(&values.identityFile),
			huh.NewInput().Title("Remote host").Value(&values.remoteHost),
			huh.NewInput().Title("Remote port").Value(&values.remotePort).Validate(portValue(false)),
			huh.NewConfirm().Title("Interactive auth (OTP via terminal)?").Value(&values.interactive),
		),
	).WithKeyMap(formKeyMap())

	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("cli.runDefaultsForm: %w", err)
	}

	return values, nil
}

// formKeyMap добавляет стрелки ↑/↓ к штатным enter / shift+tab.
func formKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Input.Next = appendKeys(km.Input.Next, "down")
	km.Input.Prev = appendKeys(km.Input.Prev, "up")
	km.Confirm.Next = appendKeys(km.Confirm.Next, "down")
	km.Confirm.Prev = appendKeys(km.Confirm.Prev, "up")

	return km
}

func appendKeys(b key.Binding, keys ...string) key.Binding {
	return key.NewBinding(
		key.WithKeys(append(b.Keys(), keys...)...),
		key.WithHelp(b.Help().Key, b.Help().Desc),
	)
}

// prefillDefaults подставляет значения defaults в пустые поля: add пишет туннель в конфиг целиком.
func prefillDefaults(t config.Tunnel, def config.Defaults) config.Tunnel {
	t.User = cmp.Or(t.User, def.User)
	t.Port = cmp.Or(t.Port, def.Port)
	t.IdentityFile = cmp.Or(t.IdentityFile, def.IdentityFile)
	t.RemoteHost = cmp.Or(t.RemoteHost, def.RemoteHost)
	t.RemotePort = cmp.Or(t.RemotePort, def.RemotePort)

	if t.Interactive == nil {
		interactive := def.Interactive
		t.Interactive = &interactive
	}

	return t
}

// defaultHint — подсказка под пустым полем: пустое значение наследует default.
func defaultHint(raw, def string) string {
	if raw != "" || def == "" {
		return ""
	}

	return hintPrefix + def
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
