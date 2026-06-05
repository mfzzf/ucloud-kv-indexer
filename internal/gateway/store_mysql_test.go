package gateway

import (
	"database/sql"
	"os"
	"testing"
)

func TestConnStoreMySQLRoundTrip(t *testing.T) {
	dsn := os.Getenv("KVGATEWAY_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("set KVGATEWAY_MYSQL_TEST_DSN to run MySQL integration test")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	if _, err := db.Exec("DROP TABLE IF EXISTS connections"); err != nil {
		_ = db.Close()
		t.Fatalf("reset connections table: %v", err)
	}
	_ = db.Close()

	s, err := OpenMySQLConnStore(dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if ok, err := s.SeedIfEmpty([]Connection{
		{ID: "mysql-0", Cluster: "mysql", URL: "http://10.0.0.1:8090", Token: "tok", Enabled: true},
	}); err != nil || !ok {
		t.Fatalf("seed: ok=%v err=%v", ok, err)
	}
	if err := s.Upsert(Connection{ID: "mysql-0", Cluster: "mysql", URL: "http://10.0.0.2:8090", Token: "tok2", Enabled: false}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got := s.Backends(); len(got) != 0 {
		t.Fatalf("disabled backend still active: %+v", got)
	}
	list := s.List()
	if len(list) != 1 || list[0].URL != "http://10.0.0.2:8090" || list[0].Token != "tok2" || list[0].Enabled {
		t.Fatalf("upserted row mismatch: %+v", list)
	}
	if err := s.Delete("mysql-0"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if s.Count() != 0 {
		t.Fatalf("count after delete=%d want 0", s.Count())
	}
}
