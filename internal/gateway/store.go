package gateway

import (
	"database/sql"
	"fmt"
	"net/url"
	"sync"

	_ "github.com/go-sql-driver/mysql" // MySQL driver for production gateway registry
	_ "modernc.org/sqlite"             // pure-Go SQLite driver (no cgo)
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

type connStoreDialect string

const (
	dialectSQLite connStoreDialect = "sqlite"
	dialectMySQL  connStoreDialect = "mysql"
)

// ConnStore is a SQL-backed registry of kvindexer connections. It is the
// gateway's source of truth after first seed; the admin API mutates it and the
// gateway reads an in-memory snapshot refreshed on every write. Local dev uses
// SQLite; Kubernetes/production should use MySQL.
type ConnStore struct {
	db      *sql.DB
	dialect connStoreDialect
	label   string

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
	return openConnStore(db, dialectSQLite, path)
}

// OpenMySQLConnStore opens a MySQL-backed connection registry. dsn uses the
// go-sql-driver/mysql form, for example:
//
//	kvgateway:secret@tcp(mysql:3306)/kvgateway?parseTime=true
func OpenMySQLConnStore(dsn string) (*ConnStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("gateway mysql: dsn required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("gateway mysql: open: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	return openConnStore(db, dialectMySQL, "mysql")
}

func openConnStore(db *sql.DB, dialect connStoreDialect, label string) (*ConnStore, error) {
	s := &ConnStore{db: db, dialect: dialect, label: label}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("gateway %s: ping: %w", dialect, err)
	}
	schema := sqliteSchema
	if dialect == dialectMySQL {
		schema = mysqlSchema
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("gateway %s: init schema: %w", dialect, err)
	}
	if err := s.reload(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

const sqliteSchema = `CREATE TABLE IF NOT EXISTS connections (
    id      TEXT PRIMARY KEY,
    cluster TEXT NOT NULL,
    url     TEXT NOT NULL,
    token   TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1
);`

const mysqlSchema = `CREATE TABLE IF NOT EXISTS connections (
    id      VARCHAR(191) PRIMARY KEY,
    cluster VARCHAR(191) NOT NULL,
    url     TEXT NOT NULL,
    token   TEXT NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    INDEX idx_connections_cluster (cluster)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`

// Close closes the database.
func (s *ConnStore) Close() error { return s.db.Close() }

// Description returns a short storage label for logs.
func (s *ConnStore) Description() string {
	if s.label == "" {
		return string(s.dialect)
	}
	return fmt.Sprintf("%s:%s", s.dialect, s.label)
}

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
		if err := s.execUpsert(tx, c); err != nil {
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
	if err := s.execUpsert(s.db, c); err != nil {
		return err
	}
	return s.reload()
}

type sqlExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (s *ConnStore) execUpsert(exec sqlExecutor, c Connection) error {
	if err := validateConnection(c); err != nil {
		return err
	}
	query := `INSERT OR REPLACE INTO connections(id,cluster,url,token,enabled) VALUES(?,?,?,?,?)`
	if s.dialect == dialectMySQL {
		query = `INSERT INTO connections(id,cluster,url,token,enabled) VALUES(?,?,?,?,?)
ON DUPLICATE KEY UPDATE cluster=VALUES(cluster), url=VALUES(url), token=VALUES(token), enabled=VALUES(enabled)`
	}
	_, err := exec.Exec(query, c.ID, c.Cluster, c.URL, c.Token, b2i(c.Enabled))
	return err
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
		return fmt.Errorf("gateway %s: list: %w", s.dialect, err)
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
