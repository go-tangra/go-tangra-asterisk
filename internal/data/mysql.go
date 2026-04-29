package data

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
)

// MySQLClients holds *sql.DB handles for the databases this module talks to.
// Cdr + Config are read-only against FreePBX. Tangra is OUR database — used
// for the PJSIP registration event log we capture from AMI. Tangra may be
// nil when the operator hasn't configured a TangraDSN.
type MySQLClients struct {
	Cfg    *Config
	Cdr    *sql.DB
	Config *sql.DB
	Tangra *sql.DB
}

// NewMySQLClients opens the configured pools and verifies connectivity. If
// TangraDSN is set, this also runs the schema migration for the registration
// event log table.
func NewMySQLClients(ctx *bootstrap.Context, cfg *Config) (*MySQLClients, func(), error) {
	l := ctx.NewLoggerHelper("asterisk/data/mysql")

	cdrDB, err := openPool("asteriskcdrdb", cfg.CdrDSN, cfg)
	if err != nil {
		return nil, func() {}, err
	}

	configDB, err := openPool("asterisk", cfg.ConfigDSN, cfg)
	if err != nil {
		_ = cdrDB.Close()
		return nil, func() {}, err
	}

	clients := &MySQLClients{Cfg: cfg, Cdr: cdrDB, Config: configDB}
	l.Infof("MySQL pools opened: cdr=%s config=%s", maskDSN(cfg.CdrDSN), maskDSN(cfg.ConfigDSN))

	if cfg.TangraDSN != "" {
		tangraDB, err := openPool("tangra", cfg.TangraDSN, cfg)
		if err != nil {
			_ = cdrDB.Close()
			_ = configDB.Close()
			return nil, func() {}, err
		}
		if err := migrateTangra(tangraDB); err != nil {
			_ = cdrDB.Close()
			_ = configDB.Close()
			_ = tangraDB.Close()
			return nil, func() {}, fmt.Errorf("migrate tangra: %w", err)
		}
		clients.Tangra = tangraDB
		l.Infof("MySQL pool opened: tangra=%s", maskDSN(cfg.TangraDSN))
	}

	cleanup := func() {
		if err := cdrDB.Close(); err != nil {
			l.Errorf("close cdr db: %v", err)
		}
		if err := configDB.Close(); err != nil {
			l.Errorf("close config db: %v", err)
		}
		if clients.Tangra != nil {
			if err := clients.Tangra.Close(); err != nil {
				l.Errorf("close tangra db: %v", err)
			}
		}
	}

	return clients, cleanup, nil
}

// migrateTangra creates the pjsip_registration_events table if missing.
// We keep this as a single CREATE TABLE so a future schema change can use a
// proper migration tool without conflicting with this idempotent baseline.
func migrateTangra(db *sql.DB) error {
	const ddl = `
		CREATE TABLE IF NOT EXISTS pjsip_registration_events (
			id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			event_time   DATETIME(3)     NOT NULL,
			endpoint     VARCHAR(64)     NOT NULL,
			aor          VARCHAR(64)     NOT NULL DEFAULT '',
			contact_uri  VARCHAR(255)    NOT NULL DEFAULT '',
			status       VARCHAR(16)     NOT NULL,
			user_agent   VARCHAR(255)    NOT NULL DEFAULT '',
			via_address  VARCHAR(64)     NOT NULL DEFAULT '',
			reg_expire   DATETIME(3)     NULL,
			rtt_usec     BIGINT          NOT NULL DEFAULT 0,
			PRIMARY KEY (id),
			KEY idx_endpoint_time (endpoint, event_time),
			KEY idx_time          (event_time)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`
	_, err := db.Exec(ddl)
	return err
}

func openPool(name, dsn string, cfg *Config) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping %s: %w", name, err)
	}
	return db, nil
}

// maskDSN scrubs the password from a MySQL DSN for log output.
func maskDSN(dsn string) string {
	at := -1
	colon := -1
	for i := 0; i < len(dsn); i++ {
		if dsn[i] == '@' {
			at = i
			break
		}
		if dsn[i] == ':' {
			colon = i
		}
	}
	if at < 0 || colon < 0 || colon >= at {
		return dsn
	}
	return dsn[:colon+1] + "***" + dsn[at:]
}
