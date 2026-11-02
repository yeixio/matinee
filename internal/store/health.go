package store

import (
	"context"
	"time"
)

// DatabaseInfo describes the database for the admin dashboard.
type DatabaseInfo struct {
	Kind    string `json:"kind"` // sqlite or postgres
	Version string `json:"version,omitempty"`
	// Size is the database's size in bytes; 0 when unknown.
	Size int64 `json:"size"`
	// Schema is how many of Matinee's migrations it has.
	Schema int `json:"schema"`
	// PingMs is how long a trivial query took.
	PingMs float64 `json:"pingMs"`
	// Connections: open, in use, and how often a query had to wait for one
	// and for how long in all.
	OpenConns int     `json:"openConns"`
	InUse     int     `json:"inUse"`
	WaitCount int64   `json:"waitCount"`
	WaitMs    float64 `json:"waitMs"`
	Items     int     `json:"items"`
	Users     int     `json:"users"`
	Libraries int     `json:"libraries"`
}

// Health describes the database: which one, how big, how quick.
func (s *Store) Health(ctx context.Context) (DatabaseInfo, error) {
	info := DatabaseInfo{Kind: "sqlite"}
	if s.db.dialect == postgres {
		info.Kind = "postgres"
	}
	start := time.Now()
	if err := s.db.PingContext(ctx); err != nil {
		return info, err
	}
	var one int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return info, err
	}
	info.PingMs = float64(time.Since(start).Microseconds()) / 1000
	st := s.db.Stats()
	info.OpenConns, info.InUse, info.WaitCount = st.OpenConnections, st.InUse, st.WaitCount
	info.WaitMs = float64(st.WaitDuration.Microseconds()) / 1000

	if info.Kind == "postgres" {
		s.db.QueryRowContext(ctx, "SHOW server_version").Scan(&info.Version)
		s.db.QueryRowContext(ctx, "SELECT pg_database_size(current_database())").Scan(&info.Size)
	} else {
		s.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&info.Version)
		var pages, pageSize int64
		if s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages) == nil &&
			s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize) == nil {
			info.Size = pages * pageSize
		}
	}
	s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&info.Schema)
	for q, dst := range map[string]*int{
		"SELECT COUNT(*) FROM items":                                       &info.Items,
		"SELECT COUNT(*) FROM users WHERE guest = 0 AND parent_id IS NULL": &info.Users,
		"SELECT COUNT(*) FROM libraries":                                   &info.Libraries,
	} {
		if err := s.db.QueryRowContext(ctx, q).Scan(dst); err != nil {
			return info, err
		}
	}
	return info, nil
}
