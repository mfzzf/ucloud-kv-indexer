# ucloud-kv-indexer

A standalone **KV-cache admission + prefix-hit judgment** service for vLLM / SGLang.

It is **not a scheduler** — it answers one question per request: *should we accept this
request, or reject it (429)?* The judgment combines **prompt length** and **prefix
cache-hit rate**, derived from the engine's live KV-cache events. It supports three
inbound protocols — **OpenAI Chat Completions**, **OpenAI Responses**, and **Anthropic
Messages** — and a web console for dynamic configuration.

> **New here? Jump to [Quickstart: from zero](#quickstart-from-zero).** It boots two
> real clusters (vLLM + SGLang) on one GPU and verifies the whole loop in ~3 minutes.

## What it does

For each request:

1. **Normalize** the inbound protocol (chat / responses / messages) into a common form,
   using a **framework-specific adapter** (vLLM and SGLang disagree on how Anthropic
   Messages map to chat — see [docs/tokenization.md](docs/tokenization.md)).
2. **Tokenize** by calling the *target engine's* `/tokenize` endpoint — never locally,
   so the authoritative chat template stays on the engine side.
3. **Compute prefix request-keys** — a deterministic chained hash over `block_size`
   token chunks, namespaced per model-profile version.
4. **Query the residency index** built from the engine's ZMQ KV-cache event stream.
5. **Judge admission** against the effective policy:

   ```
   input < long_prompt_threshold        -> accept (ordinary)
   hard cap exceeded                    -> 429 (capacity)
   events stale / untokenizable / no
     candidates / unsupported features  -> fallback accept (never low-hit 429)
   best_hit_ratio < min_hit_ratio       -> 429 long_prompt_low_cache_hit
   else                                 -> accept (high cache hit)
   ```

The cache-hit signal is only trusted when the event stream is healthy (listener
connected, no sequence gaps). A genuine miss on a healthy stream is judged as a real
miss; an unhealthy/absent stream forces fallback so we never 429 spuriously.

> **The `/v1/*` endpoints JUDGE, they do not proxy.** A long cold-cache prompt gets a
> 429 and is *not* forwarded to the engine. Your router/gateway does the actual
> proxying after this service says "accept". This matters when testing: to warm a
> prefix you must hit the **engine** directly, not the kvindexer (see
> [`deploy/smoke.py`](deploy/smoke.py)).

## Architecture

### Two planes

```
                            ┌──────────────────────────────┐
  browser ── http ─────────▶│  Next console :3000          │   same-origin
                            │  /api/kvi/* proxy            │   browser only needs :3000
                            └──────────────┬───────────────┘
                            ┌──────────────────────────────┐
                            │  kvgateway  :8095            │   federation + config authority
                            │  (fan-out reads, proxy writes)│   SQLite connection registry
                            │   ── never touches ZMQ ──     │   (which kvindexers + tokens)
                            └───────┬──────────────┬────────┘
              ?cluster= +Bearer tok │              │ ?cluster= +Bearer tok
                   ┌────────────────▼───┐    ┌─────▼──────────────┐
                   │ kvindexer :8090    │    │ kvindexer :8091    │   one per CLUSTER,
                   │ cluster=local-vllm │    │ cluster=local-sglang│  next to its engines
                   │ Mongo config/events │    │ Mongo config/events │ loads its own cluster
                   │ local-vllm.yaml     │    │ local-sglang.yaml   │ seed when Mongo empty
                   └─────────┬──────────┘    └─────────┬──────────┘
              ZMQ SUB (local)│ tcp://127.0.0.1:5559    │ tcp://127.0.0.1:5557
                   ┌─────────▼──────────┐    ┌─────────▼──────────┐
                   │ vLLM  :8000        │    │ SGLang :30000      │
                   │ qwen3.5-4b         │    │ qwen3-0.6b         │
                   └────────────────────┘    └────────────────────┘
```

- A **cluster** is a `(GPU pool + serving framework + model)` unit, e.g. *"H20 pool #1,
  SGLang, Qwen3"*. It is the top-level dimension everywhere (config, console, gateway).
- **kvindexer** — one process **per cluster**, co-located with that cluster's engines so
  the ZMQ KV-event stream stays node-local. It subscribes to events, builds the in-memory
  residency index, persists decoded prefix-cache events (`token_ids`, request keys, engine
  hashes) to MongoDB, and judges admission. Its config store can be `memory`, `file`,
  `sqlite`, or `mongo`; local dev uses `-store mongo` so frontend policy edits survive
  restart.
- **kvgateway** — the federation layer **and** the connection authority. It owns a
  **SQLite connection registry** (which kvindexers exist, their URLs, and their bearer
  tokens), seeded once from one or more YAML files and editable live via
  `/admin/connections`. It fans GET
  lists out to every cluster's kvindexer (tagging each row with `_cluster`/`_backend`),
  proxies writes/queries to one backend selected by `?cluster=`, and **attaches the bearer
  token to every call**. It does not subscribe to ZMQ — see [docs/scaling.md](docs/scaling.md).
- **web/** — a Next.js 16 console that talks *only* to the gateway and selects a cluster
  with `?cluster=`.

> **Topology note.** State is centralized in the **gateway** (the connection registry),
> while kvindexers sit next to the GPUs. This suits a deployment where
> the gateway is central and kvindexers sit remotely beside their clusters, reached over
> the (firewalled) network with a shared bearer token.

```
internal/
  types/        shared request types
  config/       dynamic config store (clusters/engines/profiles/policies); versioning +
                audit + effective-policy merge; persisters: memory | file JSON | SQLite | MongoDB
  normalize/    protocol -> RouteRequest; framework adapters (AdapterFor("vllm"|"sglang"))
  tokenizer/    HTTP client to the engine /tokenize endpoint (never tokenizes locally)
  residency/    dual-key prefix index (request_key + engine_key bridge) + manager
  kvevents/     msgpack decoder + pure-Go ZMQ listener (go-zeromq/zmq4) + bounded apply queue
  admission/    the length + cache-hit-rate judgment
  gateway/      multi-cluster HTTP federation + SQLite connection registry (cmd/kvgateway)
  httpapi/      service wiring + HTTP handlers + bearer auth + CORS (cmd/kvindexer)
cmd/kvindexer/  per-cluster admission service
cmd/kvgateway/  federation gateway + connection registry
web/            Next.js 16 management console
deploy/         local-*.yaml (per-cluster topology) + serve-*.sh (engines) + smoke.py (e2e)
docs/           kv-events.md · tokenization.md · configuration.md · scaling.md
```

### Dual-key residency index

- **`request_key`** — `FNV-64a` chained over `block_size`-token chunks from a profile
  namespace seed. Computed identically (a) at query time from the tokenizer output and
  (b) at ingest time from a `BlockStored` event's `token_ids`. This is what request-time
  prefix scoring matches on. (We do **not** reproduce vLLM's pickle+sha256 hash; we keep
  our own deterministic key and bridge to engine hashes.)
- **`engine_key`** — the opaque `uint64` hash carried in vLLM/SGLang KV events, used to
  process `BlockRemoved` (which only carries engine hashes) and resolve parent chaining.

On `BlockStored` we learn both and record an `engine_key -> request_key` bridge
(tail-aligned when counts differ, e.g. mamba-align null-block skipping). On
`BlockRemoved` we look up the bridge to drop the right residency.

---

## Quickstart: from zero

This boots the **two-cluster demo** on a single GPU: vLLM `qwen3.5-4b` and SGLang
`qwen3-0.6b`, each with a kvindexer, federated by one gateway, with the console on top.

### 0. Prerequisites

| Need | This box has | Notes |
| --- | --- | --- |
| **Go ≥ 1.25** | `go1.25` via toolchain | `modernc.org/sqlite` (pure-Go, no cgo) needs 1.25; `go build` auto-fetches the toolchain. |
| **Node 20** | `/home/ubuntu/.local/node20/bin` | Next 16 needs Node 20; the system Node 18 will fail. The Makefile puts Node 20 on `PATH`. |
| **A GPU + venvs** | RTX 4090 24 GB; `.venv-vllm`, `.venv` | vLLM in `.venv-vllm`, SGLang in `.venv`. Models in `models/` and the HF cache. |
| **A firewall** | `ufw` active, deny-incoming | **Required on a public IP** — see [Security](#security). |

> **Security first.** This machine has a public IP. Before anything else, make sure the
> host firewall is closed (only SSH open). See [Security](#security). The control plane
> binds `127.0.0.1`; engine ZMQ PUB sockets must bind `0.0.0.0` (a ZMQ quirk, below) and
> are kept private **by the firewall**.

### 1. Start the inference engines

```bash
cd ucloud-kv-indexer
make inference          # starts deploy/serve-vllm.sh (:8000) + deploy/serve-sglang.sh (:30000)
make inference-status   # poll until both report HTTP 200 (~1-2 min to load weights)
```

`make inference` is a **separate lifecycle** — `make down` never stops it. The serve
scripts bind HTTP/tokenizer to `127.0.0.1` and publish KV events on `tcp://*:PORT`
(see [the ZMQ bind note](#why-engines-publish-on-tcp-and-not-127001)).

### 2. Build and start the control plane

```bash
make build              # Go binaries + web production build (Node 20)
make up                 # backend-vllm (:8090) + backend-sglang (:8091) + gateway (:8095) + frontend (:3000)
make status             # show listening ports + per-cluster health
```

Open the console at **http://127.0.0.1:3000** (loopback only — tunnel over SSH to view
it from your laptop: `ssh -L 3000:127.0.0.1:3000 <host>`). Browser API calls go through
the frontend's `/api/kvi/*` proxy, so a frontend-only tunnel is enough for the console.

### 3. Verify the whole loop

```bash
make smoke              # tokenize -> warm engine -> index -> admit, for BOTH clusters x 3 protocols
```

Expected tail:

```
== local-vllm (qwen3.5-4b) ==
  [local-vllm   ] openai.chat         PASS  toks=1478 bs=528 matched=1056 gpu=1056 | admit=accept hit_ratio=0.714
  [local-vllm   ] openai.responses    PASS  ...
  [local-vllm   ] anthropic.messages  PASS  ...
== local-sglang (qwen3-0.6b) ==
  [local-sglang ] openai.chat         PASS  toks=1476 bs=64 matched=1472 gpu=1472 | admit=accept hit_ratio=0.997
  ...
ALL PASS — both frameworks, all three protocols: tokenize→warm→index→admit verified
```

This proves: framework-correct tokenization (3 protocols × 2 engines), accurate ZMQ
residency indexing, and the admission decision flipping **reject→accept** once a prefix
is resident.

### Day-to-day

```bash
make status     # ports + cluster health + gateway view
make logs       # tail all control-plane logs
make restart    # down + up (re-uses SQLite stores; config NOT re-seeded)
make down       # stop control plane (engines keep running)
make clean      # down + delete run/ (drops SQLite stores -> next `up` re-seeds from config)
make stop-inference   # stop the engines too
```

---

## Configuration

Local dev uses **one YAML file per cluster**:
[`deploy/local-vllm.yaml`](deploy/local-vllm.yaml) and
[`deploy/local-sglang.yaml`](deploy/local-sglang.yaml). Each cluster owns its engines +
models; the loader flattens this into normalized clusters/engines/profiles tables. Full
reference: **[docs/configuration.md](docs/configuration.md)**.

```yaml
clusters:
  - cluster_id: local-vllm
    display_name: Local · vLLM · Qwen3.5-4B
    framework: vllm                      # engines/models inherit this
    backends: [http://127.0.0.1:8090]    # the kvindexer URL(s) the gateway federates
    models:
      - model_id: qwen3.5-4b
        block_size: 528                  # vLLM full_attention block size (verified)
        tokenizer_endpoint: http://127.0.0.1:8000
    engines:
      - engine_id: vllm-local-0
        api_endpoint: http://127.0.0.1:8000
        kv_event_endpoint: tcp://127.0.0.1:5559   # kvindexer SUBs here (connect)
        replay_endpoint:  tcp://127.0.0.1:5560
        served_models: [qwen3.5-4b]
policies:
  - policy_id: local-default
    long_prompt_threshold_tokens: 256
    min_hit_ratio_for_long_prompt: 0.5
```

- Each kvindexer seeds from its own YAML **scoped to its `-cluster`**, once, when its
  MongoDB config snapshot is empty. After that **MongoDB is authoritative** and the
  console mutates it. Re-seed with `make clean-mongo` (drops the Mongo volume).
- `block_size` **must match the engine**: vLLM full_attention = 528 for qwen3.5-4b;
  SGLang `--page-size` = 64. A mismatch produces request_keys that never match.
- `hash_profile` defaults to `<framework>-v1-text` and is part of the namespace, so a
  vLLM and an SGLang serving of the "same" model never cross-pollute the index.

### Persistence

Local dev runs MongoDB in Docker (`make mongo`) and starts both kvindexers with
`-store mongo`. Policies/config snapshots are mirrored into MongoDB collections
(`policies`, `clusters`, `engines`, `profiles`, `audit`, `config_snapshots`), and decoded
ZMQ KV events are appended to `prefix_cache_events` with `token_ids` and `request_keys`.
Other store modes remain available:

- **memory** (default) — stateless; config comes from `-bootstrap` YAML each boot. Use this
  for a remote per-cluster kvindexer fronted by the gateway.
- **mongo** — `-mongo-uri ... -mongo-db ...`, persistent dynamic config plus
  prefix-cache event sink. This is the local dev default.
- **sqlite** — `-sqlite-path path.db`, pure-Go, a real local store (config mutations
  survive restart). Use for a standalone kvindexer with no gateway.
- **file** — `-config data/config.json`, a single JSON snapshot.

The **gateway** owns the connection registry in **SQLite** (`-sqlite-path`): the list of
kvindexers (`{id, cluster, url, token, enabled}`), seeded once from YAML
(`-config` or comma-separated `-configs`) with the shared `-backend-token`, then
authoritative and editable live via
`/admin/connections` (`GET` / `POST` / `DELETE /admin/connections/{id}`).

---

## Security

**This box has a public IP and the firewall is the boundary.** The setup:

- Control plane (`kvindexer` :8090/:8091, `kvgateway` :8095, frontend :3000) binds
  **`127.0.0.1`** (Makefile `BIND := 127.0.0.1`).
- vLLM **HTTP/tokenizer** (`:8000`) binds `0.0.0.0` for external testing and is allowed
  through `ufw`; the local script has no auth, so keep it on a trusted network.
- SGLang **HTTP/tokenizer** (`:30000`) still binds `127.0.0.1`.
- Engine **ZMQ PUB** (`:5557`, `:5559`) binds `0.0.0.0` — unavoidable (see below) — and
  is kept private by the firewall.
- **`ufw`**: `default deny incoming`, `22/tcp` (SSH) and `8000/tcp` (vLLM API) allowed.
  Enable once:

  ```bash
  sudo ufw allow 22/tcp
  sudo ufw allow 8000/tcp
  sudo ufw default deny incoming
  sudo ufw --force enable
  sudo ufw status verbose
  ```

  Verify vLLM is reachable and the other local-only ports are not:

  ```bash
  HOST_IP=$(ip -4 addr show | grep -oP 'inet \K10[0-9.]+' | head -1)
  for p in 8000 30000 8090 8095 3000 5559; do
    curl -s --max-time 2 -o /dev/null -w "$p: %{http_code}\n" http://$HOST_IP:$p/ || echo "$p: blocked"
  done   # 8000 should answer; the rest should be blocked/000.
  ```

  Only block-hashes and token-ids ever traverse the ZMQ stream — no prompt text — but it
  stays firewalled regardless.

### Gateway ↔ kvindexer authentication (bearer token)

The gateway reaches each kvindexer over the network, so that hop is authenticated with a
**shared bearer token**:

- kvindexer: `-auth-token <TOKEN>` (or `KVINDEXER_AUTH_TOKEN`). When set, every request
  except `GET /healthz` (liveness) must carry `Authorization: Bearer <TOKEN>`; a
  constant-time compare rejects mismatches with 401. Empty = no auth (loopback dev only).
- gateway: `-backend-token <TOKEN>` (or `KVGATEWAY_BACKEND_TOKEN`) — seeded onto each
  connection and attached to every forwarded request + health probe. Per-connection
  tokens can also be set via `/admin/connections`; the admin list **redacts** them
  (`has_token: true`).

In production also:
1. **Restrict the kvindexer port to the gateway's source IP** with ufw
   (`ufw allow from <gateway-ip> to any port 8090 proto tcp`) — the token is
   authn, the firewall is the network boundary.
2. **Terminate TLS** on the gateway↔kvindexer hop (reverse proxy or a future
   `-tls-cert/-tls-key`). The bearer token is sent in clear over HTTP, so it relies on a
   private/VPN network or TLS to not be sniffable. *(TLS is the documented next step; this
   build ships token-only.)*

### Why engines publish on `tcp://*` and not `127.0.0.1`

Both vLLM (`vllm/distributed/kv_events.py`) and SGLang
(`sglang/srt/disaggregation/kv_events.py`) use the same heuristic for the KV-event PUB
socket: **`bind()` only if the endpoint contains `*` / `::` / `ipc://` / `inproc://`,
otherwise `connect()`.** So `endpoint=tcp://127.0.0.1:5559` makes the engine *connect* a
PUB that never listens — the kvindexer SUB then loops on connection-refused
(`connected=false`, 0 events). You'll see the **replay** ROUTER bind (`:5558`/`:5560`)
while the PUB port stays absent — the tell-tale symptom. So the publisher must bind with
`tcp://*:PORT`; the kvindexer connects with `tcp://127.0.0.1:PORT`. The `*` exposure is
contained by the firewall. (The replay endpoint always `bind()`s, so it can stay
`127.0.0.1`.) The PUB socket also **binds lazily** — it only appears after the engine has
something to publish, i.e. after a prompt fills ≥ one full block (528 for vLLM, 64 for
SGLang); short prompts emit nothing even when wired correctly.

---

## HTTP API (selected)

All of these are also reachable via the gateway with `?cluster=<id>` (reads fan out;
writes/queries target one backend).

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/v1/chat/completions` | admission judgment (OpenAI chat) — **judges, doesn't proxy** |
| POST | `/v1/responses` | admission judgment (OpenAI responses) |
| POST | `/v1/messages` | admission judgment (Anthropic messages) |
| POST | `/route` | admission judgment (alias of chat) |
| POST | `/query-prefix` | Mooncake/Dynamo-style per-instance prefix hits |
| POST | `/tokenize/preview` | tokens + request-keys for a request (per framework adapter) |
| POST | `/config/effective-policy/preview` | resolve merged policy |
| GET/POST/PATCH/DELETE | `/clusters`, `/engines`, `/model-profiles`, `/policies` | config CRUD (`DELETE` currently applies to policies via `/policies/{id}` and admin connections via the gateway) |
| GET | `/event-streams`, `/kv-events/recent`, `/decisions`, `/config/audit-log`, `/index/stats` | observability |
| GET (SSE) | `/kv-events/stream` | live decoded KV-event stream for one selected backend/cluster |
| GET | `/clusters-health` *(gateway)* | per-cluster backend health |

A 429 returns a structured reason:

```json
{ "error": { "type": "long_prompt_low_cache_hit", "input_tokens": 24576,
  "best_hit_tokens": 2048, "hit_ratio": 0.083, "min_required_hit_ratio": 0.5 } }
```

## Console pages

Cluster Overview · Engine Registry (hot enable/drain) · Model Profiles (version-bump
warning on hash-affecting edits) · Routing Policy (+ effective-policy preview) · KV Event
Streams · Prefix Query Simulator (run the full pipeline across all 3 protocols) · Live
Decisions · Config Audit. A cluster switcher in the header scopes every page; `all` fans
out across clusters.

## Tests

```bash
make test          # go test ./...   (unit tests, no GPU needed)
make smoke         # end-to-end against the live engines (needs `make inference` + `make up`)
```

- `internal/residency` — request-key determinism, namespace isolation, contiguous-prefix
  matching, gap breaks, removal, AllBlocksCleared, mamba tail-align bridge, tier breakdown.
- `internal/kvevents` — msgpack decode against a **real captured golden batch**, uint64
  coercion, short-array tolerance.
- `internal/admission` — the full 429 policy matrix (short / hard-cap / low-hit /
  high-hit / stale / untokenized / hash-unsupported / disabled).
- `internal/config` — effective-policy merge order, scope filtering, version bump,
  snapshot round-trip, SQLite persister round-trip, nested-config flattening.
- `internal/normalize` — all three protocols + both framework adapters + multimodal detection.
- `internal/gateway` — fan-out merge + cluster/backend selection.

## Further reading

- [docs/configuration.md](docs/configuration.md) — nested YAML bootstraps, clusters,
  block sizes, persistence, multi-cluster production layout.
- [docs/kv-events.md](docs/kv-events.md) — the ZMQ KV-event wire format (vLLM + SGLang).
- [docs/tokenization.md](docs/tokenization.md) — how each protocol becomes engine
  `/tokenize` input, and the vLLM-vs-SGLang adapter differences.
- [docs/scaling.md](docs/scaling.md) — many engine instances, high event volume, sharding.
