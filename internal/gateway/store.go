package gateway

import (
	"database/sql"
	"fmt"
	"net/url"
	"sync"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)
)

// Connection is one kvindexer the gateway federates: a cluster served by a
// kvindexer at URL, reached with an optional bearer Token. Enabled rows are
// included in the gateway's active backend set.
//
// This is the inverse of the old topology: the GATEWAY now owns the connection
// registry (which kvindexers exist, where, and with what credential), while each
// kvindexer loads only its own single-cluster bootstrap YAML. The
// gateway↔kvindexer hop crosses the (public) network, so each connection carries
// a bearer Token the gateway attaches to every forwarded request.
type Connection struct {
	ID      string `json:"id"`
	Cluster string `json:"cluster"`
	URL     string `json:"url"`
	Token   string `json:"token,omitempty"` // bearer token sent to this kvindexer
	Enabled bool   `json:"enabled"`
}

// ConnStore is a SQLite-backed registry of kvindexer connections. It is the
// gateway's source of truth after first seed; the admin API mutates it and the
// gateway reads an in-memory snapshot refreshed on every write.
type ConnStore struct {
	db *sql.DB

	mu    sync.RWMutex
	cache []Connection
}

// OpenConnStore opens (creating if needed) the SQLite database at path and loads
// the connection cache. The caller should defer Close().
func OpenConnStore(path string) (*ConnStore, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("gateway sqlite: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	s := &ConnStore{db: db}
	const schema = `CREATE TABLE IF NOT EXISTS connections (
        id      TEXT PRIMARY KEY,
        cluster TEXT NOT NULL,
        url     TEXT NOT NULL,
        token   TEXT NOT NULL DEFAULT '',
        enabled INTEGER NOT NULL DEFAULT 1
    );`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("gateway sqlite: init schema: %w", err)
	}
	if err := s.reload(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *ConnStore) Close() error { return s.db.Close() }

// Count returns the number of stored connections (any enabled state).
func (s *ConnStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cache)
}

// SeedIfEmpty inserts the given connections only when the store has no rows
// (first boot). Returns true if it seeded. Thereafter the store is authoritative
// and the admin API mutates it — matching the kvindexer's seed-once semantics.
func (s *ConnStore) SeedIfEmpty(conns []Connection) (bool, error) {
	if s.Count() > 0 {
		return false, nil
	}
	if len(conns) == 0 {
		return false, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	for _, c := range conns {
		if _, err := tx.Exec(
			"INSERT OR REPLACE INTO connections(id,cluster,url,token,enabled) VALUES(?,?,?,?,?)",
			c.ID, c.Cluster, c.URL, c.Token, b2i(c.Enabled),
		); err != nil {
			_ = tx.Rollback()
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, s.reload()
}

// List returns a snapshot copy of all connections.
func (s *ConnStore) List() []Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Connection, len(s.cache))
	copy(out, s.cache)
	return out
}

// Backends returns the enabled connections as gateway Backends (the active set
// the gateway federates).
func (s *ConnStore) Backends() []Backend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Backend
	for _, c := range s.cache {
		if c.Enabled {
			out = append(out, Backend{ID: c.ID, Cluster: c.Cluster, URL: c.URL, Token: c.Token})
		}
	}
	return out
}

// Upsert inserts or updates a connection, then refreshes the cache.
func (s *ConnStore) Upsert(c Connection) error {
	if err := validateConnection(c); err != nil {
		return err
	}
	if _, err := s.db.Exec(
		"INSERT OR REPLACE INTO connections(id,cluster,url,token,enabled) VALUES(?,?,?,?,?)",
		c.ID, c.Cluster, c.URL, c.Token, b2i(c.Enabled),
	); err != nil {
		return err
	}
	return s.reload()
}

// validateConnection checks required fields and that the URL is a usable
// absolute http(s) URL — rejecting a scheme-less or malformed URL at
// registration time rather than letting it fail opaquely as a 502 later.
func validateConnection(c Connection) error {
	if c.ID == "" || c.Cluster == "" || c.URL == "" {
		return fmt.Errorf("connection requires id, cluster, url")
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("connection url must be an absolute http(s) URL, got %q", c.URL)
	}
	return nil
}

// Delete removes a connection by id, then refreshes the cache.
func (s *ConnStore) Delete(id string) error {
	if _, err := s.db.Exec("DELETE FROM connections WHERE id=?", id); err != nil {
		return err
	}
	return s.reload()
}

// reload refreshes the in-memory cache from the database.
func (s *ConnStore) reload() error {
	rows, err := s.db.Query("SELECT id,cluster,url,token,enabled FROM connections ORDER BY cluster,id")
	if err != nil {
		return fmt.Errorf("gateway sqlite: list: %w", err)
	}
	defer rows.Close()
	var cs []Connection
	for rows.Next() {
		var c Connection
		var en int
		if err := rows.Scan(&c.ID, &c.Cluster, &c.URL, &c.Token, &en); err != nil {
			return err
		}
		c.Enabled = en != 0
		cs = append(cs, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.cache = cs
	s.mu.Unlock()
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
