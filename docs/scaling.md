# Scaling KV-event ingestion: many engines, high volume

> Answers the recurring question: **"gateway 能否监听好多实例的 vLLM/SGLang ZMQ 事件，
> 如果量大了怎么办？"** ("can the gateway listen to many instances' ZMQ events, and what
> do we do when the volume gets high?").
>
> Short answer: the **gateway does not listen to ZMQ at all** — the **kvindexer**
> does, one process per cluster sitting next to its engines. One kvindexer happily
> handles many engine instances in a cluster (one SUB socket + one goroutine each).
> When a single cluster's event volume outgrows one process, you **shard the engines
> across multiple kvindexer processes** and let the gateway federate them
> transparently. The bottleneck is single-threaded index apply, not the gateway.
>
> Companion docs: [kv-events.md](./kv-events.md) (wire format, decode), and the
> README (overall architecture).
>
> Source references are relative to the repo root and use `file.go:line`.

---

## 1. Who subscribes to ZMQ (correcting the misconception)

It is a common misconception that the **gateway** subscribes to the engine ZMQ
streams. It does not. The gateway is a pure **HTTP federation control plane**:

- `internal/gateway/gateway.go` only ever speaks HTTP to a fixed list of kvindexer
  `Backend`s (`gateway.go:35-58`). GET list endpoints fan out and merge
  (`fanoutList`, `gateway.go:163-198`); writes/admission/query are proxied to exactly
  one backend (`proxyOne`, `gateway.go:267-300`). There is **no `zmq4` import** in
  `internal/gateway` — `grep -rln zmq4 internal/` returns only
  `internal/kvevents/listener.go`.

The component that subscribes to ZMQ is the **kvindexer**:

- `internal/kvevents/listener.go` is the only file that imports
  `github.com/go-zeromq/zmq4` and opens a SUB socket (`listener.go:12`, `:134`).
- `internal/httpapi/service.go` owns the listeners and starts them
  (`SyncListeners`, `service.go:63-95`).
- `cmd/kvindexer/main.go:73` calls `svc.SyncListeners()` at boot.

**Topology.** One kvindexer runs **per cluster**, co-located with that cluster's
engines, so the ZMQ KV-event stream stays **node-local / cluster-local** (high
bandwidth, low latency, never crosses the gateway). The gateway sits in front of one
or more kvindexer backends per cluster and is selected by the console with
`?cluster=`. The gateway reads one or more bootstrap YAML files via `-config` /
`-configs` for each cluster's `backends:` list, while each kvindexer reads its own
cluster bootstrap with `-bootstrap … -cluster` to seed only its engines.

```
            ┌─────────────┐         HTTP (fan-out reads / proxied writes)
 console ──▶│  kvgateway  │────────────────────────────────────────────┐
            └─────────────┘                                             │
                  │ HTTP                                                 │ HTTP
                  ▼                                                      ▼
        ┌───────────────────┐  cluster h20-1            ┌───────────────────┐ cluster h20-2
        │  kvindexer (h20-1)│                           │  kvindexer (h20-2)│
        └───────────────────┘                           └───────────────────┘
           ▲   ▲   ▲   ZMQ SUB (node-local, per engine)      ▲   ▲
        ┌──┘ ┌─┘ ┌─┘                                       ┌──┘ ┌─┘
      eng0  eng1 eng2 …                                  eng0  eng1 …
```

So the gateway already "listens to many instances" only in the sense that it
*aggregates* the residency views of many kvindexers. The actual ZMQ subscription
fan-out happens inside each kvindexer.

---

## 2. A single kvindexer: many engine instances

### One SUB socket + one goroutine per engine

`SyncListeners` reconciles the running listener set against the configured engines
(`service.go:63-95`):

- It walks `Store.ListEngines()`, keeps every engine that has a non-empty
  `KVEventEndpoint`, and for each one not already running it constructs a
  `kvevents.NewListener(...)` and calls `l.Start(s.ctx)`.
- The map is `listeners map[string]*kvevents.Listener` keyed by `engineID`
  (`service.go:26`) — **exactly one listener per engine instance**.
- Engines removed from config get `l.Stop()` and are deleted, so the set stays in
  sync with config edits made through the console.

Each `Listener.Start` launches **one goroutine** running `run` →
`subscribeLoop` (`listener.go:80-84`, `:105-161`):

- `subscribeLoop` creates **one ZMQ SUB socket** (`zmq4.NewSub`, `listener.go:134`),
  dials the engine's `KVEventEndpoint` (`:136`), subscribes to all topics with an
  empty filter (`:141`), then loops on `sub.Recv()` (`:155`).
- So a kvindexer with *N* engines has *N* SUB sockets and *N* goroutines, fully
  independent. There is no shared recv loop and no per-engine ordering coupling.

This scales fine to **dozens of engines per cluster**: goroutines are cheap, and a
SUB socket that is idle costs almost nothing. The cost that matters is aggregate
*event throughput* (next section), not the number of sockets.

### Sequencing & health, per engine

Each listener tracks per-stream health (`StreamHealth`, `listener.go:27-39`):
`Connected`, `LastSeq`, `EventsTotal`, `GapsTotal`, `SkippedTotal`, `DecodeErrors`.
`trackSeq` (`listener.go:186-194`) increments `GapsTotal` whenever the 8-byte
big-endian seq jumps by more than 1. These are surfaced at `/event-streams` and
merged by the gateway's `fanoutList("/event-streams")` (`gateway.go:344`).

`StreamFreshForEngines` (`service.go:117-131`) is the admission safety valve: the
residency view is trusted ("fresh") only if at least one serving engine's listener is
`Connected` with `GapsTotal == 0`. A disconnected or gapped stream makes the index
*untrusted*, so a cache MISS falls back to "accept" instead of issuing a 429. (See
§5 for why this is correct-but-pessimistic given there is no replay.)

---

## 3. What happens when volume is high

The hot path is **two goroutines per engine**: a recv loop that only drains the socket,
and an apply loop that does the expensive decode + index write, decoupled by a bounded
queue (added for backpressure; see below).

```
recv goroutine:  sub.Recv()                 listener.go (subscribeLoop)
                   └─ cloneFrames(frames)    copy (socket reuses buffers)
                   └─ queue <- frames        bounded chan, cap=applyQueueSize (8192)

apply goroutine: <-queue                     listener.go (applyLoop)
                   └─ handleFrames(frames)   decode seq + msgpack.Unmarshal
                        └─ DecodeBatch(...)   decode.go  []any → Batch
                             └─ ingest(...)   apply each event to the index
                                  └─ ix.StoreEvent / RemoveEvent / ClearEngine   index.go
```

### Per-event cost

- **Decode.** Each ZMQ message is one msgpack `Unmarshal` into `[]any` plus a
  per-event positional walk (`decode.go:142-225`). Reflection-based msgpack into
  `[]any` is the most expensive single step; it allocates one boxed `interface{}` per
  field. This dominates CPU at high rates and runs in the apply goroutine.
- **request_key recompute.** For each `BlockStored` we FNV-64a-chain over the
  `token_ids` in `block_size` chunks (`requestKeysForEvent` → `RequestKeysFromTokens`).
  FNV is cheap (a few ns per block), so this is not the bottleneck unless blocks are tiny
  (e.g. SGLang hybrid `block_size: 1`, which produces one hash *per token* — heavier).
- **Index apply** takes `ix.mu` (a single `sync.Mutex`) for the duration of the batch
  (`StoreEvent`/`RemoveEvent`/`ClearEngine`, `index.go:142-242`). All engines writing
  into the **same namespace (model)** serialize on this one lock. Within a namespace,
  ingest does **not** parallelize across engines.

### Backpressure: a bounded recv→apply queue (implemented)

The recv loop and the apply loop are decoupled by a **bounded channel**
(`applyQueueSize = 8192`, `listener.go`). This is the burst buffer the raw zmq4 SUB
socket lacks — the pure-Go socket only buffers ~10 frames internally
(`zmq4/msgio.go: qrsize = 10`) before TCP/HWM backpressure, far too shallow for a prefill
burst. With the queue:

- **A burst is absorbed in the queue** (up to 8192 batches) while the apply goroutine
  catches up; the recv loop keeps draining the socket so ZMQ HWM drops are pushed out
  much further.
- **Sustained overrun is bounded, not unbounded.** When the queue fills, the recv loop
  **blocks** on `queue <- frames` → natural backpressure → eventually ZMQ SUB HWM →
  a **sequence gap** we detect (`GapsTotal++`). So the failure mode under sustained
  overload is *flagged staleness*, never OOM and never silent wrong-accepts (a gap forces
  admission fallback — §1, §5).
- **The queue is observable.** `StreamHealth.QueueDepth` / `QueueCap` are exported on
  `/event-streams`; a depth persistently near cap is the operational signal that this
  engine's event rate exceeds one apply goroutine — time to shard (§4). The web console
  surfaces this.

The queue gives headroom; it does **not** raise single-goroutine apply throughput. Once
the *average* (not peak) rate exceeds what one apply goroutine sustains, the queue stays
full and gaps resume — that is the real ceiling, addressed by sharding.

### The three concrete bottlenecks, in order

1. **msgpack decode CPU** — one apply goroutine per engine; a very chatty engine can
   saturate its own goroutine even with the queue absorbing bursts.
2. **The per-namespace index mutex** (`index.go`) — all engines serving the same
   model contend on one lock for both ingest *and* queries (`Query` takes `RLock`).
   A hot model with many engines is the realistic contention point.
3. **Queue-full → ZMQ HWM drops → gaps** — the symptom that *average* overload (not just
   a transient burst) has been reached.

### Mitigations within one process

- **The bounded queue (above)** already absorbs bursts; tune `applyQueueSize` for more
  burst headroom (costs memory: each slot is a frame-slice, dominated by the token_ids).
- **Increase SUB receive HWM** so even queue-full overflow buffers in ZMQ a bit longer.
- **Shard by namespace already exists**: separate models are separate `Index` objects
  with separate locks (`manager.go:11-46`), so multiple models in one cluster do not
  contend.
- **Reduce decode cost** for tiny-block engines (SGLang hybrid `block_size: 1`) — the
  worst case for per-event work; prefer larger paged block sizes where the model allows.
- When a single process can't keep up on *average*, **scale out** (§4).

---

## 4. Scaling out a hot cluster

Because residency is held in memory and ingest is single-locked per namespace, the
clean way to scale a hot cluster is to **run more than one kvindexer in that cluster,
each owning a disjoint subset of engines (or namespaces), and let the gateway
federate them**. The gateway is already built for this: a cluster's `backends:` list
can hold **multiple kvindexer URLs**.

### Why this is transparent to the console

The gateway's fan-out merges per-backend results and tags each row with `_backend` /
`_cluster` (`fanoutList`, `gateway.go:180-184`). The console picks a cluster with
`?cluster=` and never sees how many backends back it. Adding a shard is a config edit
(append a URL to `backends:`), not a code change.

Two natural sharding axes:

#### (a) Shard by **namespace (model)** — most natural

The index is already per-namespace (`Manager.indexes`, `manager.go:11-16`), and the
namespace derives from the model profile (`Namespace()`, `model.go:72-74`;
`ResolveIngest`, `service.go:51-59`). So putting different models on different
kvindexer processes gives each its own lock domain *and* its own RAM budget with zero
correctness change — a query for model X only ever lands on the shard that owns X.

#### (b) Shard by **engine subset** within one model

When a *single* model is hot across many engines, split those engines across two
kvindexer processes. Residency for a given prefix is naturally *partitioned by engine*
(the `resident` key includes `engineID`, `index.go:90-94`), and a prefix query returns
a per-instance breakdown (`Query` → `Instances`, `index.go:247-343`). The gateway
merges the per-instance maps from both shards, so the console sees the full instance
list. **Caveat:** the admission/query endpoints use `proxyOne` (`gateway.go:267`), i.e.
a query is answered by *one* backend. Splitting one model's engines across two shards
means a single query only sees the engines on the shard it hits — acceptable for
"is this prefix hot *somewhere on this shard*" but not for a global cross-shard hit
count. **Prefer axis (a) (shard by model) unless a single model genuinely exceeds one
process.** This split-one-model-across-shards case is a known limitation, listed in §6.

### Config sketch (gateway federating two shards of one cluster)

In that cluster's bootstrap YAML, give the hot cluster two backends and run two
kvindexers, each seeded with a disjoint engine subset. The gateway picks up both URLs:

```yaml
clusters:
  - cluster_id: h20-1
    display_name: H20 Pool #1 — SGLang Qwen3 (sharded)
    framework: sglang
    enabled: true
    backends:                       # gateway federates BOTH kvindexer shards
      - http://10.0.0.1:8090        # shard A: engines 0..3
      - http://10.0.0.1:8091        # shard B: engines 4..7
    models:
      - model_id: qwen3
        block_size: 1
        hash_seed: "0"
        tokenizer_endpoint: http://10.0.0.1:30000
    engines:
      - { engine_id: sglang-h20-1-0, kv_event_endpoint: tcp://10.0.0.1:5557, replay_endpoint: tcp://10.0.0.1:5558, topic: kv-events, served_models: [qwen3] }
      - { engine_id: sglang-h20-1-1, kv_event_endpoint: tcp://10.0.0.1:5559, replay_endpoint: tcp://10.0.0.1:5560, topic: kv-events, served_models: [qwen3] }
      # … engines 4..7 omitted for brevity …
```

Run the two shards (each owns a subset of the cluster's engines). The cleanest split
is **by model** (each kvindexer seeds only the engines for the models it owns); when
splitting one model, give each shard its own SQLite store and register only its
engine subset into it:

```sh
# Shard A — owns engines 0..3 of cluster h20-1
kvindexer -addr :8090 -bootstrap deploy/h20-1-a.yaml -cluster h20-1 \
          -store sqlite -sqlite-path data/h20-1-a.db

# Shard B — owns engines 4..7 of cluster h20-1
kvindexer -addr :8091 -bootstrap deploy/h20-1-b.yaml -cluster h20-1 \
          -store sqlite -sqlite-path data/h20-1-b.db

# Gateway federates both via clusters[].backends
kvgateway -addr :8095 -configs deploy/h20-1-a.yaml,deploy/h20-1-b.yaml
```

> Note: `-cluster` today seeds *all* of that cluster's engines into each process
> (`ApplyBootstrapForCluster`, used at `main.go:57`). To make a shard own only a
> *subset*, either (i) prune the non-owned engines from that shard's store after seed
> (console "unregister", which calls `SyncListeners` to stop their listeners), or
> (ii) maintain a per-shard config file. There is no built-in `-engines=subset` flag —
> see §6.

Because the gateway fan-out is additive and each kvindexer's ZMQ stream stays
node-local, **adding a shard never changes the wire protocol, the console, or the
engines** — it is purely a horizontal-capacity knob.

---

## 5. Backpressure, gaps, and recovery

| Mechanism | Status in code | Reference |
|---|---|---|
| Per-stream `last_seq` tracking | ✅ tracked | `listener.go:186-194` |
| Gap detection (seq jump > 1) | ✅ counts `GapsTotal` | `listener.go:188` |
| Reconnect on socket error | ✅ exp. backoff 0.5s→5s | `run`, `listener.go:105-131` |
| **Replay on gap** (the `replay_endpoint` ROUTER protocol) | ❌ **not implemented** | see below |
| `AllBlocksCleared` resync | ✅ wipes that engine's residency | `ingest` → `ClearEngine`, `listener.go:251-252`, `index.go:208-242` |
| Memory bounding via `BlockRemoved` | ✅ removes resident + bridge entries | `RemoveEvent`, `index.go:183-204` |
| Admission safety on gap/disconnect | ✅ falls back to accept (no false 429) | `StreamFreshForEngines`, `service.go:117-131` |

### last_seq and gaps

Every batch's seq is checked against the previous (`trackSeq`, `listener.go:186-194`).
A gap means at-least-once delivery dropped something (ZMQ HWM drop, or a missed
publish). The gap is **counted but not repaired**.

### Replay endpoint is configured but never dialed (gap finding)

The engines expose a replay ROUTER socket (vLLM/SGLang send an 8-byte start-seq, the
publisher replays buffered batches; see [kv-events.md](./kv-events.md) §2 "Replay 协议").
The kvindexer **stores** that endpoint (`Engine.ReplayEndpoint`, `model.go:36`;
`bootstrap.go:79`; seeded at `main.go:156`) but **no code ever dials it**:
`grep -rni replay internal/ cmd/ --include='*.go'` returns only struct/field
definitions and the seed literal — there is no ZMQ DEALER/REQ to the replay socket and
no call site that triggers a replay when `GapsTotal` increments. So:

- **A dropped event is permanently missing** until the engine re-emits that prefix
  (a subsequent identical request re-stores the full chain in order, as noted at
  `listener.go:233-237`), or until an `AllBlocksCleared` resets the engine's slate.
- Correctness is preserved *for admission* because `StreamFreshForEngines` makes a
  gapped stream untrusted, so a missing residency causes **fallback-to-accept, never a
  wrong 429**. The cost is **degraded hit accuracy** (we under-report residency for the
  blocks we missed), not a correctness violation.

### Memory bounding

The index is bounded by **remove/clear events**, not by TTL:

- `BlockRemoved` resolves engine hashes through the `bridge` and drops the resident's
  hold; when a request_key has no holders left, the whole entry is deleted
  (`RemoveEvent`, `index.go:194-199`).
- `AllBlocksCleared` drops everything that engine contributed and wipes its bridges
  (`ClearEngine`, `index.go:208-242`).
- There is **no TTL/LRU eviction** in the index. If an engine drops events such that a
  `BlockRemoved` for a block we recorded is itself lost, that request_key can leak
  (stay resident forever). `AllBlocksCleared` is the only backstop, and it only fires
  on a full engine cache reset. This is the second consequence of the missing replay.

---

## 6. Capacity rules of thumb

These are back-of-envelope numbers to size shards; treat them as order-of-magnitude,
not benchmarks (no benchmark exists in-repo yet — see §7).

### Throughput (events/sec one Go process can apply)

- The hot path is **one msgpack decode + a few FNV hashes + one mutex-guarded map
  update** per batch. msgpack-into-`[]any` reflection decode is the dominant term,
  typically **single-digit microseconds per small batch** on a modern core.
- A single listener goroutine therefore handles **roughly 1e5 batches/sec** before the
  decode loop saturates one core; aggregate across engines is higher only until the
  shared per-namespace mutex (`index.go:103`) becomes the limiter for a hot model.
- **Practical sizing:** plan one shard per **hot model** at high QPS, and split a single
  model's engines across shards only when one process's CPU for that namespace is
  saturated (watch `GapsTotal` climbing as the early-warning signal — gaps appearing
  means decode/apply fell behind the SUB HWM).

### Memory per resident block

Per **resident request_key** the index holds (`index.go:90-99`, `:106-118`):

- one map entry in `byRequestKey`: 8-byte key + pointer to a `residencyEntry`.
- the `residencyEntry.holders` map: one entry per distinct `{engineID, dpRank, tier}`
  holding that block, each ~ (struct key + int64 timestamp).
- one `bridge` entry per engine block hash we received: `{engineID,EngineKey}` →
  `RequestKey`.
- one entry in that engine's `byEngine` set.

A Go `map` entry has substantial overhead (bucket arrays, ~48–64 B amortized per
entry is a safe planning figure). With the three maps each carrying one entry per
block per holder, budget **roughly 200–300 bytes per (block × holder)**.

- **1 million resident blocks, single holder each ≈ a few hundred MB.** Call it
  **~0.25–0.3 GB per million resident blocks**, scaling linearly with the number of
  distinct holders per block (replication across engines/DP ranks/tiers multiplies it).
- Memory grows with **#resident blocks × avg holders/block**, i.e. with
  **#engines × per-engine-KV-capacity × churn-not-yet-removed**. It does **not** grow
  with event *rate* (no queue), only with the *resident set* that BlockRemoved hasn't
  cleared.

### When to add a shard

Add a kvindexer shard to a cluster when **any** of:

- `GapsTotal` on `/event-streams` is **non-zero and climbing** for a busy engine
  (decode/apply fell behind the SUB HWM — the load signal).
- A single process's **RSS** approaches the box's RAM given the resident-block estimate
  above (e.g. you expect > ~5–10M resident blocks × holders on one process).
- One model's ingest goroutine(s) sit at ~100% CPU while gaps appear (per-namespace
  mutex / decode saturation).

Prefer **shard-by-model** (clean lock + memory split, queries stay complete). Use
**shard-by-engine-subset** only for a single hot model, accepting the per-query
single-shard visibility caveat in §4(b).

---

## 7. Known gaps / TODO for production

The numbered items below are real gaps. #2 has since been **addressed** (kept here with
its resolution for the record); the rest are **documented, not fixed**.

1. **No gap-triggered replay / no index rebuild on restart.** `ReplayEndpoint` is
   configured and stored (`model.go`, `bootstrap.go`, seeded in `main.go`) but **never
   dialed**. There is no ZMQ DEALER/REQ to the replay ROUTER and no call site wired to
   `GapsTotal` incrementing. Two consequences, both confirmed live:
   - **Dropped events are permanently lost** until the prefix is re-emitted or
     `AllBlocksCleared` fires — degraded hit accuracy under load + possible residency
     leak (a lost `BlockRemoved` is never reconciled).
   - **A kvindexer restart starts with a COLD index.** The residency index is in-memory
     only (SQLite persists *config*, not residency). After a restart the engine will
     **not** re-emit `BlockStored` for prefixes it still has cached, so those resident
     blocks are invisible until they're evicted+refilled. Wiring replay (request a
     full resync from the engine's buffer on connect and on gap) fixes both.

   This is the single biggest correctness-under-load gap.

2. **App-level backpressure — RESOLVED.** A bounded recv→apply queue
   (`applyQueueSize = 8192`) now decouples the socket recv loop from decode+apply, with
   a dedicated apply goroutine and exported `QueueDepth`/`QueueCap` health (`listener.go`,
   `TestApplyLoopDrainsQueue`). A burst buffers in the queue; sustained overrun blocks the
   recv loop → ZMQ HWM → a detected gap (folds into #1), never OOM. Remaining sub-item:
   the SUB HWM itself is still the zmq4 default and there is no per-listener *drop* metric
   distinct from `GapsTotal` — add if you need to distinguish queue-block from HWM-drop.

3. **No TTL/LRU eviction in the index.** Memory is bounded only by `BlockRemoved` /
   `AllBlocksCleared` (`index.go:183-242`). A lost `BlockRemoved` (see #1) leaks a
   request_key until that engine's next full clear. No periodic compaction exists.

4. **No per-shard engine-subset selector.** `ApplyBootstrapForCluster` (`main.go`)
   seeds *all* of a cluster's engines into the process. To shard one cluster's engines
   across processes you must prune via the console or maintain per-shard config files;
   there is no `-engines=subset` / label-selector flag. This makes axis 4(b) sharding
   operationally clumsy.

5. **Single-shard query visibility.** Admission/query go through `proxyOne`
   (`gateway.go`) to exactly one backend. If a single model's engines are split across
   shards, one query only sees that shard's engines — there is no scatter-gather merge
   for `/query-prefix` / `/route` across backends (unlike the GET list endpoints, which
   merge via `fanoutList`). Sharding-by-model avoids this; sharding one model across
   shards hits it.

6. **One mutex per namespace for both reads and writes.** `Index.mu` serializes ingest
   and query for a model. At very high combined ingest+query QPS on a single hot model
   this is the in-process ceiling; consider read-optimized or sharded sub-indexes if
   profiling shows lock contention before CPU saturation.

---

## TL;DR

- **The gateway does not subscribe to ZMQ — the kvindexer does**, one process per
  cluster, node-local, one SUB socket + one goroutine per engine
  (`service.go:63-95`, `listener.go:80-161`).
- One kvindexer comfortably listens to **many engines in a cluster**. The limiter at
  high volume is **single-threaded msgpack decode + a per-model mutex**, not the socket
  count.
- There is a **bounded recv→apply queue** (cap 8192, exported as `queue_depth`/`queue_cap`):
  a burst buffers there instead of OOMing; sustained overrun blocks recv → ZMQ drops past
  its HWM → **sequence gaps** that are **detected but not yet replayed** (gap #1).
- To handle a hot cluster, **shard engines/models across multiple kvindexers** and add
  their URLs to `clusters[].backends`; the gateway federates them transparently.
- Rules of thumb: **~1e5 batches/sec/core**, **~0.25–0.3 GB per million resident
  blocks** (× holders), add a shard when `GapsTotal` climbs or RSS nears the box.
- Biggest production gaps: **no replay on gap**, **no backpressure bound**,
  **no TTL eviction** (§7).
