# kvgateway - multi-cluster aggregation control plane

`kvgateway` federates several per-cluster `kvindexer` backends behind one HTTP
endpoint, so the web console can manage many clusters from a single base URL and
switch between them with a `?cluster=` selector.

## Why a gateway (not the frontend fanning out)

Each `kvindexer` sits next to its inference engines because it subscribes to the
engines' **ZMQ KV-cache event stream**. The browser should not dial every
cluster backend directly, so this gateway does the fan-out, merge, write-routing,
and backend authentication centrally.

```
              ┌───────────── web console (one base URL, ?cluster= selector)
              ▼
        kvgateway :8095
        ├── GET  fan-out + merge (cluster-tagged)  -> all selected backends
        ├── POST/PATCH write proxy (one backend)   -> cluster/backend-targeted
        ├── GET/POST/DELETE /admin/connections     -> MongoDB registry
        ├── local tokenizer assets                 -> MongoDB/GridFS + sidecar
        └── GET  /clusters-health                  -> live health probe
              │
      ┌───────┼─────────────────┐
      ▼       ▼                 ▼
 kvindexer  kvindexer        kvindexer
 cluster A  cluster A'       cluster B     (each next to its engines + ZMQ stream)
```

## Endpoints

| Kind | Routes | Behavior |
| --- | --- | --- |
| Fan-out GET | `/clusters` `/engines` `/model-profiles` `/policies` `/event-streams` `/index/stats` `/decisions` `/config/audit-log` | Query every selected backend, tag each array element with `_cluster` + `_backend`, merge. `/decisions` sorted by timestamp, `/config/audit-log` by version. A dead backend is logged and skipped, never blanks the response. |
| Aggregate | `/config/versions` | Per-backend `{cluster, backend, config_version}` array. |
| Write proxy | `POST/PATCH` on clusters, engines (`register`/`unregister`/`{id}`), `model-profiles`, `policies`; `DELETE /policies/{id}` | Proxied to **exactly one** backend. Selector must resolve to one - ambiguous -> `400`, no match -> `404`. Local tokenizer profile uploads are stored by gateway before sanitized profile metadata is forwarded. |
| Admission / query proxy | `POST` `/route` `/v1/chat/completions` `/v1/responses` `/v1/messages` `/query-prefix` `/tokenize/preview` `/config/effective-policy/preview` | Remote tokenizer profiles are proxied to one selected backend. Local tokenizer profiles are handled in gateway with token-count-only admission. |
| Registry admin | `GET/POST /admin/connections`, `DELETE /admin/connections/{id}` | CRUD for the gateway-owned backend registry. Tokens are redacted in list responses. |
| Gateway-native | `GET /clusters-health` `GET /healthz` | `/clusters-health` groups backends by cluster and live-probes each `/healthz`. |

### Selectors

- `?cluster=<id>` - restrict to that cluster (omit, or `cluster=all`, means every cluster for reads).
- `?backend=<id>` - restrict to one exact backend (a cluster may hold several).

Reads default to fan-out across all. Writes require an unambiguous target.

## Running

```bash
go build -o /tmp/kvgateway ./cmd/kvgateway

# Local dev / production: MongoDB registry and tokenizer asset store.
/tmp/kvgateway \
  -addr :8095 \
  -store mongo \
  -mongo-uri mongodb://127.0.0.1:27017 \
  -mongo-db kvgateway \
  -configs deploy/local-vllm.yaml,deploy/local-sglang.yaml \
  -backend-token dev-local-token \
  -local-tokenizer-url http://127.0.0.1:9000
```

Point the web console at the gateway:

```bash
echo 'NEXT_PUBLIC_API_BASE=http://127.0.0.1:8095' > web/.env.local
```

The console auto-detects federation by calling `GET /clusters-health`. Against a plain
single `kvindexer` (no `/clusters-health`), the cluster switcher hides itself and the
app behaves exactly as before.

## Tests

`go test ./internal/gateway/` covers fan-out tagging, cluster filters,
single-backend write routing, connection registry CRUD, and health aggregation
against in-process fake backends.
