package openapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckedInOpenAPIJSONIsCurrent(t *testing.T) {
	tests := []struct {
		name string
		path string
		spec map[string]any
	}{
		{name: "kvindexer", path: "kvindexer.openapi.json", spec: KVIndexerSpec()},
		{name: "gateway", path: "gateway.openapi.json", spec: GatewaySpec()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.MarshalIndent(tt.spec, "", "  ")
			if err != nil {
				t.Fatalf("marshal spec: %v", err)
			}
			got = append(got, '\n')

			want, err := os.ReadFile(filepath.Join("..", "..", "api", tt.path))
			if err != nil {
				t.Fatalf("read checked-in spec: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("%s is stale; run `make openapi`", filepath.Join("api", tt.path))
			}
		})
	}
}
