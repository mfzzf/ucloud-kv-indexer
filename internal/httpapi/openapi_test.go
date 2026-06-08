package httpapi

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ucloud/kv-indexer/internal/openapi"
)

func TestKVIndexerOpenAPIMatchesRegisteredRoutes(t *testing.T) {
	svc := NewService(nil, nil, nil, time.Now)

	registered := registeredRouteSet(svc.ginRouter().Routes())
	documented := openAPIRouteSet(t, openapi.KVIndexerSpec())

	assertRouteSetsEqual(t, registered, documented)
}

func registeredRouteSet(routes gin.RoutesInfo) map[string]struct{} {
	out := map[string]struct{}{}
	for _, r := range routes {
		out[r.Method+" "+openAPIPath(r.Path)] = struct{}{}
	}
	return out
}

func openAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		switch {
		case strings.HasPrefix(part, ":"):
			parts[i] = "{" + strings.TrimPrefix(part, ":") + "}"
		case strings.HasPrefix(part, "*"):
			parts[i] = "{" + strings.TrimPrefix(part, "*") + "}"
		}
	}
	return strings.Join(parts, "/")
}

func openAPIRouteSet(t *testing.T, spec map[string]any) map[string]struct{} {
	t.Helper()
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatalf("spec paths has type %T", spec["paths"])
	}
	out := map[string]struct{}{}
	for path, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			t.Fatalf("path item %s has type %T", path, rawPathItem)
		}
		for method := range pathItem {
			method = strings.ToUpper(method)
			if isHTTPMethod(method) {
				out[method+" "+path] = struct{}{}
			}
		}
	}
	return out
}

func isHTTPMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD", "TRACE":
		return true
	default:
		return false
	}
}

func assertRouteSetsEqual(t *testing.T, registered, documented map[string]struct{}) {
	t.Helper()
	if missing := sortedDiff(registered, documented); len(missing) > 0 {
		t.Fatalf("registered routes missing from OpenAPI:\n%s", strings.Join(missing, "\n"))
	}
	if stale := sortedDiff(documented, registered); len(stale) > 0 {
		t.Fatalf("OpenAPI documents routes that are not registered:\n%s", strings.Join(stale, "\n"))
	}
}

func sortedDiff(a, b map[string]struct{}) []string {
	var out []string
	for item := range a {
		if _, ok := b[item]; !ok {
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}
