package config

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	ErrTunnelNotFound = errors.New("tunnel not found")
	ErrExists         = errors.New("tunnel already exists")
	ErrInvalidConfig  = errors.New("invalid config")
)

const (
	MinPort = 1
	MaxPort = 65535

	DefaultSSHPort = 22

	LocalHost = "127.0.0.1"

	placeholderHost = "{host}"
	placeholderPort = "{port}"

	DefaultDSNTemplate = "jdbc:postgresql://" + placeholderHost + ":" + placeholderPort + "/postgres"

	filePerm = 0o600
	dirPerm  = 0o700
	homePref = "~/"
)

type Defaults struct {
	User         string `yaml:"user,omitempty"`
	Port         int    `yaml:"port,omitempty"`
	IdentityFile string `yaml:"identity_file,omitempty"`
	RemoteHost   string `yaml:"remote_host,omitempty"`
	RemotePort   int    `yaml:"remote_port,omitempty"`
	Interactive  bool   `yaml:"interactive,omitempty"`
}

type Tunnel struct {
	Host         string `yaml:"host"`
	User         string `yaml:"user,omitempty"`
	Port         int    `yaml:"port,omitempty"`
	IdentityFile string `yaml:"identity_file,omitempty"`
	LocalPort    int    `yaml:"local_port,omitempty"`
	RemoteHost   string `yaml:"remote_host,omitempty"`
	RemotePort   int    `yaml:"remote_port,omitempty"`
	DSN          string `yaml:"dsn,omitempty"`
	Interactive  *bool  `yaml:"interactive,omitempty"`
}

type ResolvedTunnel struct {
	Alias        string
	Host         string
	User         string
	Port         int
	IdentityFile string
	LocalPort    int
	RemoteHost   string
	RemotePort   int
	Interactive  bool
}

// DefaultsPatch — изменения defaults: nil-поле не трогается, указатель на пустое значение очищает поле.
type DefaultsPatch struct {
	User         *string
	Port         *int
	IdentityFile *string
	RemoteHost   *string
	RemotePort   *int
	Interactive  *bool
}

// TunnelPatch — изменения туннеля: nil-поле не трогается, указатель на пустое значение очищает поле.
type TunnelPatch struct {
	Host         *string
	User         *string
	Port         *int
	IdentityFile *string
	LocalPort    *int
	RemoteHost   *string
	RemotePort   *int
	DSN          *string
	Interactive  *bool
}

type Config struct {
	Defaults Defaults          `yaml:"defaults"`
	Tunnels  map[string]Tunnel `yaml:"tunnels"`
}

type ValidationError struct {
	Problems []string
}

func (e ValidationError) Error() string {
	return ErrInvalidConfig.Error() + ": " + strings.Join(e.Problems, "; ")
}

func (e ValidationError) Is(target error) bool {
	return target == ErrInvalidConfig
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the user-owned config file
	if err != nil {
		return nil, fmt.Errorf("config.Load: %w", err)
	}

	var cfg Config

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config.Load: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config.Load: %w", err)
	}

	return &cfg, nil
}

func LoadOrCreate(path string, seed Defaults) (*Config, error) {
	cfg, err := Load(path)
	if err == nil {
		return cfg, nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load: %w", err)
	}

	cfg = &Config{Defaults: seed}

	if err := Save(path, cfg); err != nil {
		return nil, fmt.Errorf("config.LoadOrCreate: %w", err)
	}

	return cfg, nil
}

func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config.Save: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("config.Save: %w", err)
	}

	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("config.Save: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("config.Save: %w", err)
	}

	return nil
}

func (c *Config) Validate() error {
	var problems []string

	for _, alias := range c.Aliases() {
		rt, err := c.Resolve(alias)
		if err != nil {
			problems = append(problems, err.Error())

			continue
		}

		problems = append(problems, rt.problems()...)
	}

	if len(problems) > 0 {
		return ValidationError{Problems: problems}
	}

	return nil
}

func (c *Config) Resolve(alias string) (ResolvedTunnel, error) {
	t, err := c.Effective(alias)
	if err != nil {
		return ResolvedTunnel{}, fmt.Errorf("config.Resolve: %w", err)
	}

	return ResolvedTunnel{
		Alias:        alias,
		Host:         t.Host,
		User:         t.User,
		Port:         t.Port,
		IdentityFile: expandHome(t.IdentityFile),
		LocalPort:    t.LocalPort,
		RemoteHost:   t.RemoteHost,
		RemotePort:   t.RemotePort,
		Interactive:  t.Interactive != nil && *t.Interactive,
	}, nil
}

// Effective отдаёт туннель после мерджа с defaults, не разворачивая "~/" в identity_file:
// значения годятся для переноса на другую машину и для предзаполнения формы.
func (c *Config) Effective(alias string) (Tunnel, error) {
	t, ok := c.Tunnels[alias]
	if !ok {
		return Tunnel{}, fmt.Errorf("config.Effective %q: %w", alias, ErrTunnelNotFound)
	}

	eff := Tunnel{
		Host:         t.Host,
		User:         cmp.Or(t.User, c.Defaults.User),
		Port:         cmp.Or(t.Port, c.Defaults.Port),
		IdentityFile: cmp.Or(t.IdentityFile, c.Defaults.IdentityFile),
		LocalPort:    t.LocalPort,
		RemoteHost:   cmp.Or(t.RemoteHost, c.Defaults.RemoteHost),
		RemotePort:   cmp.Or(t.RemotePort, c.Defaults.RemotePort),
		DSN:          t.DSN,
	}

	if effectiveInteractive(c.Defaults.Interactive, t.Interactive) {
		enabled := true
		eff.Interactive = &enabled
	}

	return eff, nil
}

func (c *Config) DSN(alias string) (string, error) {
	t, err := c.Effective(alias)
	if err != nil {
		return "", fmt.Errorf("config.DSN: %w", err)
	}

	template := strings.ReplaceAll(cmp.Or(t.DSN, DefaultDSNTemplate), placeholderHost, LocalHost)

	return strings.ReplaceAll(template, placeholderPort, strconv.Itoa(t.LocalPort)), nil
}

func (c *Config) Aliases() []string {
	aliases := make([]string, 0, len(c.Tunnels))
	for alias := range c.Tunnels {
		aliases = append(aliases, alias)
	}

	sort.Strings(aliases)

	return aliases
}

func (c *Config) Add(alias string, t Tunnel) error {
	if c.Tunnels == nil {
		c.Tunnels = make(map[string]Tunnel)
	}

	if _, ok := c.Tunnels[alias]; ok {
		return fmt.Errorf("config.Add %q: %w", alias, ErrExists)
	}

	c.Tunnels[alias] = t

	return nil
}

func (c *Config) Set(alias string, t Tunnel) {
	if c.Tunnels == nil {
		c.Tunnels = make(map[string]Tunnel)
	}

	c.Tunnels[alias] = t
}

func (d Defaults) With(p DefaultsPatch) Defaults {
	override(&d.User, p.User)
	override(&d.Port, p.Port)
	override(&d.IdentityFile, p.IdentityFile)
	override(&d.RemoteHost, p.RemoteHost)
	override(&d.RemotePort, p.RemotePort)

	if p.Interactive != nil {
		d.Interactive = *p.Interactive
	}

	return d
}

func (t Tunnel) With(p TunnelPatch) Tunnel {
	override(&t.Host, p.Host)
	override(&t.User, p.User)
	override(&t.Port, p.Port)
	override(&t.IdentityFile, p.IdentityFile)
	override(&t.LocalPort, p.LocalPort)
	override(&t.RemoteHost, p.RemoteHost)
	override(&t.RemotePort, p.RemotePort)
	override(&t.DSN, p.DSN)

	if p.Interactive != nil {
		value := *p.Interactive
		t.Interactive = &value
	}

	return t
}

func (t Tunnel) Diff(next Tunnel) TunnelPatch {
	var p TunnelPatch

	if t.Host != next.Host {
		p.Host = &next.Host
	}

	if t.User != next.User {
		p.User = &next.User
	}

	if t.Port != next.Port {
		p.Port = &next.Port
	}

	if t.IdentityFile != next.IdentityFile {
		p.IdentityFile = &next.IdentityFile
	}

	if t.LocalPort != next.LocalPort {
		p.LocalPort = &next.LocalPort
	}

	if t.RemoteHost != next.RemoteHost {
		p.RemoteHost = &next.RemoteHost
	}

	if t.RemotePort != next.RemotePort {
		p.RemotePort = &next.RemotePort
	}

	if t.DSN != next.DSN {
		p.DSN = &next.DSN
	}

	if next.Interactive != nil && !equalBoolPtr(t.Interactive, next.Interactive) {
		p.Interactive = next.Interactive
	}

	return p
}

func (rt ResolvedTunnel) Validate() error {
	problems := rt.problems()
	if len(problems) > 0 {
		return ValidationError{Problems: problems}
	}

	return nil
}

func (rt ResolvedTunnel) problems() []string {
	var problems []string

	if rt.Host == "" {
		problems = append(problems, rt.Alias+": host is required")
	}

	if rt.LocalPort < MinPort || rt.LocalPort > MaxPort {
		problems = append(problems, fmt.Sprintf("%s: local_port must be %d-%d", rt.Alias, MinPort, MaxPort))
	}

	if rt.RemoteHost == "" {
		problems = append(problems, rt.Alias+": remote_host is required")
	}

	if rt.RemotePort < MinPort || rt.RemotePort > MaxPort {
		problems = append(problems, fmt.Sprintf("%s: remote_port must be %d-%d", rt.Alias, MinPort, MaxPort))
	}

	if rt.Port > MaxPort || rt.Port < 0 {
		problems = append(problems, fmt.Sprintf("%s: port must be %d-%d", rt.Alias, MinPort, MaxPort))
	}

	return problems
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, homePref) {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	return filepath.Join(home, strings.TrimPrefix(path, homePref))
}

func effectiveInteractive(def bool, own *bool) bool {
	if own != nil {
		return *own
	}

	return def
}

func equalBoolPtr(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

func override[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}
