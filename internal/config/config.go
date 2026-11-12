// Package config holds the pr-review settings: model assignments per role,
// thresholds and paths, persisted as YAML.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Thresholds are the size limits that trigger the review warnings.
type Thresholds struct {
	MaxLines        int `yaml:"max_lineas"`
	MaxFiles        int `yaml:"max_archivos"`
	MaxMinorDetails int `yaml:"max_detalles_menores"`
}

// Config is the full pr-review configuration.
type Config struct {
	Workspace     string              `yaml:"workspace"`
	Preset        Preset              `yaml:"preset"`
	Models        map[Role]Assignment `yaml:"modelos"`
	Thresholds    Thresholds          `yaml:"umbrales"`
	TaskIDPattern string              `yaml:"patron_id_tarea"`
	CloneURL      string              `yaml:"url_clonado"`
	Concurrency   int                 `yaml:"concurrencia"`
	AgentTimeout  time.Duration       `yaml:"-"` // (un)marshaled as a duration string, see below
	ReportsDir    string              `yaml:"directorio_reportes"`
	CacheDir      string              `yaml:"directorio_cache"`
}

// Defaults returns the default configuration.
func Defaults() Config {
	return Config{
		Preset:        PresetBalanceado,
		Models:        PresetAssignments(PresetBalanceado),
		Thresholds:    Thresholds{MaxLines: 400, MaxFiles: 15, MaxMinorDetails: 5},
		TaskIDPattern: `^(CU-[0-9a-z]+|[A-Z][A-Z0-9]{1,9}-\d+)`,
		CloneURL:      "git@bitbucket.org:{workspace}/{repo}.git",
		Concurrency:   4,
		AgentTimeout:  10 * time.Minute,
	}
}

// Path returns the config file location under the user config dir.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pr-review", "config.yaml"), nil
}

// wire mirrors Config on disk, adding AgentTimeout as a readable string.
type plainConfig Config

// wire mirrors Config on disk, adding AgentTimeout as a readable string.
type wire struct {
	plainConfig  `yaml:",inline"`
	AgentTimeout string `yaml:"timeout_agente"`
}

// MarshalYAML writes AgentTimeout as a duration string such as "10m".
func (c Config) MarshalYAML() (any, error) {
	return wire{plainConfig(c), c.AgentTimeout.String()}, nil
}

// UnmarshalYAML reads AgentTimeout from a duration string. Fields absent in
// the document keep the receiver's current values.
func (c *Config) UnmarshalYAML(n *yaml.Node) error {
	w := wire{plainConfig: plainConfig(*c), AgentTimeout: c.AgentTimeout.String()}
	if err := n.Decode(&w); err != nil {
		return err
	}
	d, err := time.ParseDuration(w.AgentTimeout)
	if err != nil {
		return fmt.Errorf("timeout_agente inválido: %w", err)
	}
	*c = Config(w.plainConfig)
	c.AgentTimeout = d
	return nil
}

// Load reads the config at path. A missing file yields Defaults and found=false.
func Load(path string) (cfg Config, found bool, err error) {
	cfg = Defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return Defaults(), false, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Defaults(), true, fmt.Errorf("leyendo %s: %w", path, err)
	}
	return cfg, true, nil
}

// Save writes cfg atomically (temp file + rename) with mode 0600, creating
// the parent directory with 0700.
func Save(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// AssignmentFor returns the role's configured assignment, falling back to the
// preset's when it is missing or invalid.
func (c Config) AssignmentFor(r Role) Assignment {
	if a, ok := c.Models[r]; ok && a.Valid() {
		return a
	}
	return PresetAssignments(c.Preset)[r]
}
