# Configuration

`ucloud-kv-indexer` is configured by **one nested YAML file** that describes the whole
topology. Both binaries read the *same* file:

- **`kvgateway -config config.yaml`** builds its federated backend list from each
  cluster's `backends`.
- **`kvindexer -bootstrap config.yaml -cluster <id>`** seeds its runtime store **once,
  when empty**, scoped to one cluster. After that the **runtime store is authoritative**
  and the console mutates it (the file is never re-read unless you wipe the store).

The local two-cluster example is [`deploy/config.local.yaml`](../deploy/config.local.yaml);
a production multi-cluster example is [`deploy/config.yaml`](../deploy/config.yaml).

---

## The cluster model

A **cluster** is the top-level unit: a `(GPU pool + serving framework + model)` triple,
e.g. *"H20 pool #1 · SGLang · Qwen3"*. It is the dimension the console switches on
(`?cluster=`), the unit a kvindexer is scoped to (`-cluster`), and how the gateway groups
backends. Each cluster owns its engines and models.

```
cluster (local-vllm, framework=vllm)
  ├─ backends:  [http://127.0.0.1:8090]      # kvindexer URL(s) the GATEWAY federates
  ├─ models:    [ qwen3.5-4b → profile ]     # how to tokenize + hash (block_size, seed)
  └─ engines:   [ vllm-local-0 ]             # where to tokenize + which ZMQ stream to SUB
```

## Nested vs flat (both supported)

The **nested** form (preferred) writes `engines:` and `models:` *under* each cluster, with
a cluster-level `framework:`. The loader (`LoadBootstrap` → `flattenNested`) moves them
into flat lists, having them **inherit** the cluster's `cluster_id` + `framework`, and
gives each model a default `hash_profile` of `<framework>-v1-text`. Downstream code and
the normalized SQLite tables only ever see the flat, de-duplicated representation.

The **flat** form (top-level `engines:` / `profiles:` lists, each repeating `cluster_id`
+ `framework`) is still accepted for legacy seeds. A legacy single `cluster:` block is
also merged in. Unknown keys are rejected (the decoder uses `KnownFields(true)`), so a
typo fails loudly instead of silently dropping config.

---

## Schema reference

### `clusters[]` — `BootstrapCluster`

| Key | Type | Notes |
| --- | --- | --- |
| `cluster_id` | string (required) | Unique id, e.g. `local-vllm`. Used by `-cluster` and `?cluster=`. |
| `display_name` | string | Shown in the console header/switcher. |
| `region` | string | Physical-region label (informational; distinct from the federation `_cluster` tag). |
| `environment` | string | `dev` / `prod` … (informational). |
| `enabled` | bool | Default true. |
| `framework` | `vllm` \| `sglang` | **Inherited** by nested engines/models. Drives the normalize adapter + tokenize endpoint choice. |
| `backends` | []string | kvindexer base URL(s) serving this cluster. **Read by the gateway only** (kvindexer ignores it). One → backend id `<cluster>-0`; many → `<cluster>-0/-1/…`. |
| `models` | []model | Nested model profiles (below). |
| `engines` | []engine | Nested engines (below). |

### `models[]` / `profiles[]` — `BootstrapProfile`

| Key | Type | Notes |
| --- | --- | --- |
| `model_id` | string (required) | The served model name (must match what the engine serves and what clients send). |
| `framework` | string | Inherited from cluster if nested. |
| `block_size` | int (required) | **Must equal the engine's KV block size.** vLLM full_attention = 528 for qwen3.5-4b; SGLang = `--page-size` (64 here). Mismatch → request_keys never match. |
| `hash_seed` | string | Namespace salt. Keep stable; `"0"` matches `PYTHONHASHSEED=0`. Only affects the namespace, not engine hashing. |
| `hash_profile` | string | Defaults to `<framework>-v1-text`. Part of the namespace, so vLLM vs SGLang servings of the "same" model never cross-pollute. |
| `tokenizer_endpoint` | string | Where to POST `/tokenize`. Falls back to an engine's `tokenizer_endpoint`. |

The **namespace** a request maps to is `"<model_id>/v1/<hash_profile>/<block_size>"`
(e.g. `qwen3.5-4b/v1/vllm-v1-text/528`). It is what `/index/stats` and `/query-prefix`
report.

### `engines[]` — `BootstrapEngine`

| Key | Type | Notes |
| --- | --- | --- |
| `engine_id` | string (required) | Unique within the cluster. |
| `cluster_id` | string | Inherited if nested. |
| `framework` | string | Inherited if nested. |
| `api_endpoint` | string | Engine OpenAI API base (used to suggest a target + warm in tests). |
| `tokenizer_endpoint` | string | Engine `/tokenize` base. |
| `kv_event_endpoint` | string | ZMQ PUB to **SUBSCRIBE** to, e.g. `tcp://127.0.0.1:5559`. The kvindexer *connects* here; the engine must *bind* it with `tcp://*:5559` (see README "Why engines publish on `tcp://*`"). |
| `replay_endpoint` | string | ZMQ ROUTER for gap replay, e.g. `tcp://127.0.0.1:5560`. (Replay reconnect is a documented gap — see [scaling.md](scaling.md).) |
| `topic` | string | KV-event topic, usually `kv-events`. |
| `served_models` | []string | Models this engine serves; ties engine ↔ profile. |
| `dp_ranks` | int | Data-parallel ranks (per-rank residency tracked separately). |
| `max_num_seqs`, `max_model_len` | int | Informational capacity hints shown in the console. |

### `policies[]` — `BootstrapPolicy`

Policies are **cross-cluster** (scoped by cluster/model/tenant), so they live at the top
level, not nested. The effective policy for a request is the merge of all matching scopes
(most-specific wins); preview it via `/config/effective-policy/preview`.

| Key | Type | Notes |
| --- | --- | --- |
| `policy_id` | string (required) | |
| `scope_cluster_id` / `scope_model_id` / `scope_tenant_id` | string | Empty = matches all. |
| `long_prompt_threshold_tokens` | int | Below this → always accept (ordinary prompt). |
| `hard_long_prompt_threshold_tokens` | int | Above this → 429 (capacity), regardless of hit. |
| `min_hit_ratio_for_long_prompt` | float | A long prompt below this hit ratio → 429 `long_prompt_low_cache_hit`. |
| `event_freshness_ttl_ms` | int | A stream silent longer than this is "stale" → fallback-accept (never low-hit 429). |
| `enabled` | bool | |

---

## Minimal example (one cluster)

```yaml
clusters:
  - cluster_id: local-vllm
    display_name: Local · vLLM · Qwen3.5-4B
    framework: vllm
    backends: [http://127.0.0.1:8090]
    models:
      - model_id: qwen3.5-4b
        block_size: 528
        hash_seed: "0"
        tokenizer_endpoint: http://127.0.0.1:8000
    engines:
      - engine_id: vllm-local-0
        api_endpoint: http://127.0.0.1:8000
        tokenizer_endpoint: http://127.0.0.1:8000
        kv_event_endpoint: tcp://127.0.0.1:5559
        replay_endpoint: tcp://127.0.0.1:5560
        topic: kv-events
        served_models: [qwen3.5-4b]
        dp_ranks: 1
        max_model_len: 8192
policies:
  - policy_id: local-default
    long_prompt_threshold_tokens: 256
    hard_long_prompt_threshold_tokens: 7168
    min_hit_ratio_for_long_prompt: 0.5
    event_freshness_ttl_ms: 5000
    enabled: true
```

## Production layout (many clusters)

See [`deploy/config.yaml`](../deploy/config.yaml) for three clusters (SGLang Qwen3 on H20,
vLLM Qwen2.5 on H20, SGLang Qwen3.6 on H200), each with its own `backends` (the
remote kvindexer URLs), engines, and models, plus cross-cluster policies. The deployment
pattern (inverse topology — gateway holds state, kvindexers are stateless):

- Run **one kvindexer per cluster**, next to that cluster's engines, each
  `-bootstrap config.yaml -cluster <id> -store memory -auth-token <TOKEN>`. It loads only
  its own cluster from the YAML into memory each boot and subscribes only to its local ZMQ
  streams. No local database.
- Run **one kvgateway** with `-sqlite-path connections.db -config config.yaml
  -backend-token <TOKEN>`. On first boot it seeds its connection registry from every
  cluster's `backends` (one row per URL, id `<cluster>-N`) and attaches the token on every
  hop. Thereafter the registry is authoritative and editable via `/admin/connections`. The
  console points only at the gateway.
- To scale a hot cluster, add more kvindexer connections (more rows in the registry, or
  more `backends` entries before first seed) and shard engines/models across them — the
  gateway federation is transparent. See [scaling.md](scaling.md).

---

## Persistence & state ownership

**The kvindexer is stateless by default.** State ownership is split:

| Component | Store | Holds | Flags |
| --- | --- | --- | --- |
| **kvindexer** | `memory` (default) | nothing — loads its 1 cluster from YAML each boot | `-store memory -bootstrap config.yaml -cluster <id>` |
| kvindexer (standalone) | `sqlite` / `file` | the full config, persisted | `-store sqlite -sqlite-path …` |
| **kvgateway** | `sqlite` | the connection registry: `{id, cluster, url, token, enabled}` | `-sqlite-path conns.db -config config.yaml -backend-token <TOKEN>` |

kvindexer `-store` values:

| Store | Flags | When |
| --- | --- | --- |
| **memory** (default) | `-bootstrap config.yaml -cluster <id>` | Stateless per-cluster worker behind the gateway. Loads YAML each boot. |
| **sqlite** | `-sqlite-path path.db` | Standalone kvindexer (no gateway) that must persist console edits. Pure-Go (`modernc.org/sqlite`, Go ≥ 1.25). |
| **file** | `-config data/config.json` | Single JSON snapshot; simplest, good for inspection. |

**Seeding** (`-bootstrap config.yaml`, operator-authored) only applies when the store is
still empty — which, for `memory`, is *every boot*.

The gateway's connection registry uses the same **seed-once** rule: it seeds from
`-config` only when the DB has no rows, then `/admin/connections` is authoritative. Drop
the gateway DB (`make clean`, or delete the `-sqlite-path` file) to re-seed from YAML.

The config has a **version** that bumps on every mutation; hash-affecting profile edits
bump the profile version (and thus the namespace), which the console warns about.

## Gateway connection registry (admin API)

When the gateway runs with `-sqlite-path`, it serves a CRUD surface for the kvindexers it
federates:

| Method | Path | Body / effect |
| --- | --- | --- |
| GET | `/admin/connections` | List connections; tokens redacted to `has_token: bool`. |
| POST | `/admin/connections` | Upsert `{id, cluster, url, token?, enabled}`. An omitted `token` on an existing id preserves the stored secret. |
| DELETE | `/admin/connections/{id}` | Remove a connection. |

Edits take effect immediately (the gateway re-reads its in-memory snapshot after each
write). `enabled: false` keeps a row but drops it from the federated set.

## Security: the gateway ↔ kvindexer hop

That hop crosses the network, so it is authenticated with a **shared bearer token**:
the gateway sends `Authorization: Bearer <TOKEN>` (`-backend-token`), the kvindexer
requires it (`-auth-token`) on every route except `GET /healthz`. In production also
restrict the kvindexer port to the gateway's source IP (ufw) and terminate TLS on the
hop. See the README "Security" section. *(This build is token-only; TLS is the next step.)*
