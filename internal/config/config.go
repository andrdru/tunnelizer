package config

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	t, ok := c.Tunnels[alias]
	if !ok {
		return ResolvedTunnel{}, fmt.Errorf("config.Resolve %q: %w", alias, ErrTunnelNotFound)
	}

	rt := ResolvedTunnel{
		Alias:        alias,
		Host:         t.Host,
		User:         cmp.Or(t.User, c.Defaults.User),
		Port:         cmp.Or(t.Port, c.Defaults.Port),
		IdentityFile: expandHome(cmp.Or(t.IdentityFile, c.Defaults.IdentityFile)),
		LocalPort:    t.LocalPort,
		RemoteHost:   cmp.Or(t.RemoteHost, c.Defaults.RemoteHost),
		RemotePort:   cmp.Or(t.RemotePort, c.Defaults.RemotePort),
		Interactive:  c.Defaults.Interactive,
	}

	if t.Interactive != nil {
		rt.Interactive = *t.Interactive
	}

	return rt, nil
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
