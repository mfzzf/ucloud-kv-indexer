package config

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)
)

// SQLitePersister stores the config snapshot in a local SQLite database using
// the pure-Go modernc.org/sqlite driver (no cgo, no external service). It is
// the recommended single-node persistent backend: a real, inspectable database
// file you can open with the `sqlite3` CLI.
//
// Each entity type lives in its own table keyed by id, with the entity payload
// stored as JSON in a `data` column. Storing JSON (rather than a column per
// field) keeps the schema stable as the Go structs evolve, while the id column
// keeps rows individually inspectable/queryable. Because the Store hands us the
// FULL snapshot on every mutation (config is small), Save replaces all rows in
// a single transaction — atomic, with no partial-write window.
type SQLitePersister struct {
	db     *sql.DB
	saveTO time.Duration
}

// NewSQLitePersister opens (creating if needed) the SQLite database at path and
// ensures the schema exists. The caller should defer Close().
func NewSQLitePersister(path string) (*SQLitePersister, error) {
	// _pragma busy_timeout avoids spurious "database is locked" under concurrent
	// writers; WAL improves read/write concurrency for the console + hot path.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}
	// SQLite handles one writer at a time; cap connections to avoid lock churn.
	db.SetMaxOpenConns(1)
	p := &SQLitePersister{db: db, saveTO: 5 * time.Second}
	if err := p.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return p, nil
}

func (p *SQLitePersister) initSchema() error {
	const schema = `
CREATE TABLE IF NOT EXISTS meta     (k TEXT PRIMARY KEY, v TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS clusters (id TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS engines  (id TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS profiles (id TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS policies (id TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS audit    (seq INTEGER PRIMARY KEY AUTOINCREMENT, data TEXT NOT NULL);
`
	if _, err := p.db.Exec(schema); err != nil {
		return fmt.Errorf("sqlite: init schema: %w", err)
	}
	return nil
}

// Save replaces all rows from the snapshot in a single transaction. Best-effort:
// errors are logged (not returned) so a transient DB problem never blocks a
// config mutation — the in-memory store stays authoritative for the process.
func (p *SQLitePersister) Save(snap Snapshot) {
	tx, err := p.db.Begin()
	if err != nil {
		log.Printf("config: sqlite begin: %v", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, stmt := range []string{
		"DELETE FROM meta", "DELETE FROM clusters", "DELETE FROM engines",
		"DELETE FROM profiles", "DELETE FROM policies", "DELETE FROM audit",
	} {
		if _, err := tx.Exec(stmt); err != nil {
			log.Printf("config: sqlite clear: %v", err)
			return
		}
	}

	if _, err := tx.Exec("INSERT INTO meta(k,v) VALUES('version',?)", itoa(snap.Version)); err != nil {
		log.Printf("config: sqlite meta: %v", err)
		return
	}
	if !insertJSONRows(tx, "clusters", len(snap.Clusters), func(i int) (string, any) {
		return snap.Clusters[i].ClusterID, snap.Clusters[i]
	}) {
		return
	}
	if !insertJSONRows(tx, "engines", len(snap.Engines), func(i int) (string, any) {
		return snap.Engines[i].EngineID, snap.Engines[i]
	}) {
		return
	}
	if !insertJSONRows(tx, "profiles", len(snap.Profiles), func(i int) (string, any) {
		return snap.Profiles[i].ModelID, snap.Profiles[i]
	}) {
		return
	}
	if !insertJSONRows(tx, "policies", len(snap.Policies), func(i int) (string, any) {
		return snap.Policies[i].PolicyID, snap.Policies[i]
	}) {
		return
	}
	for _, a := range snap.Audit {
		b, err := json.Marshal(a)
		if err != nil {
			log.Printf("config: sqlite marshal audit: %v", err)
			return
		}
		if _, err := tx.Exec("INSERT INTO audit(data) VALUES(?)", string(b)); err != nil {
			log.Printf("config: sqlite insert audit: %v", err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("config: sqlite commit: %v", err)
		return
	}
	committed = true
}

// insertJSONRows inserts n rows into table, marshaling each entity to JSON.
// Returns false (and logs) on the first error so Save can abort the tx.
func insertJSONRows(tx *sql.Tx, table string, n int, at func(i int) (id string, entity any)) bool {
	q := "INSERT INTO " + table + "(id,data) VALUES(?,?)"
	for i := 0; i < n; i++ {
		id, entity := at(i)
		b, err := json.Marshal(entity)
		if err != nil {
			log.Printf("config: sqlite marshal %s: %v", table, err)
			return false
		}
		if _, err := tx.Exec(q, id, string(b)); err != nil {
			log.Printf("config: sqlite insert %s: %v", table, err)
			return false
		}
	}
	return true
}

// Load reconstructs the Snapshot from the database. An empty database (no
// version row) means "fresh" (ok=false, no error).
func (p *SQLitePersister) Load() (Snapshot, bool, error) {
	var snap Snapshot

	var versionStr string
	err := p.db.QueryRow("SELECT v FROM meta WHERE k='version'").Scan(&versionStr)
	if err == sql.ErrNoRows {
		return Snapshot{}, false, nil // fresh database
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("sqlite: load version: %w", err)
	}
	snap.Version = atoiSafe(versionStr)

	if err := loadJSONRows(p.db, "clusters", func(b []byte) error {
		var c Cluster
		if e := json.Unmarshal(b, &c); e != nil {
			return e
		}
		snap.Clusters = append(snap.Clusters, &c)
		return nil
	}); err != nil {
		return Snapshot{}, false, err
	}
	if err := loadJSONRows(p.db, "engines", func(b []byte) error {
		var e Engine
		if er := json.Unmarshal(b, &e); er != nil {
			return er
		}
		snap.Engines = append(snap.Engines, &e)
		return nil
	}); err != nil {
		return Snapshot{}, false, err
	}
	if err := loadJSONRows(p.db, "profiles", func(b []byte) error {
		var pr ModelProfile
		if e := json.Unmarshal(b, &pr); e != nil {
			return e
		}
		snap.Profiles = append(snap.Profiles, &pr)
		return nil
	}); err != nil {
		return Snapshot{}, false, err
	}
	if err := loadJSONRows(p.db, "policies", func(b []byte) error {
		var po Policy
		if e := json.Unmarshal(b, &po); e != nil {
			return e
		}
		snap.Policies = append(snap.Policies, &po)
		return nil
	}); err != nil {
		return Snapshot{}, false, err
	}
	// Audit ordered by insertion sequence to preserve the original order.
	rows, err := p.db.Query("SELECT data FROM audit ORDER BY seq ASC")
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("sqlite: load audit: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return Snapshot{}, false, err
		}
		var a AuditEntry
		if err := json.Unmarshal([]byte(data), &a); err != nil {
			return Snapshot{}, false, err
		}
		snap.Audit = append(snap.Audit, a)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, false, err
	}
	return snap, true, nil
}

// loadJSONRows reads every `data` cell from a table and hands it to fn.
func loadJSONRows(db *sql.DB, table string, fn func([]byte) error) error {
	rows, err := db.Query("SELECT data FROM " + table)
	if err != nil {
		return fmt.Errorf("sqlite: load %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return err
		}
		if err := fn([]byte(data)); err != nil {
			return fmt.Errorf("sqlite: decode %s: %w", table, err)
		}
	}
	return rows.Err()
}

// Close closes the database.
func (p *SQLitePersister) Close() error {
	return p.db.Close()
}

// atoiSafe parses a non-negative int, returning 0 on any error.
func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
