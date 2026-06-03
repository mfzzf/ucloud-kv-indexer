// Command kvgateway is the multi-cluster aggregation control plane that sits in
// front of one or more per-cluster (stateless) kvindexer backends. The web
// console talks to this single endpoint and selects a cluster with ?cluster=;
// the gateway fans out reads and proxies writes/admission to the right backend.
//
// The gateway OWNS the connection registry in SQLite (-sqlite-path): the list of
// kvindexers it federates ({id, cluster, url, token, enabled}). On first boot an
// empty registry is SEEDED from -config/-configs topology YAML — one connection
// per clusters[].backends URL, id
// "<cluster>-N", carrying the shared -backend-token. Thereafter the registry is
// authoritative and is managed live via the /admin/connections CRUD endpoints.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ucloud/kv-indexer/internal/config"
	"github.com/ucloud/kv-indexer/internal/gateway"
)

func main() {
	var (
		addr         = flag.String("addr", ":8095", "HTTP listen address")
		sqlitePath   = flag.String("sqlite-path", "data/gateway.db", "SQLite connection registry path: the gateway manages kvindexer connections here and serves /admin/connections CRUD")
		configPath   = flag.String("config", "", "topology YAML; on first boot its clusters[].backends SEED the (empty) connection registry")
		configPaths  = flag.String("configs", "", "comma-separated topology config YAML files; merged for first-boot connection seeding")
		backendToken = flag.String("backend-token", "", "bearer token the gateway sends to every kvindexer (env KVGATEWAY_BACKEND_TOKEN); applied to seeded connections")
	)
	flag.Parse()

	if *backendToken == "" {
		*backendToken = os.Getenv("KVGATEWAY_BACKEND_TOKEN")
	}

	// The gateway OWNS the connection registry in SQLite (inverse topology — the
	// Seed once from -config/-configs when the DB is empty, then
	// the DB + /admin/connections are authoritative.
	store, err := gateway.OpenConnStore(*sqlitePath)
	if err != nil {
		log.Fatalf("connection store: %v", err)
	}
	defer store.Close()

	paths := splitConfigPaths(*configPath, *configPaths)
	if len(paths) > 0 {
		seed := connectionsFromConfigs(paths, *backendToken)
		if ok, err := store.SeedIfEmpty(seed); err != nil {
			log.Fatalf("seed connections: %v", err)
		} else if ok {
			log.Printf("seeded %d connection(s) from %s into %s", len(seed), strings.Join(paths, ","), *sqlitePath)
		} else {
			log.Printf("connection registry already populated (%d rows); not re-seeding", store.Count())
		}
	}

	active := store.Backends()
	for _, b := range active {
		log.Printf("connection %-16s cluster=%-12s url=%s token=%s", b.ID, b.Cluster, b.URL, hasTok(b.Token))
	}
	if len(active) == 0 {
		log.Printf("warning: gateway has 0 enabled connections (registry rows=%d); it will federate nothing until you add connections via /admin/connections or seed with -config", store.Count())
	}
	runServer(*addr, gateway.NewWithStore(store, time.Now), len(active))
}

// runServer starts the HTTP server and blocks until SIGINT/SIGTERM.
func runServer(addr string, gw *gateway.Gateway, n int) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           gw.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("kvgateway listening on %s, federating %d backend(s)", addr, n)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	<-sigc
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func hasTok(t string) string {
	if t != "" {
		return "set"
	}
	return "none"
}

func splitConfigPaths(configPath, configPaths string) []string {
	var out []string
	if configPath != "" {
		out = append(out, configPath)
	}
	for _, p := range strings.Split(configPaths, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func connectionsFromConfigs(paths []string, token string) []gateway.Connection {
	var out []gateway.Connection
	for _, path := range paths {
		bs, err := config.LoadBootstrap(path)
		if err != nil {
			log.Fatalf("read config %s: %v", path, err)
		}
		out = append(out, connectionsFromConfig(bs, token)...)
	}
	return out
}

// connectionsFromConfig derives the seed connection list from the shared topology
// config's clusters[].backends, applying the shared bearer token to each. A
// cluster's `enabled` flag is honored (nil = enabled), matching how the kvindexer
// treats it — so a cluster taken out of service in YAML is not federated.
func connectionsFromConfig(bs *config.Bootstrap, token string) []gateway.Connection {
	var out []gateway.Connection
	for _, c := range bs.AllClusters() {
		enabled := c.Enabled == nil || *c.Enabled
		for i, url := range c.Backends {
			out = append(out, gateway.Connection{
				ID:      c.ClusterID + "-" + itoa(i),
				Cluster: c.ClusterID,
				URL:     url,
				Token:   token,
				Enabled: enabled,
			})
		}
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
