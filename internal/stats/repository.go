// Package stats serves the extension directory: numbers and names from the
// FreePBX configuration, for pickers. The call statistics are the support
// reports (internal/support).
package stats

import (
	"context"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
)

type Repository struct {
	CDR *cdr.Repository
}

type DirectoryEntry struct {
	Extension string `json:"extension"`
	Name      string `json:"name"`
}

func (r *Repository) Directory(ctx context.Context, tenant string) ([]DirectoryEntry, error) {
	if e := r.CDR.Pools.Authorize(tenant); e != nil {
		return nil, e
	}
	out := []DirectoryEntry{}
	if !r.CDR.Pools.Names {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.CDR.Pools.Timeout)
	defer cancel()
	rows, e := r.CDR.Pools.Config.QueryContext(ctx, "SELECT extension,COALESCE(name,'') FROM users WHERE extension<>'' ORDER BY extension LIMIT 10001")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v DirectoryEntry
		if e = rows.Scan(&v.Extension, &v.Name); e != nil {
			return nil, e
		}
		out = append(out, v)
		if len(out) > 10000 {
			return nil, cdr.ErrBound
		}
	}
	return out, rows.Err()
}
