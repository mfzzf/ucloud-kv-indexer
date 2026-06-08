// Command openapi writes the OpenAPI documents exposed by the kvindexer and
// gateway services. It is used by `make openapi` so the checked-in JSON stays
// in sync with the spec served at /openapi.json.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ucloud/kv-indexer/internal/openapi"
)

func main() {
	kind := flag.String("kind", "gateway", "spec to emit: kvindexer or gateway")
	out := flag.String("out", "", "output path; stdout when empty or -")
	flag.Parse()

	spec, err := specForKind(*kind)
	if err != nil {
		log.Fatal(err)
	}

	var f *os.File
	switch *out {
	case "", "-":
		f = os.Stdout
	default:
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			log.Fatalf("create output directory: %v", err)
		}
		f, err = os.Create(*out)
		if err != nil {
			log.Fatalf("create %s: %v", *out, err)
		}
		defer f.Close()
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(spec); err != nil {
		log.Fatalf("encode OpenAPI: %v", err)
	}
}

func specForKind(kind string) (map[string]any, error) {
	switch kind {
	case "kvindexer":
		return openapi.KVIndexerSpec(), nil
	case "gateway":
		return openapi.GatewaySpec(), nil
	default:
		return nil, fmt.Errorf("unknown -kind %q (want kvindexer or gateway)", kind)
	}
}
