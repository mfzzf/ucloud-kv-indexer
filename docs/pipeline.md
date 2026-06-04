# ucloud-kv-indexer 工作原理详解

## 整体架构

```
vLLM/SGLang engine
    │  ZMQ PUB (tcp://*:PORT)
    │  3帧: [topic | seq(8B big-endian) | msgpack payload]
    ▼
Listener.subscribeLoop()
    │  recv→applyQueue (8192 深度的异步缓冲)
    ▼
Listener.ingest()  ─── 解码 + 过滤 + 计算 request_key
    │
    ▼
residency.Index  ─── 内存索引（链式哈希前缀键，等价于 radix tree）
    │
    ▼
admission.Evaluate() ─── 命中率计算 + 策略决策
```

---

## 第一层：ZMQ 事件解码

[`internal/kvevents/decode.go`](../internal/kvevents/decode.go) 定义了 wire format：

```
ZMQ 3帧消息：
  Frame[0]: topic bytes（任意，订阅时用空过滤器接收全部）
  Frame[1]: seq uint64（8字节 big-endian，用于 gap 检测）
  Frame[2]: msgpack 编码的 payload

msgpack payload = [ts(float64), events([]event), dp_rank(int?)]

BlockStored event = [
  "BlockStored",      // [0]  tag
  block_hashes,       // [1]  []uint64 - 引擎内部哈希
  parent_block_hash,  // [2]  uint64   - 父块哈希（用于链式推导）
  token_ids,          // [3]  []int32  - 该批块的 token ID
  block_size,         // [4]  int      - 每块 token 数
  lora_id,            // [5]  int?
  medium,             // [6]  string   - "GPU" / "CPU" / "DISK"
  lora_name,          // [7]  string?
  extra_keys,         // [8]  []any?
  group_idx,          // [9]  int      - 区分 mamba/attention 组
  kv_cache_spec_kind, // [10] string   - "full_attention" / "mamba" 等
  sliding_window,     // [11] int?
]

BlockRemoved event = ["BlockRemoved", block_hashes, medium, group_idx]
AllBlocksCleared  = ["AllBlocksCleared"]
```

入口在 [`listener.go:286`](../internal/kvevents/listener.go)：

```go
func (l *Listener) handleFrames(frames [][]byte) {
    seq := binary.BigEndian.Uint64(frames[1])  // 解序列号
    msgpack.Unmarshal(frames[2], &payload)     // 解 payload
    batch, _ := DecodeBatch(seq, payload)      // 解事件批次
    l.trackSeq(int64(seq))                     // 检测 gap（缺包）
    l.ingest(batch)                            // 写入索引
}
```

`trackSeq` 做 gap 检测：若 `seq > lastSeq+1`，说明有 ZMQ 帧丢失（TCP HWM 背压触发），`gaps.Add(1)` 计数，后续 freshness 判断会感知到并触发 fallback 策略。

recv 和 apply 解耦：ZMQ 收帧后写入一个 8192 深度的 channel，独立 goroutine `applyLoop` 负责 decode + 写索引，recv loop 持续热跑不阻塞在锁上。

---

## 第二层：从 ZMQ 事件到链式哈希索引

### 2.0 先理解"为什么要两套 key"

vLLM/SGLang 引擎自己维护了一套 `block_hashes`（`EngineKey`），是引擎内部的 opaque uint64，我们拿不到它的生成规则，也无法在收到请求时提前算出来。

所以 kv-indexer 自己再算一套 `RequestKey`：用 token_ids 做链式 FNV-64a，这套 key **入库时**（从 BlockStored 的 token_ids 算）和**查询时**（从 tokenizer 返回的 token_ids 算）用完全相同的公式，天然匹配。

两套 key 各有用途：

| key 类型 | 从哪来 | 用在哪 |
|---------|-------|-------|
| `EngineKey` | ZMQ `block_hashes` 字段 | 处理 BlockRemoved（只带 engine hash，没有 token_ids） |
| `RequestKey` | 我们自己从 `token_ids` 计算 | 查询时前缀命中匹配 |

`bridge map[{engineID, EngineKey}]RequestKey` 把两套 key 连接起来，让 BlockRemoved 能通过 engine hash 找到对应的 request_key 删掉它。

---

### 2.1 一条 BlockStored 事件到底携带了什么

ZMQ 收到的原始 msgpack 数组（对应 [decode.go:191](../internal/kvevents/decode.go)）：

```
fields[0] = "BlockStored"
fields[1] = block_hashes       → []uint64，本次存储的所有 block 的引擎内部哈希
                                  例如 [0xAB12..., 0xCD34..., 0xEF56...]（3个block）
fields[2] = parent_block_hash  → uint64，上一个 block 的引擎哈希
                                  例如 0x1234...（没有父块时为 nil/absent）
fields[3] = token_ids          → []int32，本次这几个 block 的全部 token ID 拼在一起
                                  例如 [15496, 318, 257, 1263, ..., 31337]（共 N 个）
fields[4] = block_size         → int，每个 block 包含多少个 token
                                  例如 16
fields[6] = medium             → string，存在哪个存储层："GPU" / "CPU" / "DISK"
fields[10]= kv_cache_spec_kind → string，"full_attention" / "mamba" 等
```

**关键点**：`token_ids` 是这次事件里**所有 block 的 token 拼在一起的平铺数组**，不是按 block 分组的。如果这次事件有 3 个 block，block_size=16，那 token_ids 就有 48 个元素，前 16 个属于 block[0]，中间 16 个属于 block[1]，最后 16 个属于 block[2]。

---

### 2.2 ingest() 的决策流程

[`listener.go:408`](../internal/kvevents/listener.go) 收到 batch 后逐事件处理：

```
收到 BlockStored 事件
    │
    ├─ spec_kind == "mamba" / "linear_attention"?
    │    → 跳过（这类 block 不做前缀缓存）
    │
    ├─ HasNestedTokenIDs? (token_ids 是嵌套数组)
    │    → 跳过（暂不支持）
    │
    ├─ HasExtraKeys / HasLoraID?
    │    → 跳过（LoRA/多模态请求，hash 不可靠）
    │
    ├─ token_ids 为空，但 block_hashes 不为空?
    │    → offload 二次存储（如 CPU←GPU 迁移）
    │    → ix.StoreEventByEngineKeys()：通过已有 bridge 找 request_key，补记 tier
    │
    └─ 正常路径：有 token_ids
         → requestKeysForEvent()  计算本次的 request_keys
         → ix.StoreEvent()        写入索引 + 建桥接
```

---

### 2.3 链式哈希：从 token_ids 计算 request_keys

这是核心，分三步。

**第一步：确定起点 seed**

[`listener.go:498`](../internal/kvevents/listener.go)：

```go
func (l *Listener) requestKeysForEvent(..., ev *Event) ([]RequestKey, bool) {
    parentSeed := seed                // seed = namespace 的 FNV hash，是整条链的根
    if ev.HasParent {
        // 这个事件不是第一批 block，它的 token_ids 是整个序列的中间段
        // 需要找到前一段末尾 block 的 request_key，作为本段的起点
        rk, ok := ix.LookupBridge(engineID, EngineKey(ev.ParentHash))
        //   ev.ParentHash = fields[2]，引擎给的上一个 block 的 engine hash
        //   bridge[{engineID, ParentHash}] = 上一个 block 的 request_key（之前入库时存下来的）
        if !ok {
            return nil, false   // 父块还没入库（乱序），跳过这条事件
        }
        parentSeed = uint64(rk) // 用父块 request_key 的数值作为本段哈希链的起点
    }
    return RequestKeysFromTokensSeeded(parentSeed, ev.TokenIDs, blockSize), true
}
```

**第二步：把 token_ids 切成 block_size 大小的块**

[`index.go:42`](../internal/residency/index.go)：

```go
func ChunkTokens(tokens []int32, blockSize int) [][]int32 {
    n := len(tokens) / blockSize     // 只取整块，丢掉尾部不足一块的部分
    for i := 0; i < n; i++ {
        out = append(out, tokens[i*blockSize : (i+1)*blockSize])
    }
    return out
}
```

例子：token_ids 有 48 个，blockSize=16 → 切成 3 个 chunk，每个 16 个 token。

**第三步：对每个 chunk 做链式 FNV-64a**

[`index.go:56`](../internal/residency/index.go)：

```go
func hashBlock(parent uint64, chunk []int32) uint64 {
    h := fnv.New64a()
    // 先写入父块的哈希值（8字节小端）
    binary.LittleEndian.PutUint64(buf[:], parent)
    h.Write(buf[:])
    // 再逐个写入本块的每个 token ID（每个 4字节小端）
    for _, t := range chunk {
        binary.LittleEndian.PutUint32(tb[:], uint32(t))
        h.Write(tb[:])
    }
    return h.Sum64()
}

func RequestKeysFromTokens(seed uint64, tokens []int32, blockSize int) []RequestKey {
    parent := seed
    for _, chunk := range ChunkTokens(tokens, blockSize) {
        parent = hashBlock(parent, chunk)         // 每块的哈希都依赖上一块的输出
        keys = append(keys, RequestKey(parent))   // 这块的 request_key 就是这个 parent
    }
    return keys
}
```

**FNV 是什么**：FNV-64a（Fowler–Noll–Vo）是一种非加密哈希算法，把任意字节序列映射成一个 uint64 整数。特点是速度快、实现简单，输入哪怕只变一个字节，输出值就会完全不同。这里用它把"父块哈希 + 本块所有 token ID"压缩成一个 64 位整数作为这个 block 的身份标识。

---

**具体数字例子**，假设 `blockSize=4`，namespace seed=`S`：

**请求 A** 的完整 token 序列：

```
[10, 20, 30, 40,  50, 60, 70, 80,  90, 100, 110, 120]
 ←── block 0 ──→  ←── block 1 ──→  ←───  block 2  ──→

request_key_A[0] = FNV(S,                    [10,20,30,40])    = 0xAAAA
request_key_A[1] = FNV(0xAAAA,               [50,60,70,80])    = 0xBBBB
request_key_A[2] = FNV(0xBBBB,               [90,100,110,120]) = 0xCCCC
```

**请求 B** 的 token 序列（前 8 个和 A 相同，第 3 块不同）：

```
[10, 20, 30, 40,  50, 60, 70, 80,  200, 201, 202, 203]
 ←── block 0 ──→  ←── block 1 ──→  ←────  不同  ────→

request_key_B[0] = FNV(S,                    [10,20,30,40])    = 0xAAAA  ← 和 A 完全一样
request_key_B[1] = FNV(0xAAAA,               [50,60,70,80])    = 0xBBBB  ← 和 A 完全一样
request_key_B[2] = FNV(0xBBBB,               [200,201,202,203])= 0xDDDD  ← 不同
```

request_key[1] 之所以和 A 一样，不只是因为 block 1 的 token 相同，还因为 request_key[0] 相同（request_key[0] 参与了 request_key[1] 的计算）。展开来写：

```
request_key[1] = FNV(request_key[0],   block1_tokens)
               = FNV(FNV(S, block0_tokens), block1_tokens)
                          ↑ block0 的信息已经嵌入进来了

request_key[2] = FNV(request_key[1],   block2_tokens)
               = FNV(FNV(FNV(S, block0_tokens), block1_tokens), block2_tokens)
                                    ↑ block0 + block1 + block2 全都嵌进来了
```

**这就是"链式"的含义**：request_key[N] 的值由 block 0 到 block N 的所有 token 共同决定。所以两个请求前 N 个 token 完全相同，前 N/blockSize 个 request_key 就完全相同。

**查询时怎么用**：

新请求来了，tokenizer 返回 token 序列，用同一个公式算出 `[0xAAAA, 0xBBBB, ...]`，然后直接查 map：

```
map[0xAAAA] 存在？ → engine-A 持有 block 0（GPU）
map[0xBBBB] 存在？ → engine-A 持有 block 1（GPU）
map[0xCCCC] 存在？ → 不存在，block 2 未缓存，停止
```

三次 O(1) map 查找，得出"前 2 个 block（8 个 token）在 engine-A 的 GPU 上命中"。不需要树结构，不需要遍历，链式哈希把"这个前缀存不存在"压缩成了一个 uint64 值。

---

### 2.4 写入索引和建桥接

[`index.go:142`](../internal/residency/index.go)：

```go
func (ix *Index) StoreEvent(engineID string, dpRank int, tier string,
                             engineKeys []EngineKey, requestKeys []RequestKey) {
    // ① 记录所有 requestKey 的 residency（谁持有、什么时间）
    for _, rk := range requestKeys {
        ix.byRequestKey[rk].holders[{engineID, dpRank, tier}] = now()
        ix.byEngine[engineID][rk] = struct{}{}
    }

    // ② 建桥接（尾对齐）
    //    engineKeys 来自 fields[1]（block_hashes，引擎给的）
    //    requestKeys 是我们刚算出来的
    //    两个列表通常等长；hybrid 模型 mamba null-block 会导致 engineKeys 更长，尾对齐保证最后一个 block 总是配对
    k := min(len(engineKeys), len(requestKeys))
    for i := 0; i < k; i++ {
        ek := engineKeys[len(engineKeys)-k+i]
        rk := requestKeys[len(requestKeys)-k+i]
        ix.bridge[{engineID, ek}] = rk
    }
}
```

写入后索引里有两张表：

```
byRequestKey:
  rk[0] → holders: { {engine-A, dp=0, gpu}: ts, ... }
  rk[1] → holders: { {engine-A, dp=0, gpu}: ts, ... }
  rk[2] → holders: { {engine-A, dp=0, gpu}: ts, ... }

bridge:
  {engine-A, block_hashes[0]} → rk[0]
  {engine-A, block_hashes[1]} → rk[1]
  {engine-A, block_hashes[2]} → rk[2]
```

---

### 2.5 为什么乱序到达的事件要跳过而不是等待

vLLM 一次 prefill 可能产生多个 BlockStored 事件，按顺序发送：

```
事件 A：block_hashes=[h0,h1,h2]，parent=nil，  token_ids=[t0..t47]   ← 前 3 块
事件 B：block_hashes=[h3,h4],   parent=h2，    token_ids=[t48..t79]  ← 后 2 块
```

事件 B 的 token_ids 只有后半段（t48..t79），它的 request_key 必须从 rk[2]（h2 对应的 request_key）续链。如果事件 A 还没到，bridge 里查不到 h2，就不知道 rk[2] 是什么，无法推导出 rk[3]/rk[4]。

**如果强行用 namespace_seed 当作 B 的起点**，算出的 rk[3]/rk[4] 是错的，和"A 先到、B 正常续链"算出的值完全不同，写进索引后永远不会被匹配到，反而污染了索引。所以宁可丢弃，等下次完整请求重来。

---

### 2.6 Mamba / Hybrid 模型为什么只过滤 spec_kind

[`listener.go:397`](../internal/kvevents/listener.go)：

```go
func ingestableSpecKind(kind string) bool {
    switch strings.ToLower(kind) {
    case "mamba", "mamba2", "linear_attention", "short_conv":
        return false
    default:
        return true
    }
}
```

Mamba/SSM 类层不是 attention，没有 KV cache 可以前缀复用，每次都要重算，缓存命中无意义。过滤掉可以让索引只记录真正能被命中的 attention block。

Qwen3.5 这类 hybrid 模型同一个 prefill 会发出多个 BlockStored 事件，`group_idx=0` 是 mamba 组（被过滤），`group_idx=1` 是 full_attention 组（被收录），两个组的 `block_hashes` 是独立的，过滤只看 `spec_kind` 字段，不影响另一组。

---

## 第三层：缓存命中率计算

### Query 前缀连续扫描

[`internal/residency/index.go:316`](../internal/residency/index.go)：

```go
func (ix *Index) Query(requestKeys []RequestKey, blockSize int) *QueryResult {
    // active = 还保持连续前缀命中的实例集合
    // 只有从 block[0] 就命中的实例才能进入 active；中途才有的实例不计入
    var active map[string]bool

    for i, rk := range requestKeys {   // 按前缀顺序逐块查
        entry := ix.byRequestKey[rk]   // O(1) 哈希查找

        // 收集这一块的持有者（按 engineID 聚合 tier 和 dp 信息）
        holders := buildHolders(entry)

        if i == 0 {
            active = {eng: true for eng in holders}  // 第一块：初始化 active
        } else {
            for eng := range active {
                if _, ok := holders[eng]; !ok {
                    delete(active, eng)              // 该实例没有这块 → 断链，移出
                }
            }
        }
        if len(active) == 0 { break }               // 所有实例都断了，提前退出

        for eng := range active {
            ih := res.Instances[eng]
            ih.LongestMatched += blockSize
            if tiers[GPU]              { ih.GPU  += blockSize }
            if tiers[GPU] || tiers[CPU]{ ih.CPU  += blockSize }  // 累积：CPU 包含 GPU
            ih.Disk += blockSize                                  // 任何 tier 都算 Disk
        }
    }
    return res
}
```

**时间复杂度：O(P × E)**，P = 请求的 prefix block 数，E = 当前活跃实例数（通常 < 10）。

`InstanceHit.CPU`、`GPU`、`Disk` 是**累积值**（`GPU ≤ CPU ≤ Disk`），代表各存储层级可见的 token 数。

### 命中率计算与加权

[`internal/admission/admission.go:97`](../internal/admission/admission.go)：

```go
const (
    gpuHitWeight  = 1.0   // GPU 内存：延迟最低
    cpuHitWeight  = 0.75  // CPU/host pinned：需 D2H 传输
    diskHitWeight = 0.25  // 磁盘/offload：IO 成本高
)

func bestHit(in Input) HitInfo {
    for id, inst := range in.Query.Instances {
        // 先把累积 tier 值还原为各 tier 独立 token 数
        gpu     := inst.GPU
        cpuOnly := inst.CPU - inst.GPU   // 只在 CPU、不在 GPU 的 token
        diskOnly:= inst.Disk - inst.CPU  // 只在 Disk 的 token

        // 加权等效 token 数
        eff := float64(gpu)*gpuHitWeight +
               float64(cpuOnly)*cpuHitWeight +
               float64(diskOnly)*diskHitWeight

        if eff > bestEff { 选该实例 }
    }
    // hit_ratio = min(effective_cached, input_tokens) / input_tokens
    hi.HitRatio = float64(min(hi.EffectiveCachedTokens, in.InputTokens)) / float64(in.InputTokens)
}
```

### 策略决策

[`internal/admission/admission.go:139`](../internal/admission/admission.go)：

```go
func Evaluate(in Input) Result {
    hit := bestHit(in)

    for _, rule := range in.Rules {    // 按 priority 降序，第一条匹配即决定
        if !matchesRule(in, hit, rule) { continue }

        switch rule.Action.Type {
        case "accept":
            return 200 OK

        case "reject":
            return 429 / 自定义状态码

        case "require_cache_hit":
            // 不确定性检查（tokenization 失败、hash 不支持、stream 不 fresh）
            if reason, uncertain := uncertaintyReason(in); uncertain {
                return applyOutcome(action.OnUncertain, ...)  // fallback_accept / reject
            }
            if hit.HitRatio < action.MinHitRatio {
                return applyOutcome(action.OnLowHit, ReasonCacheHitTooLow, ...)
            }
            return 200 OK（命中率达标）
        }
    }
    return accept(ReasonNoMatchingRule)  // 无匹配规则，默认放行
}
```

`require_cache_hit` 规则下，若 `fresh=false`（ZMQ stream 有 gap 或断连），触发 `on_uncertain` 分支，可配置为 `accept` / `reject` / `fallback_accept`（带 `fallback: true` 标记），让上游路由器感知到这次决策是在不确定条件下做出的。

---

## 完整数据流

```
① vLLM/SGLang 发出 BlockStored ZMQ 事件
   decode.go: 解析 token_ids, block_hashes, parent_hash, medium, spec_kind, group_idx

② listener.go ingest()
   ├─ 过滤：spec_kind == mamba/linear_attention → skip (skip_reason: non_ingestable_spec_kind)
   ├─ 过滤：extra_keys / lora_id 存在 → skip (unsupported_hash_extra_keys)
   ├─ 有 token_ids：
   │    requestKeysForEvent():
   │      parent_hash → bridge.LookupBridge → 父块 request_key
   │      → RequestKeysFromTokensSeeded(parentSeed, token_ids, blockSize)
   │    ix.StoreEvent(engineKeys, requestKeys)
   │      → byRequestKey[rk].holders[{engineID,dpRank,tier}] = now()
   │      → bridge[{engineID,ek}] = rk  （尾对齐）
   └─ 无 token_ids（offload tier 二次存储）：
        ix.StoreEventByEngineKeys(): bridge 查 requestKeys → 补记 tier residency

③ HTTP 请求到达（/v1/chat/completions 等）
   tokenizer.TokenizeChat() → token_ids
   residency.RequestKeysFromTokens(namespace_seed, tokens, blockSize) → reqKeys
   ix.Query(reqKeys, blockSize) → QueryResult{Instances}
   admission.Evaluate(Input{Query, Rules, Fresh, ...}) → Result{Decision, HitRatio, ...}
   Decision==reject → HTTP 429；Decision==accept → 转发到引擎

④ vLLM/SGLang 发出 BlockRemoved ZMQ 事件
   ix.RemoveEvent(engineKeys):
     bridge[{engineID,ek}] → rk → delete holders[resident]
     len(holders)==0 → delete byRequestKey[rk] + bridge entry

⑤ AllBlocksCleared（引擎重启/清空）
   ix.ClearEngine(engineID, tier=""):
     遍历 byEngine[engineID] 的所有 rk → 清空 holders → 清桥接 → 清 byEngine
```
