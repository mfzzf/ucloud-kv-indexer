# vLLM / SGLang KV-Cache 事件参考

> 本文件枚举 vLLM 与 SGLang 通过 ZMQ 发布的全部 KV-cache 事件、它们的 wire format、
> 逐字段差异，以及 `ucloud-kv-indexer` 如何解码与适配两者。
>
> 配套文档：[tokenization.md](./tokenization.md)（两框架如何 tokenize 拿到 token_ids、
> indexer 如何适配）。
>
> 源码引用（相对仓库根 `/home/ubuntu/selfhost-schedular`）：
> - vLLM：`vllm/vllm/distributed/kv_events.py`
> - SGLang：`sglang/python/sglang/srt/disaggregation/kv_events.py`、
>   `sglang/python/sglang/srt/mem_cache/events.py`、
>   `sglang/python/sglang/srt/mem_cache/utils.py`
> - indexer：`internal/kvevents/decode.go`、`internal/kvevents/listener.go`、
>   `internal/residency/index.go`

---

## 1. 为什么需要 KV 事件

`ucloud-kv-indexer` 不持有 GPU，也不读引擎显存。它要回答的是“某个 prompt 的前缀有多少
已经驻留在某台引擎的 KV cache 里”。引擎是唯一知道 radix tree / prefix cache 真实状态的
组件，因此它通过 ZMQ PUB 把每一次 **block 写入 / 淘汰 / 清空** 广播出来。indexer 订阅
这些事件，在内存里重建一份 **residency index**（哪个 block 在哪台引擎、哪个 tier），
查询时即可判断前缀命中。

事件流是 **at-least-once + 单调 seq 号**，配合一个 replay ROUTER socket 补洞。

---

## 2. Wire Format（两框架完全一致）

每条消息是一个 3-frame ZMQ PUB 多帧消息：

```
frame[0] = topic_bytes          # 订阅过滤用，可为空
frame[1] = seq                  # 8 字节, big-endian, 无符号 uint64, 单调递增
frame[2] = msgpack_payload      # msgspec array_like 编码的 EventBatch
```

- vLLM：`ZmqEventPublisher._publisher_thread` → `send_multipart((topic, seq_bytes, payload))`
  （`kv_events.py:434`）
- SGLang：同名方法，同样 `send_multipart((self._topic_bytes, seq_bytes, payload))`
  （`kv_events.py:315`）

`payload` 反序列化后是一个**数组**（`msgspec.Struct(array_like=True)`，所以按**位置**而非
字段名编码）：

```
[ ts(float), events(list), dp_rank(int|null) ]
   位置 0       位置 1         位置 2
```

| 位置 | vLLM 字段名 | SGLang 字段名 | 说明 |
|---|---|---|---|
| 0 | `ts` | `ts` | 批次时间戳（float 秒） |
| 1 | `events` | `events` | 事件数组，每个元素是 tag-prefixed array |
| 2 | `data_parallel_rank` | `attn_dp_rank` | DP rank；**字段名不同但位置相同**，`array_like` 按位置编码，故 wire 上无差异 |

> **关键点**：因为是 `array_like`，字段名差异（`data_parallel_rank` vs `attn_dp_rank`）
> 在 wire 上不可见，indexer 只按位置 `payload[2]` 取值。

`events` 里每个事件也是 `array_like` + `tag=True` 的 struct，即**首元素是字符串 tag**，
后续是按位置排列的字段：

```
[ "BlockStored", field1, field2, ... ]
[ "BlockRemoved", field1, ... ]
[ "AllBlocksCleared" ]
```

### Replay 协议（两框架一致）

订阅者发现 seq 跳号时，向 replay ROUTER socket 发送 8 字节 big-endian 的起始 seq，
publisher 回放缓冲区中 `seq >= start` 的所有批次，并以 `seq = -1`（8 字节有符号）+ 空
payload 作为结束标记（vLLM `kv_events.py:444-465`；SGLang 同构）。

---

## 3. 事件类型（共 3 种）

两框架都只有这三种事件，tag 字符串完全一致：`BlockStored` / `BlockRemoved` /
`AllBlocksCleared`。

### 3.1 `BlockStored` — 新前缀块写入 KV cache

这是唯一携带 `token_ids` 的事件，是 indexer 重建 request_key 的数据来源。

**vLLM** `BlockStored`（`kv_events.py:49-90`），共 **12** 个位置字段：

```
[ "BlockStored",
  block_hashes,                  # 1: list[bytes|int]  本块的 engine 哈希
  parent_block_hash,             # 2: bytes|int|null   父块 engine 哈希（前缀链）
  token_ids,                     # 3: list[int]        本事件覆盖的 token id
  block_size,                    # 4: int              每块 token 数
  lora_id,                       # 5: int|null         (已废弃，向后兼容)
  medium,                        # 6: str|null         "GPU" / ...
  lora_name,                     # 7: str|null
  extra_keys,                    # 8: list[tuple|null]|null  每块的 MM/LoRA/cache_salt 等
  group_idx,                     # 9: int|null         KV group 序号（混合模型）
  kv_cache_spec_kind,            # 10: str|null        "full_attention"/"mamba"/...
  kv_cache_spec_sliding_window ] # 11: int|null
```

**SGLang** `BlockStored`（`kv_events.py:86-92`），只有 **7** 个位置字段：

```
[ "BlockStored",
  block_hashes,        # 1: list[int]        总是 int64（见 §4）
  parent_block_hash,   # 2: int|null
  token_ids,           # 3: list[int]
  block_size,          # 4: int
  lora_id,             # 5: int|null
  medium ]             # 6: str|null         "GPU" / "CPU_PINNED" / "DISK" / "EXTERNAL"
```

> SGLang **没有** 位置 7–11（`lora_name` / `extra_keys` / `group_idx` /
> `kv_cache_spec_kind` / `sliding_window`）。因为 `msgspec(omit_defaults=True)`，
> trailing 字段缺省时直接不出现在数组里，所以 SGLang 发出的就是短数组。

### 3.2 `BlockRemoved` — 块被淘汰

**vLLM**（`kv_events.py:93-105`）：
```
[ "BlockRemoved", block_hashes, medium, group_idx ]
   tag            1: list        2: str  3: int|null
```

**SGLang**（`mem_cache/events.py:107-109`）：
```
[ "BlockRemoved", block_hashes, medium ]
   tag            1: list        2: str|null
```

`BlockRemoved` **不带 token_ids**，只带 engine 哈希。indexer 靠 §6 的 bridge 把 engine
哈希反查回 request_key 再删除。

### 3.3 `AllBlocksCleared` — 全量清空

两框架都是无字段：`[ "AllBlocksCleared" ]`。indexer 据此清掉该引擎贡献的全部 residency。

---

## 4. `block_hashes` 的编码差异（重点）

| | vLLM | SGLang |
|---|---|---|
| 类型 | `ExternalBlockHash = bytes \| int`（`kv_cache_utils.py:52`） | 总是 **int64** |
| 默认形态 | **sha256 bytes**；当 `VLLM_KV_EVENTS_USE_INT_BLOCK_HASHES=1` 时转 int64（取大端低 64 位，`maybe_convert_block_hash` `kv_cache_utils.py:77-80`） | `hash_str_to_int64`：取 sha256 hex 前 16 位（64 bit）转 **有符号** int64（`utils.py:444-452`） |
| 在 wire 上 | 可能是 msgpack bin（bytes）或 int | 总是 msgpack int |

indexer 的 `toUint64`（`decode.go:60-94`）同时处理两种：
- `int64/uint64/int32/...` → 直接转 `uint64`
- `[]byte` → 取**末 8 字节大端**拼成 `uint64`（对齐 llm-d 的 `getHashAsUint64`）

> 注意：这些 engine 哈希 **只用于 BlockRemoved 反查和 parent 链**，indexer 查询命中
> 用的是自己算的 request_key（见 [tokenization.md](./tokenization.md) §5 与本文 §6），
> 所以两框架哈希算法不同 **不影响** 命中判断，只要 engine 哈希在 store/remove 间自洽即可。

---

## 5. `medium`（存储 tier）差异

| tier | vLLM 发出 | SGLang 发出（`StorageMedium` 枚举，`kv_events.py:60-66`） | indexer 映射（`listener.go:286-298`）|
|---|---|---|---|
| L1 设备 HBM | `"GPU"` | `"GPU"` | `gpu` |
| L2 host pinned | （vLLM 当前主要发 GPU） | `"CPU_PINNED"` ⚠️ | `cpu` |
| L3 SSD/NVMe | — | `"DISK"` | `disk` |
| L4 远端/共享 | — | `"EXTERNAL"` | `disk` |

> SGLang 的 `StorageMedium.CPU` 序列化值是 **`"CPU_PINNED"`** 而非 `"CPU"`。indexer 的
> `mediumToTier` 已同时接受 `"CPU"` 和 `"CPU_PINNED"`，都归到 `cpu` tier。空 / 未知
> medium 默认 `gpu`。

---

## 6. 混合模型（Mamba / 线性注意力）的处理差异（重点）

`qwen3.5-4b` 等 hybrid 模型同时有 Mamba 组和 full-attention 组。**只有 attention 组**的
KV 是可按 token 前缀复用的；Mamba/recurrent 组发出的是退化的单一 hash，不能参与前缀命中
评分，否则会污染 hit-ratio。

### vLLM 的做法
每个 KV group 发**独立**的 `BlockStored`，带 `group_idx` 和 `kv_cache_spec_kind`。
indexer 的 `ingestableSpecKind`（`listener.go:199-207`）按 `spec_kind` 过滤：
```
mamba / mamba2 / linear_attention / short_conv  -> 跳过（不 index）
full_attention / sliding_window / "" (未知/标准) -> index
```

### SGLang 的做法
SGLang 的 `BlockStored` **不带** `kv_cache_spec_kind`（短数组），indexer 收到的
`SpecKind == ""` → `ingestableSpecKind("")` 返回 `true` → 全部 index。

这看似有风险，但实际是正确的：SGLang 对 hybrid 模型通过 **`page_size=1`** 让 radix tree
本身处理混合（见 `deploy/localhost.yaml`：`block_size: 1  # qwen3.5 hybrid-Mamba
forces SGLang page_size=1`）。`mamba_radix_cache._record_store_event`
（`mamba_radix_cache.py:1171`）发出的 `BlockStored` 里 `token_ids` 是**真实 token**、
hash 是真实前缀 hash，对 indexer 而言就是普通 attention 块，request_key 链能正常匹配。
**SGLang 不会把 Mamba 退化块作为独立事件发给事件流**，所以“全部 index”对 SGLang 是对的。

> 结论：`spec_kind == ""` 对 SGLang 表示“正常可索引块”，**不要**把空串当成需要过滤的
> 未知类型——这是 vLLM 与 SGLang 在该字段上的语义分叉。

---

## 7. `extra_keys` / LoRA / 多模态

- **vLLM**：`BlockStored.extra_keys`（位置 8）每块一项，承载 MM 标识、LoRA name、
  `cache_salt`、prompt-embedding hash 等——这些都是 prefix 命中的“命名空间”一部分
  （相同 token_ids 但不同 extra_key 不能共享 KV）。indexer 的
  `hasNonNilExtraKeys`（`decode.go:229-240`）只把它当作一个 **feature flag**
  （`HasExtraKeys`），标记“这是带 LoRA/MM/salt 的请求，纯文本 hash profile 无法可靠
  匹配”，并不参与 request_key 计算。
- **SGLang**：不发 `extra_keys`（也不发 `lora_name`），`HasExtraKeys` 恒为 `false`。

---

## 8. indexer 的解码与入库流程

```
ZMQ SUB (listener.go)
   └─ Recv() 3-frame → handleFrames()
        ├─ seq = big-endian(frame[1])，trackSeq 检测跳号 → gaps
        ├─ msgpack.Unmarshal(frame[2]) → []any
        └─ DecodeBatch(seq, payload)         (decode.go:142)
             ├─ payload[0] → TS
             ├─ payload[2] → DPRank          (位置取值，名字无关)
             └─ payload[1] 逐事件 decodeEvent()
                  ├─ "BlockStored"  → 读 1..10 位（带边界检查，短数组安全）
                  ├─ "BlockRemoved" → 读 block_hashes + medium + group_idx
                  └─ "AllBlocksCleared"
   └─ ingest(batch)                          (listener.go:210)
        ├─ ResolveIngest(model, engine) → 拿到 index / namespace / seed / blockSize
        ├─ BlockStored:
        │    ├─ ingestableSpecKind(SpecKind)? 否则跳过（vLLM mamba）
        │    ├─ tier = mediumToTier(Medium)
        │    ├─ requestKeysForEvent: 用 token_ids 重算 request_key 链
        │    │     parentSeed = (有父块 ? bridge[engineParentHash] : namespaceSeed)
        │    │     若声明了父块但 bridge 查不到 → 跳过（避免用错 seed 污染索引）
        │    └─ ix.StoreEvent(engine, dp, tier, engineKeys, requestKeys)
        │          记录 request_key 驻留 + engineKey→requestKey bridge（尾对齐）
        ├─ BlockRemoved: ix.RemoveEvent(engine, dp, tier, engineKeys)  经 bridge 反查
        └─ AllBlocksCleared: ix.ClearEngine(engine)
```

`requestKeysForEvent`（`listener.go:266-276`）是关键桥接：indexer 用事件里的
`token_ids` + 命名空间 seed，**重算**出和查询侧一模一样的 request_key 链
（FNV-64a chained hash，见 [tokenization.md](./tokenization.md) §5），从而把“引擎内部的
不透明 hash”和“查询侧的 token-based hash”对上。

---

## 9. 差异速查表

| 维度 | vLLM | SGLang | indexer 兼容 |
|---|---|---|---|
| 3-frame `[topic, seq, msgpack]` | ✅ | ✅ | ✅ 通用 |
| replay ROUTER 协议 | ✅ | ✅ | ✅ |
| EventBatch 位置 `[ts, events, dp]` | `data_parallel_rank` | `attn_dp_rank` | ✅ 按位置取值 |
| `BlockStored` 字段数 | 12（含 spec/extra_keys） | 7 | ✅ 边界检查容短数组 |
| `block_hashes` | bytes 或 int64 | 总是 int64 | ✅ `toUint64` 双路 |
| `medium` host tier | （主要 GPU） | `"CPU_PINNED"` | ✅ 已映射 |
| `kv_cache_spec_kind` | 有，用于过滤 mamba | 无（`""`）→ 全 index | ✅ 语义正确（SGLang 不发退化块） |
| `extra_keys` | 有 | 无 | ✅ 仅作 feature flag |
| `BlockRemoved` 字段 | hashes+medium+group_idx | hashes+medium | ✅ |
| `AllBlocksCleared` | ✅ | ✅ | ✅ |

---

## 10. 开启事件的引擎侧配置

- **vLLM**：`--kv-events-config '{"enable_kv_cache_events": true, "publisher": "zmq",
  "endpoint": "tcp://*:5557", "replay_endpoint": "tcp://*:5558", "topic": "kv-events"}'`
  （`engine/arg_utils.py:1476`，`config/kv_events.py`）
- **SGLang**：`--kv-events-config '{"publisher":"zmq","endpoint":"tcp://*:5557",
  "replay_endpoint":"tcp://*:5558","topic":"kv-events"}'`
  （`server_args.py:5370`）

indexer 侧在 `deploy/localhost.yaml` 的 engine 条目里配 `kv_event_endpoint` /
`replay_endpoint` / `topic` 即可订阅（见 `internal/config` 与 `httpapi/service.go`
的 `SyncListeners`）。
