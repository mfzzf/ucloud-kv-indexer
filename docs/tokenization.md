# Tokenization：vLLM / SGLang 如何拿到 token_ids，indexer 如何适配

> 本文件说明 vLLM 与 SGLang 的 `/tokenize` 接口如何把一个请求（chat / completion）变成
> `token_ids`，两者的 schema 差异，以及 `ucloud-kv-indexer` 如何把三种入站协议
> （OpenAI Chat / OpenAI Responses / Anthropic Messages）统一适配到这两个引擎的
> `/tokenize` 上——**不在本地 apply_chat_template**。
>
> 配套文档：[kv-events.md](./kv-events.md)（KV 事件 wire format 与差异）。
>
> 源码引用（相对仓库根 `/home/ubuntu/selfhost-schedular`）：
> - vLLM：`vllm/vllm/entrypoints/serve/tokenize/{protocol,serving,api_router}.py`、
>   `vllm/vllm/entrypoints/openai/responses/utils.py`
> - SGLang：`sglang/python/sglang/srt/entrypoints/openai/{serving_tokenize,serving_chat,protocol}.py`、
>   `sglang/python/sglang/srt/entrypoints/anthropic/serving.py`
> - indexer：`internal/normalize/normalize.go`、`internal/tokenizer/client.go`、
>   `internal/types/types.go`、`internal/httpapi/route.go`

---

## 1. 为什么 token_ids 必须来自引擎

KV-cache 的前缀复用是在 **token 序列** 上做的，而 token 序列 = `apply_chat_template(messages)`
之后再 tokenize 的结果。chat template 是**每个模型独有**的（system 放哪、tool 怎么渲染、
generation prompt、特殊 token……），只有引擎侧加载的 tokenizer 才是权威。

如果 indexer 自己实现一套 chat template，哪怕差一个空格或一个 BOS，算出的 token_ids 就
和引擎实际缓存的不一致，request_key 永远匹配不上。因此 indexer 的铁律是：

> **永远调用目标引擎的 `/tokenize`，绝不在本地 apply_chat_template。**

indexer 只做协议适配——把 Anthropic / Responses 的消息结构**改写**成 OpenAI-chat 的消息
结构，原样转发给引擎，由引擎应用权威模板。

---

## 2. vLLM 的 `/tokenize`

### 接口
- 路由：`POST /tokenize`（**无** `/v1` 前缀，`api_router.py:38`）
- 请求是 union：`TokenizeCompletionRequest | TokenizeChatRequest`（`protocol.py:156`）

### chat 形式 `TokenizeChatRequest`（`protocol.py:50-153`）
```jsonc
{
  "model": "...",
  "messages": [ ...OpenAI chat messages... ],   // 必填
  "add_generation_prompt": true,                // 默认 true
  "continue_final_message": false,              // 与 add_generation_prompt 互斥
  "add_special_tokens": false,                  // ⚠️ chat 默认 false（模板已加特殊 token）
  "chat_template": null,                        // 可覆盖模板
  "chat_template_kwargs": {},                   // 透传给模板（如 thinking）
  "tools": [ ... ],                             // OpenAI function tools
  "return_token_strs": false                    // 可要 token 字符串
}
```
**注意**：vLLM 的 `TokenizeChatRequest` **没有** `tool_choice` 字段，也没有
`reasoning_effort`（走 `chat_template_kwargs`）。

### 处理链路（`serving.py:55-122`）
```
create_tokenize(TokenizeChatRequest)
  └─ openai_serving_render.preprocess_chat(messages, chat_template, tools, ...)
        └─ 内部 apply_chat_template → engine_inputs（已是 token_ids）
  └─ 汇总 input_ids = Σ engine_input.token_ids
  └─ TokenizeResponse(tokens, count=len(tokens), max_model_len, token_strs?)
```

### 响应 `TokenizeResponse`（`protocol.py:159-163`）
```jsonc
{ "count": 42, "max_model_len": 8192, "tokens": [1,2,...], "token_strs": null }
```

---

## 3. SGLang 的 `/tokenize`

### 接口
- 路由：`POST /v1/tokenize` 与 `POST /tokenize`（新版本两者等价，`http_server.py:1526/1531`）；
  indexer 对 SGLang chat tokenization 默认使用 `/v1/tokenize`。
- **版本边界**：chat 形式的 `messages` 支持来自 SGLang `27445f9836`
  (`Add ChatCompletionRequest-style support to /v1/tokenize`, PR #23981)，进入 `v0.5.12+`。
  `v0.5.11` 及更老版本的 `TokenizeRequest` 只有 `prompt: Union[str, List[str]]` 必填。
- 请求是**单个** `TokenizeRequest`，新版本用 validator 强制 `prompt` / `messages` 二选一
  （`protocol.py:1152-1176`）

如果看到类似下面的错误：

```json
{"message":"1 validation error:\n  {'type':'missing','loc':('body','prompt'),'msg':'Field required', ...}"}
```

说明当前 SGLang 构建仍是 prompt-only `/tokenize` schema。indexer 不能把 chat 请求退化为
普通 `prompt` 字符串，因为那会要求本地复制目标模型的 chat template，算出的 token_ids 很容易
和引擎实际缓存的 KV 前缀不一致。正确处理是升级 SGLang 到 `v0.5.12+`，或使用包含
`27445f9836` / PR #23981 的 DeepSeek-V4 分支构建。

### chat 形式（`TokenizeRequest` with `messages`）
```jsonc
{
  "model": "...",
  "messages": [ ...OpenAI chat messages... ],
  "tools": [ ... ],
  "tool_choice": "auto",          // SGLang 有此字段（vLLM 没有）
  "reasoning_effort": "high",     // SGLang 有专门字段
  "continue_final_message": false,
  "chat_template_kwargs": {},
  "add_special_tokens": true       // ⚠️ 默认 true（与 vLLM chat 相反）
  // 没有 add_generation_prompt 字段：内部硬编码 add_generation_prompt=True
}
```

### 处理链路（`serving_tokenize.py:87-115`）
```
_tokenize_chat_request(TokenizeRequest)
  └─ to_chat_completion_request()  → ChatCompletionRequest
  └─ chat_serving._process_messages()
        └─ _apply_jinja_template / _apply_conversation_template
              └─ tokenizer.apply_chat_template(messages, tokenize=True,
                                                add_generation_prompt=True, tools=...)
  └─ prompt_ids（List[int]）
  └─ TokenizeResponse(tokens=prompt_ids, count, max_model_len)
```

### 引擎侧对 tool_calls 的再解析（重要）
OpenAI 协议里 `assistant.tool_calls[].function.arguments` 是 **JSON 字符串**。SGLang 在
`_apply_jinja_template`（`serving_chat.py:627-643`）会把它 `orjson.loads` **再解析回 dict**
后才喂给模板——这是 Transformers chat template 期望的形态。所以 indexer 发送时
**arguments 必须是 JSON 字符串**，引擎自己负责反解析，我们发的空白/转义在引擎侧被归一化掉。

---

## 4. 两个 `/tokenize` 的 schema 差异速查

| 维度 | vLLM | SGLang |
|---|---|---|
| 路由 | `/tokenize` | `/tokenize` 与 `/v1/tokenize` |
| 请求模型 | `TokenizeCompletionRequest` \| `TokenizeChatRequest`（互斥 union） | 单个 `TokenizeRequest` + validator |
| chat `add_special_tokens` 默认 | **`false`** | **`true`** ⚠️ |
| `add_generation_prompt` | 有（默认 true） | 无字段（内部恒 true） |
| `continue_final_message` | 有（与上互斥） | 有 |
| `tools` | 有 | 有 |
| `tool_choice` | **无** | 有 |
| `reasoning_effort` | 无（走 kwargs） | 有 |
| `return_token_strs` | 有 | 无 |
| 响应字段 | `count/max_model_len/tokens/token_strs` | `count/max_model_len/tokens` |
| detokenize 响应 | `{"prompt": "..."}` | `{"text": "..."}` |

> **`add_special_tokens` 默认值相反** 是唯一会造成绝对 token 数差异的点：vLLM chat 默认
> 不额外加 BOS，SGLang 默认加。但因为 indexer 的命中判断用的是“查询侧和 ingest 侧用
> **同一引擎端点** 算出的 request_key 链”，只要同一引擎自洽，**不影响命中**，只影响
> `input_tokens` 这个展示/policy 数值。详见 §6。

---

## 5. indexer 的统一适配：三协议 → OpenAI-chat → `/tokenize`

indexer 接受三种入站协议，全部 normalize 成 **结构化的 OpenAI-chat 消息 + function tools**，
然后通过 `tokenizer.TokenizeChat` 原样转发给引擎 `/tokenize`（chat 形式）。

```
入站请求 (chat / responses / anthropic)
   └─ normalize.From{OpenAIChat,OpenAIResponses,Anthropic}(raw)   internal/normalize/normalize.go
        → types.RouteRequest{ Messages []ChatMessage, Tools []any }
   └─ tokenizer.TokenizeChat(endpoint, model, Messages, Tools)    internal/tokenizer/client.go
        → POST {engine}/tokenize  {messages, tools, add_generation_prompt:true}
        ← {tokens, count, max_model_len}
   └─ residency.RequestKeysFromTokens(seed, tokens, blockSize)    internal/residency/index.go
        → request_key 链（FNV-64a chained hash，每 block_size 个 token 一段）
   └─ ix.Query(requestKeys) → 前缀命中
```

### 5.1 转换逻辑来源（按推理框架选择 adapter）

indexer 的转换器**直接照搬引擎自己的转换器**，保证产出的 messages 和引擎内部一致。

**关键事实：vLLM 和 SGLang 各自都有 Anthropic→chat 转换器，而且它们不一样。**

- vLLM：`vllm/entrypoints/anthropic/serving.py`
- SGLang：`sglang/srt/entrypoints/anthropic/serving.py`

两者在 system 拼接、thinking 处理、`tool_result` 落位、默认图片 media type、`tool_call_id`
回退规则上都有差异（见下表）。因此 indexer 不能只抄一家，而是用 **adapter 模式按引擎框架
选择**（`normalize.AdapterFor("vllm"|"sglang")`，`internal/normalize/normalize.go`）。
调用方先用请求里的 `model` 解析出 `ModelProfile.Framework`，再选对应 adapter，把请求转换成
**那个引擎**会缓存的形态，并发往**那个引擎**的 `/tokenize`。

| 协议 | 转换来源 | 框架相关？ |
|---|---|---|
| **OpenAI Chat** | 直通（已是目标形态） | 否（`baseAdapter`） |
| **OpenAI Responses** | 照搬 vLLM `responses/utils.py::construct_input_messages` | 否（两家一致，`baseAdapter`） |
| **Anthropic Messages** | 各自照搬 vLLM / SGLang 的 `anthropic/serving.py` | **是**（`vllmAdapter` / `sglangAdapter`） |

实现：`internal/normalize/anthropic.go`（共享 helper）、`anthropic_vllm.go`（`vllmAdapter`）、
`anthropic_sglang.go`（`sglangAdapter`）。包级 `FromAnthropic` 为向后兼容默认走 SGLang。

### 5.2 OpenAI Chat（直通）
已是目标形态，结构化直通：content 数组、`tool_calls`、tool-role 消息全部按
`types.ChatMessage`（`Content any`）原样保留，不扁平化。

### 5.3 Anthropic Messages → chat（按框架：`vllmAdapter` / `sglangAdapter`）

下表标注两家**一致**与**分歧**之处。一致项是核心结构映射；分歧项正是必须按框架选 adapter 的原因。

| Anthropic 输入 | OpenAI-chat 输出（一致部分） |
|---|---|
| `text` block | content part `{type:text,text}` |
| `image` block（base64 / url source） | `{type:image_url,image_url:{url}}`（base64 → `data:<mt>;base64,<data>`） |
| `tool_use` block（assistant） | assistant `tool_calls[]`，`arguments = JSON 字符串(input)` |
| `tools` / `tool_choice:none` | 转 function tools；`none` 时丢弃 tools（见 §5.5） |

| 分歧维度 | **vLLM**（`vllmAdapter`） | **SGLang**（`sglangAdapter`） |
|---|---|---|
| `system`（多 block） | 直接 **拼接（无分隔符）**，并剥离 `x-anthropic-billing-header` block | 用 `\n` 连接 text block |
| `thinking` / `redacted_thinking` | 写入 `reasoning` 字段（**保留**） | **丢弃**（不进模板） |
| user 角色 `tool_result` | 拆成**多条**消息：`{role:tool}` 文本 + 独立 `{role:user}` 图片 + 独立 `{role:tool}` tool_reference | **单条** `{role:tool}`，content 可为部件数组（text+image+tool_reference 混合） |
| `tool_call_id` 回退 | `tool_use_id ‖ ""`（**不**回退到 `id`） | `tool_use_id ‖ id` |
| 默认图片 media type | `image/jpeg` | `image/png` |
| assistant 角色 `tool_result` | inline `{type:text,text:"Tool result: ..."}` | inline `{type:text,text:"Tool result: ..."}`（一致） |
| 生成的 `tool_use` id | 确定性计数器 `call_N`（见 §5.5） | 确定性计数器 `call_N`（见 §5.5） |

### 5.4 OpenAI Responses → chat（`normalize.go::FromOpenAIResponses`）
| Responses 输入 | OpenAI-chat 输出 |
|---|---|
| `instructions` | 首条 `{role:system}` |
| `input` 为 string | `{role:user, content}` |
| `input` item：role+content（含 input_text/input_image/input_file 部件） | 对应 role 消息，content 结构化保留 |
| `input` item：`function_call` | assistant `tool_calls[]`，**arguments 原样透传**（已是 JSON 字符串，不要再编码） |
| 连续多个 `function_call` | 合并进同一条 assistant 的 `tool_calls` |
| `input` item：`function_call_output` | `{role:tool, tool_call_id, content}` |
| `input` item：`reasoning` | 丢弃 |
| `tools`（扁平 `{type,name,description,parameters}`） | 嵌套成 `{type:function, function:{...}}` |

### 5.5 两个刻意的取舍（与引擎转换器的偏差）
1. **不转发 `tool_choice`**：它是 grammar/sampling 约束，**不改变渲染出的 prompt**——
   唯一例外是 `"none"`，会让引擎跳过渲染 tools（`serving_chat.py:477`：`request.tools and
   request.tool_choice != "none"`）。所以三个转换器在 `tool_choice == "none"` 时**丢弃
   tools**，其余情况不转发 `tool_choice`，避免把 Responses 形态的 `tool_choice` 发给
   SGLang 触发校验错误 → tokenize 失败 → fallback。
2. **生成的 tool-call id 用确定性计数器**（`call_0` / `function_call_0` …），而非 SGLang
   的 `uuid4().hex`。KV-cache indexer **必须**让相同请求 tokenize 出相同结果，随机 id 会
   破坏这一点（同一请求两次算出不同 token → request_key 不稳）。

### 5.6 client 发送形态（`client.go`）
`chatRequest` 已携带 `Messages []types.ChatMessage` + `Tools []any` +
`add_generation_prompt:true`。`json.Marshal` 会自动序列化扩展后的 `ChatMessage`
（`tool_calls`、`tool_call_id`，arguments 为 JSON 字符串）。client 会按框架选择路径：
vLLM 走 `/tokenize`，SGLang 走 `/v1/tokenize`。payload 同时满足 vLLM 和 SGLang `v0.5.12+`
的 chat schema（两者都接受 `messages` / `tools` / `add_generation_prompt`；SGLang 独有的
`tool_choice` 我们不发，vLLM 没有的字段我们也不发）。

---

## 6. token 数差异 vs 命中判断（关键澄清）

`add_special_tokens` 默认值在两引擎相反，会让**同一段文本**在 vLLM 和 SGLang 算出**不同
的绝对 token 数**（差一个 BOS 等）。这对 indexer 的影响要分开看：

- **命中判断**（request_key 匹配）：**不受影响**。因为：
  - ingest 侧的 token_ids 来自该引擎自己发的 `BlockStored.token_ids`；
  - 查询侧的 token_ids 来自调用**同一引擎**的 `/tokenize`；
  - 两侧用同一 tokenizer、同一 namespace seed、同一 chained-hash 算法
    （`residency.RequestKeysFromTokens`），只要引擎自洽，request_key 就一致。
- **`input_tokens` 数值**（用于 long-prompt 阈值、展示）：**会因引擎而异**。如果将来同一
  model 同时挂 vLLM 和 SGLang 后端（多 region），同一 prompt 的 `input_tokens` 可能差几
  个 token，阈值边界上可能判定不同。

> 可选增强：让 `client.go` 在 chat tokenize 时**显式发送 `add_special_tokens`**，固定两
> 引擎行为，消除多 region 混合后端下的 `input_tokens` 抖动。当前未做（依赖各自默认值）。

---

## 7. 端到端示例（Anthropic 多轮 tool 流）

入站 Anthropic：
```jsonc
{
  "model": "qwen3.5-4b", "max_tokens": 64, "system": "be brief",
  "messages": [
    {"role":"user","content":[
      {"type":"text","text":"weather in SF?"},
      {"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}
    ]},
    {"role":"assistant","content":[
      {"type":"tool_use","id":"tu1","name":"get_weather","input":{"city":"SF"}}
    ]},
    {"role":"user","content":[
      {"type":"tool_result","tool_use_id":"tu1","content":"sunny"}
    ]}
  ],
  "tools": [{"name":"get_weather","description":"w","input_schema":{"type":"object"}}]
}
```

indexer normalize 后发给引擎 `/tokenize` 的 body（结构化，未扁平化）：
```jsonc
{
  "model": "qwen3.5-4b",
  "add_generation_prompt": true,
  "messages": [
    {"role":"system","content":"be brief"},
    {"role":"user","content":[
      {"type":"text","text":"weather in SF?"},
      {"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}
    ]},
    {"role":"assistant","tool_calls":[
      {"id":"tu1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}}
    ]},
    {"role":"tool","tool_call_id":"tu1","content":"sunny"}
  ],
  "tools": [{"type":"function","function":{"name":"get_weather","description":"w","parameters":{"type":"object"}}}]
}
```

引擎应用权威 chat template → 返回 `{tokens, count, max_model_len}` → indexer 用 tokens 算
request_key 链 → 查 residency index → 命中判断。该流程在
`internal/tokenizer/client_test.go` 有针对 Anthropic / Responses 的端到端断言（验证引擎
确实收到结构化 messages、tool_calls、tool 消息，而非扁平字符串）。

---

## 8. 适配兼容性速查

| 维度 | vLLM | SGLang | indexer |
|---|---|---|---|
| chat `/tokenize` 接受结构化 messages | ✅ | ✅（SGLang `v0.5.12+` / PR #23981） | ✅ 直接转发 |
| `messages` / `tools` / `add_generation_prompt` | ✅ | ✅ | ✅ client 发这三项 |
| `tool_choice` | 无 | 有 | 不发（除 `none` 丢 tools）|
| tool_calls.arguments 为 JSON 字符串 | ✅ 引擎再解析 | ✅ 引擎再解析 | ✅ Anthropic 编码 / Responses 透传 |
| `add_special_tokens` 默认 | false | true | 不发，依赖默认（见 §6）|
| 多模态 content 数组 | ✅ | ✅ | ✅ 保留结构（`HasMultimodalContent` 可检测）|
| 响应 `count` 字段 | ✅ | ✅ | ✅ 优先取 count |
