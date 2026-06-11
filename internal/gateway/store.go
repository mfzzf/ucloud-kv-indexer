package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"sort"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

var ErrTokenizerAssetNotFound = errors.New("tokenizer asset not found")

// Connection is one kvindexer the gateway federates: a cluster served by a
// kvindexer at URL, reached with an optional bearer Token. Enabled rows are
// included in the gateway's active backend set.
//
// The gateway owns this registry. Each kvindexer loads only its own cluster
// config; the gateway decides which kvindexers exist and which credential is
// attached to the gateway-to-kvindexer hop.
type Connection struct {
	ID      string `json:"id" bson:"_id"`
	Cluster string `json:"cluster" bson:"cluster"`
	URL     string `json:"url" bson:"url"`
	Token   string `json:"token,omitempty" bson:"token,omitempty"`
	Enabled bool   `json:"enabled" bson:"enabled"`
}

// TokenizerAsset is the gateway-owned local tokenizer material for one
// cluster/model pair. Zip bytes live in GridFS for MongoDB and in-memory only in
// tests.
type TokenizerAsset struct {
	ID                 string        `json:"-" bson:"_id"`
	Cluster            string        `json:"cluster" bson:"cluster"`
	ModelID            string        `json:"model_id" bson:"model_id"`
	ZipFileID          bson.ObjectID `json:"-" bson:"zip_file_id,omitempty"`
	ZipSHA256          string        `json:"zip_sha256,omitempty" bson:"zip_sha256,omitempty"`
	ChatTemplate       string        `json:"-" bson:"chat_template,omitempty"`
	ChatTemplateSHA256 string        `json:"chat_template_sha256,omitempty" bson:"chat_template_sha256,omitempty"`
	UpdatedAt          time.Time     `json:"updated_at" bson:"updated_at"`

	zipBytes []byte `bson:"-"`
}

// TokenizerAssetInput is an upsert payload for TokenizerAsset. Empty
// TokenizerZip/ChatTemplate preserve existing values when the asset already
// exists.
type TokenizerAssetInput struct {
	Cluster            string
	ModelID            string
	TokenizerZip       []byte
	TokenizerZipName   string
	ChatTemplate       string
	ChatTemplateSHA256 string
}

// Store is the gateway's persistence boundary. Production uses MongoDB; tests
// use the in-memory implementation below.
type Store interface {
	Close() error
	Description() string
	Count() int
	SeedIfEmpty([]Connection) (bool, error)
	List() []Connection
	Backends() []Backend
	Upsert(Connection) error
	Delete(id string) error

	PutTokenizerAsset(context.Context, TokenizerAssetInput) (TokenizerAsset, error)
	GetTokenizerAsset(context.Context, string, string) (TokenizerAsset, error)
	ReadTokenizerZip(context.Context, TokenizerAsset, io.Writer) error
}

// ConnStore is a MongoDB-backed gateway store. Connections are ordinary
// documents; tokenizer zips are stored in a GridFS bucket so they do not bloat
// the profile document.
type ConnStore struct {
	client *mongo.Client
	db     *mongo.Database
	bucket *mongo.GridFSBucket

	timeout time.Duration
	label   string

	mu    sync.RWMutex
	cache []Connection
}

// OpenMongoConnStore connects to MongoDB and initializes indexes.
func OpenMongoConnStore(parent context.Context, uri, database string) (*ConnStore, error) {
	if uri == "" {
		return nil, fmt.Errorf("gateway mongo: uri required")
	}
	if database == "" {
		return nil, fmt.Errorf("gateway mongo: database required")
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("gateway mongo: connect: %w", err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("gateway mongo: ping: %w", err)
	}

	db := client.Database(database)
	s := &ConnStore{
		client:  client,
		db:      db,
		bucket:  db.GridFSBucket(options.GridFSBucket().SetName("tokenizer_blobs")),
		timeout: 5 * time.Second,
		label:   database,
	}
	if err := s.initIndexes(parent); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	if err := s.reload(); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	return s, nil
}

func (s *ConnStore) initIndexes(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	if _, err := s.db.Collection("connections").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "cluster", Value: 1}}},
		{Keys: bson.D{{Key: "enabled", Value: 1}}},
	}); err != nil {
		return fmt.Errorf("gateway mongo: create connection indexes: %w", err)
	}
	if _, err := s.db.Collection("tokenizer_assets").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "cluster", Value: 1}, {Key: "model_id", Value: 1}}},
	}); err != nil {
		return fmt.Errorf("gateway mongo: create tokenizer asset indexes: %w", err)
	}
	return nil
}

func (s *ConnStore) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	return s.client.Disconnect(ctx)
}

func (s *ConnStore) Description() string {
	if s.label == "" {
		return "mongo"
	}
	return "mongo:" + s.label
}

func (s *ConnStore) Count() int {
	s.reloadBestEffort()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cache)
}

// SeedIfEmpty inserts connections only when the registry has no rows.
func (s *ConnStore) SeedIfEmpty(conns []Connection) (bool, error) {
	if s.Count() > 0 || len(conns) == 0 {
		return false, nil
	}
	for _, c := range conns {
		if err := s.upsertNoReload(c); err != nil {
			return false, err
		}
	}
	return true, s.reload()
}

func (s *ConnStore) List() []Connection {
	s.reloadBestEffort()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Connection, len(s.cache))
	copy(out, s.cache)
	return out
}

func (s *ConnStore) Backends() []Backend {
	s.reloadBestEffort()
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

func (s *ConnStore) Upsert(c Connection) error {
	if err := s.upsertNoReload(c); err != nil {
		return err
	}
	return s.reload()
}

func (s *ConnStore) upsertNoReload(c Connection) error {
	if err := validateConnection(c); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	_, err := s.db.Collection("connections").ReplaceOne(
		ctx,
		bson.M{"_id": c.ID},
		c,
		options.Replace().SetUpsert(true),
	)
	return err
}

func (s *ConnStore) Delete(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	if _, err := s.db.Collection("connections").DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		return err
	}
	return s.reload()
}

func (s *ConnStore) reload() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	cur, err := s.db.Collection("connections").Find(
		ctx,
		bson.M{},
		options.Find().SetSort(bson.D{{Key: "cluster", Value: 1}, {Key: "_id", Value: 1}}),
	)
	if err != nil {
		return fmt.Errorf("gateway mongo: list connections: %w", err)
	}
	defer cur.Close(ctx)
	var cs []Connection
	if err := cur.All(ctx, &cs); err != nil {
		return fmt.Errorf("gateway mongo: decode connections: %w", err)
	}
	s.mu.Lock()
	s.cache = cs
	s.mu.Unlock()
	return nil
}

func (s *ConnStore) reloadBestEffort() {
	if err := s.reload(); err != nil {
		log.Printf("gateway mongo: refresh connection registry failed: %v", err)
	}
}

func (s *ConnStore) PutTokenizerAsset(parent context.Context, in TokenizerAssetInput) (TokenizerAsset, error) {
	if in.Cluster == "" || in.ModelID == "" {
		return TokenizerAsset{}, fmt.Errorf("tokenizer asset requires cluster and model_id")
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	existing, err := s.getTokenizerAsset(ctx, in.Cluster, in.ModelID)
	if err != nil && !errors.Is(err, ErrTokenizerAssetNotFound) {
		return TokenizerAsset{}, err
	}

	asset := existing
	asset.ID = tokenizerAssetID(in.Cluster, in.ModelID)
	asset.Cluster = in.Cluster
	asset.ModelID = in.ModelID
	if in.ChatTemplate != "" {
		asset.ChatTemplate = in.ChatTemplate
		asset.ChatTemplateSHA256 = sha256String(in.ChatTemplate)
	}
	if in.ChatTemplateSHA256 != "" {
		asset.ChatTemplateSHA256 = in.ChatTemplateSHA256
	}

	var oldZip bson.ObjectID
	if !existing.ZipFileID.IsZero() {
		oldZip = existing.ZipFileID
	}
	if len(in.TokenizerZip) > 0 {
		name := in.TokenizerZipName
		if name == "" {
			name = safeAssetFilename(in.ModelID) + ".zip"
		}
		fileID, err := s.bucket.UploadFromStream(ctx, name, bytes.NewReader(in.TokenizerZip))
		if err != nil {
			return TokenizerAsset{}, fmt.Errorf("gateway mongo: upload tokenizer zip: %w", err)
		}
		asset.ZipFileID = fileID
		asset.ZipSHA256 = sha256Bytes(in.TokenizerZip)
	}
	asset.UpdatedAt = time.Now()

	_, err = s.db.Collection("tokenizer_assets").ReplaceOne(
		ctx,
		bson.M{"_id": asset.ID},
		asset,
		options.Replace().SetUpsert(true),
	)
	if err != nil {
		return TokenizerAsset{}, fmt.Errorf("gateway mongo: upsert tokenizer asset: %w", err)
	}
	if !oldZip.IsZero() && oldZip != asset.ZipFileID {
		if err := s.bucket.Delete(ctx, oldZip); err != nil && !errors.Is(err, mongo.ErrFileNotFound) {
			log.Printf("gateway mongo: delete old tokenizer zip %s: %v", oldZip.Hex(), err)
		}
	}
	return asset, nil
}

func (s *ConnStore) GetTokenizerAsset(parent context.Context, cluster, modelID string) (TokenizerAsset, error) {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	return s.getTokenizerAsset(ctx, cluster, modelID)
}

func (s *ConnStore) getTokenizerAsset(ctx context.Context, cluster, modelID string) (TokenizerAsset, error) {
	var asset TokenizerAsset
	err := s.db.Collection("tokenizer_assets").FindOne(ctx, bson.M{"_id": tokenizerAssetID(cluster, modelID)}).Decode(&asset)
	if err == mongo.ErrNoDocuments {
		return TokenizerAsset{}, ErrTokenizerAssetNotFound
	}
	if err != nil {
		return TokenizerAsset{}, fmt.Errorf("gateway mongo: get tokenizer asset: %w", err)
	}
	return asset, nil
}

func (s *ConnStore) ReadTokenizerZip(parent context.Context, asset TokenizerAsset, w io.Writer) error {
	if asset.ZipFileID.IsZero() {
		return ErrTokenizerAssetNotFound
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if _, err := s.bucket.DownloadToStream(ctx, asset.ZipFileID, w); err != nil {
		return fmt.Errorf("gateway mongo: read tokenizer zip: %w", err)
	}
	return nil
}

// MemoryStore is intentionally small and is used by gateway unit tests. It
// implements the same semantics as Mongo for connection CRUD and tokenizer
// assets, without external services.
type MemoryStore struct {
	mu          sync.RWMutex
	conns       map[string]Connection
	assetByID   map[string]TokenizerAsset
	description string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		conns:       map[string]Connection{},
		assetByID:   map[string]TokenizerAsset{},
		description: "memory",
	}
}

func (s *MemoryStore) Close() error { return nil }

func (s *MemoryStore) Description() string { return s.description }

func (s *MemoryStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns)
}

func (s *MemoryStore) SeedIfEmpty(conns []Connection) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.conns) > 0 || len(conns) == 0 {
		return false, nil
	}
	for _, c := range conns {
		if err := validateConnection(c); err != nil {
			return false, err
		}
		s.conns[c.ID] = c
	}
	return true, nil
}

func (s *MemoryStore) List() []Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Connection, 0, len(s.conns))
	for _, c := range s.conns {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cluster != out[j].Cluster {
			return out[i].Cluster < out[j].Cluster
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *MemoryStore) Backends() []Backend {
	conns := s.List()
	var out []Backend
	for _, c := range conns {
		if c.Enabled {
			out = append(out, Backend{ID: c.ID, Cluster: c.Cluster, URL: c.URL, Token: c.Token})
		}
	}
	return out
}

func (s *MemoryStore) Upsert(c Connection) error {
	if err := validateConnection(c); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[c.ID] = c
	return nil
}

func (s *MemoryStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, id)
	return nil
}

func (s *MemoryStore) PutTokenizerAsset(_ context.Context, in TokenizerAssetInput) (TokenizerAsset, error) {
	if in.Cluster == "" || in.ModelID == "" {
		return TokenizerAsset{}, fmt.Errorf("tokenizer asset requires cluster and model_id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := tokenizerAssetID(in.Cluster, in.ModelID)
	asset := s.assetByID[id]
	asset.ID = id
	asset.Cluster = in.Cluster
	asset.ModelID = in.ModelID
	if in.ChatTemplate != "" {
		asset.ChatTemplate = in.ChatTemplate
		asset.ChatTemplateSHA256 = sha256String(in.ChatTemplate)
	}
	if in.ChatTemplateSHA256 != "" {
		asset.ChatTemplateSHA256 = in.ChatTemplateSHA256
	}
	if len(in.TokenizerZip) > 0 {
		asset.zipBytes = append(asset.zipBytes[:0], in.TokenizerZip...)
		asset.ZipSHA256 = sha256Bytes(in.TokenizerZip)
		asset.ZipFileID = bson.NewObjectID()
	}
	asset.UpdatedAt = time.Now()
	s.assetByID[id] = asset
	return asset, nil
}

func (s *MemoryStore) GetTokenizerAsset(_ context.Context, cluster, modelID string) (TokenizerAsset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	asset, ok := s.assetByID[tokenizerAssetID(cluster, modelID)]
	if !ok {
		return TokenizerAsset{}, ErrTokenizerAssetNotFound
	}
	if len(asset.zipBytes) > 0 {
		asset.zipBytes = append([]byte(nil), asset.zipBytes...)
	}
	return asset, nil
}

func (s *MemoryStore) ReadTokenizerZip(_ context.Context, asset TokenizerAsset, w io.Writer) error {
	if len(asset.zipBytes) == 0 {
		return ErrTokenizerAssetNotFound
	}
	_, err := w.Write(asset.zipBytes)
	return err
}

// validateConnection checks required fields and that the URL is a usable
// absolute http(s) URL.
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

func tokenizerAssetID(cluster, modelID string) string {
	sum := sha256.Sum256([]byte(cluster + "\x00" + modelID))
	return hex.EncodeToString(sum[:])
}

func sha256Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sha256String(s string) string {
	return sha256Bytes([]byte(s))
}

func safeAssetFilename(modelID string) string {
	sum := sha256.Sum256([]byte(modelID))
	return "tokenizer-" + hex.EncodeToString(sum[:8])
}
