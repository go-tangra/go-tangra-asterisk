package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config carries the asterisk-specific data settings parsed from
// configs/data.yaml under the `data.asterisk` block.
type Config struct {
	CdrDSN          string        `yaml:"cdr_dsn"`
	ConfigDSN       string        `yaml:"config_dsn"`
	// TangraDSN points at a database this module OWNS (separate from
	// FreePBX's asteriskcdrdb / asterisk schemas). Used for the PJSIP
	// registration event log we capture from AMI. Optional — when blank,
	// AMI capture is disabled.
	TangraDSN       string        `yaml:"tangra_dsn"`
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
	QueryTimeout    time.Duration `yaml:"query_timeout"`

	AMI AMIConfig `yaml:"ami"`

	// PrometheusURL points at a Prometheus server scraping the
	// freepbx-exporter sidecar. When empty the dashboard endpoints return
	// PROMETHEUS_DISABLED. Override with ASTERISK_PROMETHEUS_URL.
	PrometheusURL string `yaml:"prometheus_url"`
}

// AMIConfig holds the Asterisk Manager Interface listener settings. When
// Host is empty the listener is not started.
type AMIConfig struct {
	Host           string        `yaml:"host"`
	Port           int           `yaml:"port"`
	Username       string        `yaml:"username"`
	Secret         string        `yaml:"secret"`
	ReconnectDelay time.Duration `yaml:"reconnect_delay"`
}

type rawDataYAML struct {
	Data struct {
		Asterisk Config `yaml:"asterisk"`
	} `yaml:"data"`
}

// LoadConfig reads configs/data.yaml relative to the bootstrap config dir
// and pulls the data.asterisk subtree. Env vars ASTERISK_CDR_DSN and
// ASTERISK_CONFIG_DSN override the file values.
func LoadConfig() (*Config, error) {
	dir := configDir()
	path := filepath.Join(dir, "data.yaml")

	cfg := &Config{
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 30 * time.Minute,
		QueryTimeout:    30 * time.Second,
		AMI: AMIConfig{
			Port:           5038,
			ReconnectDelay: 5 * time.Second,
		},
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	} else {
		var parsed rawDataYAML
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		applyAsteriskDefaults(cfg, &parsed.Data.Asterisk)
	}

	if v := os.Getenv("ASTERISK_CDR_DSN"); v != "" {
		cfg.CdrDSN = v
	}
	if v := os.Getenv("ASTERISK_CONFIG_DSN"); v != "" {
		cfg.ConfigDSN = v
	}
	if v := os.Getenv("ASTERISK_TANGRA_DSN"); v != "" {
		cfg.TangraDSN = v
	}
	if v := strings.TrimSpace(os.Getenv("ASTERISK_AMI_HOST")); v != "" {
		cfg.AMI.Host = v
	}
	if v := strings.TrimSpace(os.Getenv("ASTERISK_AMI_PORT")); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.AMI.Port = p
		}
	}
	if v := strings.TrimSpace(os.Getenv("ASTERISK_AMI_USERNAME")); v != "" {
		cfg.AMI.Username = v
	}
	if v := strings.TrimSpace(os.Getenv("ASTERISK_AMI_SECRET")); v != "" {
		cfg.AMI.Secret = v
	}
	// Belt-and-braces: trim YAML-loaded values too. Whitespace in the
	// secret is the #1 cause of "login: EOF" — Asterisk silently drops
	// the socket instead of replying with an authentication error.
	cfg.AMI.Username = strings.TrimSpace(cfg.AMI.Username)
	cfg.AMI.Secret = strings.TrimSpace(cfg.AMI.Secret)
	cfg.AMI.Host = strings.TrimSpace(cfg.AMI.Host)

	if v := strings.TrimSpace(os.Getenv("ASTERISK_PROMETHEUS_URL")); v != "" {
		cfg.PrometheusURL = v
	}
	cfg.PrometheusURL = strings.TrimRight(strings.TrimSpace(cfg.PrometheusURL), "/")

	if cfg.CdrDSN == "" {
		return nil, fmt.Errorf("asterisk: cdr_dsn is required (configs/data.yaml or ASTERISK_CDR_DSN)")
	}
	if cfg.ConfigDSN == "" {
		return nil, fmt.Errorf("asterisk: config_dsn is required (configs/data.yaml or ASTERISK_CONFIG_DSN)")
	}

	return cfg, nil
}

func applyAsteriskDefaults(dst, src *Config) {
	if src.CdrDSN != "" {
		dst.CdrDSN = src.CdrDSN
	}
	if src.ConfigDSN != "" {
		dst.ConfigDSN = src.ConfigDSN
	}
	if src.MaxOpenConns > 0 {
		dst.MaxOpenConns = src.MaxOpenConns
	}
	if src.MaxIdleConns > 0 {
		dst.MaxIdleConns = src.MaxIdleConns
	}
	if src.ConnMaxLifetime > 0 {
		dst.ConnMaxLifetime = src.ConnMaxLifetime
	}
	if src.QueryTimeout > 0 {
		dst.QueryTimeout = src.QueryTimeout
	}
}

// configDir resolves where configs/*.yaml live: prefer the -c flag the
// bootstrap framework uses, falling back to ./configs.
func configDir() string {
	for i, a := range os.Args {
		if a == "-c" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "-c="); ok {
			return v
		}
		if v, ok := strings.CutPrefix(a, "--conf="); ok {
			return v
		}
	}
	if v := os.Getenv("CONFIG_DIR"); v != "" {
		return v
	}
	return "./configs"
}
