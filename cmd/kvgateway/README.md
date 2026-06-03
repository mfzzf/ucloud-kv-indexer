# kvgateway — multi-region aggregation control plane

`kvgateway` federates several per-region `kvindexer` backends behind one HTTP
endpoint, so the web console can manage many regions/clusters from a single base
URL and switch between them with a `?region=` selector.

## Why a gateway (not the frontend fanning out)

Each `kvindexer` sits next to its inference engines because it subscribes to the
engines' **ZMQ KV-cache event stream** — that stream is high-frequency and must
stay local (you don't pull ZMQ across a WAN). So the natural topology is **one
kvindexer per region/deployment**. The browser can't and shouldn't dial every
regional backend directly (network reachability + CORS per backend), so this
gateway does the fan-out, merge, and write-routing centrally.

```
              ┌───────────── web console (one base URL, ?region= selector)
              ▼
        kvgateway :8095
        ├── GET  fan-out + merge (region-tagged)   → all selected backends
        ├── POST/PATCH write proxy (one backend)   → region/backend-targeted
        └── GET  /regions (live health probe)
              │
      ┌───────┼─────────────────┐
      ▼       ▼                 ▼
 kvindexer  kvindexer        kvindexer
 region A   region A'        region B      (each next to its engines + ZMQ stream)
```

## Endpoints

| Kind | Routes | Behavior |
| --- | --- | --- |
| Fan-out GET | `/clusters` `/engines` `/model-profiles` `/policies` `/event-streams` `/index/stats` `/decisions` `/config/audit-log` | Query every selected backend, tag each array element with `_region` + `_backend`, merge. `/decisions` sorted by timestamp, `/config/audit-log` by version. A dead backend is logged and skipped, never blanks the response. |
| Aggregate | `/config/versions` | Per-backend `{region, backend, config_version}` array. |
| Write proxy | `POST/PATCH` on clusters, engines (`register`/`unregister`/`{id}`), `model-profiles`, `policies`; `DELETE /policies/{id}` | Proxied to **exactly one** backend. Selector must resolve to one — ambiguous → `400`, no match → `404`. Response carries `X-KVI-Backend` / `X-KVI-Region`. |
| Admission / query proxy | `POST` `/route` `/v1/chat/completions` `/v1/responses` `/v1/messages` `/query-prefix` `/tokenize/preview` `/config/effective-policy/preview` | Proxied to one selected backend. |
| Gateway-native | `GET /regions` `GET /healthz` | `/regions` groups backends by region and live-probes each `/healthz`. |

### Selectors

- `?region=<id>` — restrict to that region (omit, or `region=all`, = every region).
- `?backend=<id>` — restrict to one exact backend (a region may hold several).

Reads default to fan-out across all. Writes require an unambiguous target.

## Running

```bash
go build -o /tmp/kvgateway ./cmd/kvgateway

# backends via inline JSON, a file, or KVGATEWAY_BACKENDS env:
/tmp/kvgateway -addr :8095 -backends '[
  {"id":"sh-0","region":"cn-shanghai","url":"http://10.0.0.1:8090"},
  {"id":"sh-1","region":"cn-shanghai","url":"http://10.0.0.2:8090"},
  {"id":"gz-0","region":"cn-guangzhou","url":"http://10.1.0.1:8090"}
]'
# or: -backends-file backends.json
```

Point the web console at the gateway:

```bash
echo 'NEXT_PUBLIC_API_BASE=http://127.0.0.1:8095' > web/.env.local
```

The console auto-detects federation by calling `GET /regions`. Against a plain
single `kvindexer` (no `/regions`), the region switcher hides itself and the app
behaves exactly as before — fully backward compatible.

## Tests

`go test ./internal/gateway/` — fan-out tagging, region filter, single-backend
write routing (ambiguity → 400, unknown → 404), `/regions` health, version
aggregate, all against in-process fake backends.
