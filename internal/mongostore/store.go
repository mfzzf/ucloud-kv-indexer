// Package mongostore provides MongoDB-backed persistence for dynamic config
// and an async sink for decoded KV-cache ZMQ events.
package mongostore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ucloud/kv-indexer/internal/config"
	"github.com/ucloud/kv-indexer/internal/kvevents"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const (
	defaultTimeout      = 5 * time.Second
	eventBufferSize     = 8192
	eventFlushBatchSize = 256
	eventFlushInterval  = time.Second
)

// Store implements config.Persister and kvevents.EventSink using one MongoDB
// database. Config loads from an atomic active snapshot document; per-entity
// collections are mirrored for inspection and ad hoc querying.
type Store struct {
	client *mongo.Client
	db     *mongo.Database

	timeout time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	events chan kvevents.KVEventRecord
	done   chan struct{}
	once   sync.Once
}

// Open connects to MongoDB, pings it, creates useful indexes, and starts the
// async prefix-cache event writer.
func Open(parent context.Context, uri, database string) (*Store, error) {
	if uri == "" {
		return nil, fmt.Errorf("mongo uri required")
	}
	if database == "" {
		return nil, fmt.Errorf("mongo database required")
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	runCtx, runCancel := context.WithCancel(parent)
	s := &Store{
		client:  client,
		db:      client.Database(database),
		timeout: defaultTimeout,
		ctx:     runCtx,
		cancel:  runCancel,
		events:  make(chan kvevents.KVEventRecord, eventBufferSize),
		done:    make(chan struct{}),
	}
	if err := s.initIndexes(parent); err != nil {
		runCancel()
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	go s.eventLoop()
	return s, nil
}

func (s *Store) initIndexes(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	_, err := s.db.Collection("prefix_cache_events").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "observed_at", Value: -1}}},
		{Keys: bson.D{{Key: "engine_id", Value: 1}, {Key: "seq", Value: 1}}},
		{Keys: bson.D{{Key: "namespace", Value: 1}, {Key: "kind", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("mongo create indexes: %w", err)
	}
	return nil
}

// Save persists the active config snapshot. Errors are logged to keep config
// mutations non-blocking for callers; the in-memory store remains authoritative
// for the process.
func (s *Store) Save(snap config.Snapshot) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	if err := s.saveSnapshot(ctx, snap); err != nil {
		log.Printf("config: mongo save: %v", err)
	}
}

func (s *Store) saveSnapshot(ctx context.Context, snap config.Snapshot) error {
	doc, err := toDoc(snap)
	if err != nil {
		return err
	}
	_, err = s.db.Collection("config_snapshots").ReplaceOne(
		ctx,
		bson.M{"_id": "active"},
		bson.M{"_id": "active", "version": snap.Version, "data": doc, "updated_at": time.Now()},
		options.Replace().SetUpsert(true),
	)
	if err != nil {
		return err
	}

	if err := s.replaceEntityCollection(ctx, "clusters", len(snap.Clusters), func(i int) (string, any) {
		return snap.Clusters[i].ClusterID, snap.Clusters[i]
	}); err != nil {
		return err
	}
	if err := s.replaceEntityCollection(ctx, "engines", len(snap.Engines), func(i int) (string, any) {
		return snap.Engines[i].EngineID, snap.Engines[i]
	}); err != nil {
		return err
	}
	if err := s.replaceEntityCollection(ctx, "profiles", len(snap.Profiles), func(i int) (string, any) {
		return snap.Profiles[i].ModelID, snap.Profiles[i]
	}); err != nil {
		return err
	}
	if err := s.replaceEntityCollection(ctx, "policies", len(snap.Policies), func(i int) (string, any) {
		return snap.Policies[i].PolicyID, snap.Policies[i]
	}); err != nil {
		return err
	}
	return s.replaceEntityCollection(ctx, "audit", len(snap.Audit), func(i int) (string, any) {
		return fmt.Sprintf("%08d", i), snap.Audit[i]
	})
}

func (s *Store) replaceEntityCollection(ctx context.Context, name string, n int, at func(int) (string, any)) error {
	coll := s.db.Collection(name)
	if _, err := coll.DeleteMany(ctx, bson.M{}); err != nil {
		return fmt.Errorf("clear %s: %w", name, err)
	}
	if n == 0 {
		return nil
	}
	docs := make([]any, 0, n)
	for i := 0; i < n; i++ {
		id, entity := at(i)
		doc, err := toDoc(entity)
		if err != nil {
			return fmt.Errorf("marshal %s/%s: %w", name, id, err)
		}
		docs = append(docs, bson.M{"_id": id, "data": doc})
	}
	if _, err := coll.InsertMany(ctx, docs); err != nil {
		return fmt.Errorf("insert %s: %w", name, err)
	}
	return nil
}

// Load restores the active config snapshot. ok=false means Mongo has no active
// config yet, so the caller should seed from bootstrap YAML.
func (s *Store) Load() (config.Snapshot, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	var row struct {
		Data bson.M `bson:"data"`
	}
	err := s.db.Collection("config_snapshots").FindOne(ctx, bson.M{"_id": "active"}).Decode(&row)
	if err == mongo.ErrNoDocuments {
		return config.Snapshot{}, false, nil
	}
	if err != nil {
		return config.Snapshot{}, false, fmt.Errorf("mongo load config snapshot: %w", err)
	}
	var snap config.Snapshot
	if err := fromDoc(row.Data, &snap); err != nil {
		return config.Snapshot{}, false, err
	}
	return snap, true, nil
}

// RecordKVEvent queues a decoded KV event for async insertion into
// prefix_cache_events. If Mongo cannot keep up, events are dropped rather than
// blocking ZMQ ingest.
func (s *Store) RecordKVEvent(rec kvevents.KVEventRecord) {
	select {
	case s.events <- rec:
	default:
		log.Printf("mongo prefix_cache_events queue full; dropping event engine=%s seq=%s kind=%s", rec.EngineID, rec.Seq, rec.Kind)
	}
}

// RecentKVEvents returns the most recent persisted decoded KV events, oldest
// first. It is used to repopulate the Streams page after a kvindexer restart.
func (s *Store) RecentKVEvents(parent context.Context, limit int) ([]kvevents.KVEventRecord, error) {
	if limit <= 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	cur, err := s.db.Collection("prefix_cache_events").Find(
		ctx,
		bson.M{},
		options.Find().SetSort(bson.D{{Key: "observed_at", Value: -1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("mongo recent prefix_cache_events: %w", err)
	}
	defer cur.Close(ctx)

	var newestFirst []kvevents.KVEventRecord
	if err := cur.All(ctx, &newestFirst); err != nil {
		return nil, fmt.Errorf("mongo decode prefix_cache_events: %w", err)
	}
	for i, j := 0, len(newestFirst)-1; i < j; i, j = i+1, j-1 {
		newestFirst[i], newestFirst[j] = newestFirst[j], newestFirst[i]
	}
	return newestFirst, nil
}

func (s *Store) eventLoop() {
	defer close(s.done)
	ticker := time.NewTicker(eventFlushInterval)
	defer ticker.Stop()

	batch := make([]any, 0, eventFlushBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
		_, err := s.db.Collection("prefix_cache_events").InsertMany(ctx, batch)
		cancel()
		if err != nil {
			log.Printf("mongo prefix_cache_events insert: %v", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case rec := <-s.events:
			batch = append(batch, rec)
			if len(batch) >= eventFlushBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.ctx.Done():
			for {
				select {
				case rec := <-s.events:
					batch = append(batch, rec)
				default:
					flush()
					return
				}
			}
		}
	}
}

// Close stops the event writer and disconnects the MongoDB client.
func (s *Store) Close() error {
	s.once.Do(func() {
		s.cancel()
		<-s.done
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.client.Disconnect(ctx)
}

func toDoc(v any) (bson.M, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out bson.M
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func fromDoc(doc bson.M, dst any) error {
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
