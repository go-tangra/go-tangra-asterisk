package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	fconfig "github.com/go-tangra/go-tangra/v4/config"
	"gopkg.in/yaml.v3"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	fconfig.Config `yaml:",inline"`
	Binding        Binding `yaml:"binding"`
	AMI            AMI     `yaml:"ami"`
	Gateway        Gateway `yaml:"gateway"`
	// MeshEnroll obtains the module's mesh SPIFFE SVID by enrolling with lcm
	// over the network (identity.provider: provided), as every go-tangra v4
	// module does; a SPIRE workload socket (identity.provider: spiffe) remains
	// possible where SPIRE runs.
	MeshEnroll          MeshEnroll `yaml:"mesh_enroll"`
	QueryTimeoutSeconds int        `yaml:"query_timeout_seconds"`
	StreamSeconds       int        `yaml:"stream_seconds"`
}
type Binding struct {
	TenantID          string `yaml:"tenant_id"`
	PBXID             string `yaml:"pbx_id"`
	CDRDSN            string `yaml:"cdr_dsn" json:"-"`
	ConfigDSN         string `yaml:"config_dsn" json:"-"`
	RegistrationDSN   string `yaml:"registration_dsn" json:"-"`
	AdoptRegistration bool   `yaml:"adopt_registration"`
	// RegistrationRetentionDays prunes registration events older than this
	// many days (0 keeps them forever).
	RegistrationRetentionDays int    `yaml:"registration_retention_days"`
	RecordingRoot             string `yaml:"recording_root"`
	Timezone                  string `yaml:"timezone"`
	SourceTimezone            string `yaml:"source_timezone"`
	MonitoringURL             string `yaml:"monitoring_url" json:"-"`
	MonitoringDedicated       bool   `yaml:"monitoring_dedicated"`
}

// MeshEnroll configures lcm network enrollment for the module's own SVID.
type MeshEnroll struct {
	Enabled       bool   `yaml:"enabled"`
	EnrollURL     string `yaml:"enroll_url"`
	LCMGRPCTarget string `yaml:"lcm_grpc"`
	TenantID      string `yaml:"tenant_id"`
	TokenFile     string `yaml:"token_file" json:"-"`
	StateFile     string `yaml:"state_file"`
	Insecure      bool   `yaml:"insecure"`
}
type AMI struct {
	Address  string `yaml:"address"`
	Username string `yaml:"username" json:"-"`
	Secret   string `yaml:"secret" json:"-"`
	TLS      bool   `yaml:"tls"`
	Enabled  bool   `yaml:"enabled"`
}
type Gateway struct {
	Service string `yaml:"service"`
	Issuer  string `yaml:"issuer"`
}

func Default() Config {
	f := fconfig.Default()
	f.ServiceName = "asterisk"
	f.Server.HTTPAddr = ":8444"
	f.Limits.RequestTimeout = 300 * time.Second
	return Config{Config: f, Binding: Binding{Timezone: "Europe/Sofia", SourceTimezone: "Europe/Sofia", RegistrationRetentionDays: 400}, Gateway: Gateway{Service: "gateway"}, QueryTimeoutSeconds: 5, StreamSeconds: 240}
}
func Load(path string) (Config, error) {
	c := Default()
	raw, e := os.ReadFile(path)
	if e != nil {
		return c, errors.New("configuration file unavailable")
	}
	raw = []byte(os.ExpandEnv(string(raw)))
	d := yaml.NewDecoder(bytes.NewReader(raw))
	d.KnownFields(true)
	if d.Decode(&c) != nil {
		return c, errors.New("invalid configuration document")
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if e := c.Config.Validate(); e != nil {
		return e
	}
	if m := c.MeshEnroll; m.Enabled {
		if m.EnrollURL == "" || m.LCMGRPCTarget == "" || m.TenantID == "" || m.TokenFile == "" {
			return errors.New("mesh_enroll requires enroll_url, lcm_grpc, tenant_id and token_file")
		}
		if c.Config.Identity.Provider != "provided" {
			return errors.New("mesh_enroll requires identity.provider: provided")
		}
	}
	if c.Binding.TenantID == "" || c.Binding.PBXID == "" {
		return errors.New("exclusive tenant_id and pbx_id required")
	}
	if c.Binding.CDRDSN == "" {
		return errors.New("binding.cdr_dsn required")
	}
	for _, dsn := range []string{c.Binding.CDRDSN, c.Binding.ConfigDSN, c.Binding.RegistrationDSN} {
		if dsn != "" {
			if _, e := mysql.ParseDSN(dsn); e != nil {
				return errors.New("invalid database DSN")
			}
		}
	}
	if c.Binding.RegistrationDSN != "" {
		owned, _ := mysql.ParseDSN(c.Binding.RegistrationDSN)
		for _, dsn := range []string{c.Binding.CDRDSN, c.Binding.ConfigDSN} {
			if dsn == "" {
				continue
			}
			source, _ := mysql.ParseDSN(dsn)
			if owned.Net == source.Net && owned.Addr == source.Addr && owned.DBName == source.DBName {
				return errors.New("registration database must be module-owned and separate from PBX sources")
			}
		}
	}

	for _, tz := range []string{c.Binding.Timezone, c.Binding.SourceTimezone} {
		if _, e := time.LoadLocation(tz); e != nil {
			return errors.New("invalid timezone")
		}
	}
	if c.Limits.RequestTimeout < time.Duration(c.StreamSeconds)*time.Second {
		return errors.New("framework request_timeout must cover stream_seconds")
	}
	if c.Binding.RegistrationRetentionDays < 0 || c.Binding.RegistrationRetentionDays > 3650 {
		return errors.New("registration_retention_days must be 0 (keep forever) to 3650")
	}
	if c.QueryTimeoutSeconds < 1 || c.QueryTimeoutSeconds > 30 || c.StreamSeconds < 1 || c.StreamSeconds > 300 {
		return errors.New("invalid query/stream bounds")
	}
	if c.AMI.Enabled {
		if _, _, e := net.SplitHostPort(c.AMI.Address); e != nil || c.AMI.Username == "" || c.AMI.Secret == "" {
			return errors.New("AMI credentials/address required")
		}
	}
	if c.Binding.RecordingRoot != "" && !filepath.IsAbs(c.Binding.RecordingRoot) {
		return errors.New("recording_root must be absolute")
	}
	if c.Binding.MonitoringURL != "" {
		u, e := url.Parse(c.Binding.MonitoringURL)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !c.Binding.MonitoringDedicated {
			return errors.New("dedicated monitoring upstream required")
		}
	}
	if c.Gateway.Issuer == "" || c.Gateway.Service == "" {
		return errors.New("gateway issuer/service required")
	}
	return nil
}
func (c Config) String() string {
	return fmt.Sprintf("asterisk tenant=%s pbx=%s secrets=[REDACTED]", c.Binding.TenantID, c.Binding.PBXID)
}
func (c Config) GoString() string { return c.String() }
func (c Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Tenant string `json:"tenant"`
		PBX    string `json:"pbx"`
	}{c.Binding.TenantID, c.Binding.PBXID})
}
func (c Config) Timeout() time.Duration { return time.Duration(c.QueryTimeoutSeconds) * time.Second }
