// Command kvindexer runs the ucloud-kv-indexer admission/cache-hit service.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ucloud/kv-indexer/internal/config"
	"github.com/ucloud/kv-indexer/internal/httpapi"
	"github.com/ucloud/kv-indexer/internal/residency"
	"github.com/ucloud/kv-indexer/internal/tokenizer"
)

func main() {
	var (
		addr       = flag.String("addr", ":8090", "HTTP listen address")
		store      = flag.String("store", "memory", "config persistence backend: memory | file | sqlite")
		snapPath   = flag.String("config", "data/config.json", "config snapshot path (store=file)")
		sqlitePath = flag.String("sqlite-path", "data/config.db", "SQLite database path (store=sqlite)")
		bootPath   = flag.String("bootstrap", "", "YAML bootstrap seed (config.yaml); REQUIRED for store=memory (loaded into memory each boot)")
		cluster    = flag.String("cluster", "", "scope this instance to a single cluster_id from the bootstrap (empty = seed whole topology)")
		authToken  = flag.String("auth-token", "", "Bearer token required on the API (env KVINDEXER_AUTH_TOKEN); empty = no auth (loopback dev only)")
	)
	flag.Parse()

	if *authToken == "" {
		*authToken = os.Getenv("KVINDEXER_AUTH_TOKEN")
	}

	persister, closer := buildPersister(*store, *snapPath, *sqlitePath)
	if closer != nil {
		defer closer()
	}

	st := config.NewStoreWith(persister, time.Now)
	if err := st.Load(); err != nil {
		log.Printf("config load: %v (starting empty)", err)
	}

	idx := residency.NewManager(time.Now)
	tok := tokenizer.New()
	svc := httpapi.NewService(st, idx, tok, time.Now)
	svc.AuthToken = *authToken

	// The store is seeded from -bootstrap YAML when empty — which, for store=memory
	// (the default), is EVERY boot since nothing is persisted. config.yaml is the
	// config authority; a kvindexer scoped with -cluster seeds only that cluster.
	if *bootPath != "" {
		bs, err := config.LoadBootstrap(*bootPath)
		if err != nil {
			log.Fatalf("bootstrap: %v", err)
		}
		if st.ApplyBootstrapForCluster(bs, *cluster) {
			scope := "all clusters"
			if *cluster != "" {
				scope = "cluster=" + *cluster
			}
			log.Printf("applied bootstrap %s (%s, version=%d)", *bootPath, scope, st.Version())
		} else {
			log.Printf("bootstrap skipped: store already has config (version=%d)", st.Version())
		}
	} else if *store == "memory" {
		log.Printf("warning: store=memory with no -bootstrap; starting with empty config")
	}

	// Start listeners for all configured engines.
	svc.SyncListeners()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           svc.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		auth := "no-auth"
		if *authToken != "" {
			auth = "bearer-token"
		}
		log.Printf("ucloud-kv-indexer listening on %s (store=%s, %s)", *addr, *store, auth)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	<-sigc
	log.Println("shutting down...")
	svc.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// buildPersister constructs the configured persistence backend. Returns the
// persister and an optional cleanup func (sqlite close). The default, `memory`,
// is stateless: config comes from -bootstrap YAML on every boot.
func buildPersister(store, snapPath, sqlitePath string) (config.Persister, func()) {
	switch store {
	case "memory":
		// Stateless: no persistence. The store is seeded from -bootstrap YAML on
		// every boot (config authority lives elsewhere — e.g. the gateway). This
		// is the default for a kvindexer sitting remotely next to one cluster.
		log.Printf("config store: memory (stateless; seeded from -bootstrap each boot)")
		return nil, nil
	case "file":
		return config.NewFilePersister(snapPath), nil
	case "sqlite":
		sp, err := config.NewSQLitePersister(sqlitePath)
		if err != nil {
			log.Fatalf("sqlite store: %v", err)
		}
		log.Printf("config store: sqlite %s", sqlitePath)
		return sp, func() { _ = sp.Close() }
	default:
		log.Fatalf("unknown -store %q (want memory|file|sqlite)", store)
		return nil, nil
	}
}
