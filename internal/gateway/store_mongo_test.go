package gateway

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestConnStoreMongoRoundTrip(t *testing.T) {
	uri := os.Getenv("KVGATEWAY_MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set KVGATEWAY_MONGO_TEST_URI to run MongoDB integration test")
	}
	dbName := "kvgateway_test_" + time.Now().Format("20060102150405")
	ctx := context.Background()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect mongo: %v", err)
	}
	defer client.Disconnect(ctx)
	defer client.Database(dbName).Drop(ctx)

	s, err := OpenMongoConnStore(ctx, uri, dbName)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if ok, err := s.SeedIfEmpty([]Connection{
		{ID: "mongo-0", Cluster: "mongo", URL: "http://10.0.0.1:8090", Token: "tok", Enabled: true},
	}); err != nil || !ok {
		t.Fatalf("seed: ok=%v err=%v", ok, err)
	}
	if err := s.Upsert(Connection{ID: "mongo-0", Cluster: "mongo", URL: "http://10.0.0.2:8090", Token: "tok2", Enabled: false}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got := s.Backends(); len(got) != 0 {
		t.Fatalf("disabled backend still active: %+v", got)
	}

	asset, err := s.PutTokenizerAsset(ctx, TokenizerAssetInput{
		Cluster:          "mongo",
		ModelID:          "model-a",
		TokenizerZip:     []byte("zip-bytes"),
		TokenizerZipName: "model-a.zip",
		ChatTemplate:     "template",
	})
	if err != nil {
		t.Fatalf("put asset: %v", err)
	}
	gotAsset, err := s.GetTokenizerAsset(ctx, "mongo", "model-a")
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	if gotAsset.ZipSHA256 != asset.ZipSHA256 || gotAsset.ChatTemplateSHA256 == "" {
		t.Fatalf("asset mismatch: got=%+v want=%+v", gotAsset, asset)
	}
	var buf bytes.Buffer
	if err := s.ReadTokenizerZip(ctx, gotAsset, &buf); err != nil {
		t.Fatalf("read zip: %v", err)
	}
	if buf.String() != "zip-bytes" {
		t.Fatalf("zip bytes=%q", buf.String())
	}
}
