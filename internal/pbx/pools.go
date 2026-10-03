package pbx

import (
	"context"
	"database/sql"
	"errors"
	"github.com/go-sql-driver/mysql"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"time"
)

type Pools struct {
	CDR     *sql.DB
	Config  *sql.DB
	Columns map[string]bool
	CEL     bool
	Names   bool
	Binding config.Binding
	Timeout time.Duration
}

func Open(ctx context.Context, c config.Config) (p *Pools, err error) {
	p = &Pools{Binding: c.Binding, Timeout: c.Timeout(), Columns: map[string]bool{}}
	opened := p
	defer func() {
		if err != nil {
			opened.Close()
		}
	}()
	p.CDR, err = OpenDB(ctx, c.Binding.CDRDSN, c.Binding.SourceTimezone)
	if err != nil {
		return nil, errors.New("history source unavailable")
	}
	if err = p.CDR.QueryRowContext(ctx, "SELECT 1 FROM cdr LIMIT 1").Scan(new(int)); err != nil && err != sql.ErrNoRows {
		return nil, errors.New("history schema unavailable")
	}
	rows, e := p.CDR.QueryContext(ctx, "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='cdr'")
	if e != nil {
		return nil, errors.New("history schema probe unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if rows.Scan(&s) != nil {
			return nil, errors.New("history schema probe failed")
		}
		p.Columns[s] = true
	}
	if rows.Err() != nil {
		return nil, errors.New("history schema probe failed")
	}
	for _, col := range []string{"linkedid", "uniqueid", "calldate", "channel", "dstchannel", "src", "dst", "disposition", "duration", "billsec"} {
		if !p.Columns[col] {
			return nil, errors.New("required history column unavailable")
		}
	}
	var n int
	p.CEL = p.CDR.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='cel' AND COLUMN_NAME IN ('eventtime','eventtype','channame','uniqueid','linkedid')").Scan(&n) == nil && n == 5
	if c.Binding.ConfigDSN != "" {
		p.Config, _ = OpenDB(ctx, c.Binding.ConfigDSN, c.Binding.SourceTimezone)
		if p.Config != nil {
			p.Names = p.Config.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='users' AND COLUMN_NAME IN ('extension','name')").Scan(&n) == nil && n == 2
		}
	}
	return p, nil
}
func OpenDB(ctx context.Context, dsn, tz string) (*sql.DB, error) {
	c, e := mysql.ParseDSN(dsn)
	if e != nil {
		return nil, errors.New("invalid database configuration")
	}
	c.ParseTime = true
	c.Loc, _ = time.LoadLocation(tz)
	c.Timeout = 3 * time.Second
	c.ReadTimeout = 5 * time.Second
	c.WriteTimeout = 5 * time.Second
	c.MultiStatements = false
	db, e := sql.Open("mysql", c.FormatDSN())
	if e != nil {
		return nil, errors.New("database open failed")
	}
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)
	if e = db.PingContext(ctx); e != nil {
		db.Close()
		return nil, errors.New("database unavailable")
	}
	return db, nil
}
func (p *Pools) Close() {
	if p.CDR != nil {
		p.CDR.Close()
	}
	if p.Config != nil {
		p.Config.Close()
	}
}
func (p *Pools) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	var n int
	e := p.CDR.QueryRowContext(ctx, "SELECT 1 FROM cdr LIMIT 1").Scan(&n)
	if e == sql.ErrNoRows {
		return nil
	}
	return e
}
