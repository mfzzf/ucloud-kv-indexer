# examples — kvindexer demos

## `langchain_multiturn_cache.py`

A LangChain (`langchain-openai` v1) multi-turn chat that demonstrates KV-cache
**prefix hits** through the kvindexer admission gate.

### The two-service architecture this demonstrates

```
LangChain ChatOpenAI
   │  1. ask the GATE: admit or reject?
   ▼
kvindexer  :8090   → admission decision ONLY (accept / 429 + cache hit_ratio).
   │                 It JUDGES (length + prefix-cache hit). It never generates text.
   │  2. if accepted, generate on the engine the gate picked (target.endpoint)
   ▼
SGLang     :30000  → the actual chat completion (qwen3.5-4b).
                     Prefilling emits KV-cache events → kvindexer ingests them →
                     the next turn's prefix is "resident" and scores a hit.
```

The gate is **not** an OpenAI server — calling `:8090/v1/chat/completions` returns a
`{decision, reason, cache:{hit_ratio,...}, target}` body, not a completion. The demo
makes the flow literal: it asks the gate, and on `accept` it points `ChatOpenAI` at the
engine endpoint the gate returned.

### What you see

**ACT 1** — one conversation, four-plus turns. Each turn re-sends `[system + history]`
as its prefix; SGLang already cached that prefix while generating the previous turn, so
`best_hit_tokens` / `hit_ratio` climb every turn:

```
turn  input_tok  best_hit  hit_ratio  reason
   1        148        10      6.8%  ordinary_short_prompt
   2        345       146     42.3%  ordinary_short_prompt
   3        548       343     62.6%  ordinary_short_prompt
   4        700       546     78.0%  ordinary_short_prompt
   5        878       698     79.5%  ordinary_short_prompt
   6       1136       876     77.1%  long_prompt_high_cache_hit   ← long, admitted *because* it hit cache
```

**ACT 2** (control) — a brand-new, **equally long but unseen** prompt. Same size,
cold → the gate returns `429 long_prompt_low_cache_hit` (hit_ratio ~0.4%). Same length,
opposite verdict: cache residency, not length, is what earns admission.

### Run

Services must be up first (they already are in this environment):
- kvindexer gate on `:8090` (engine `sglang-qwen-0` registered → `:30000`)
- SGLang serving `qwen3.5-4b` on `:30000` with KV events on `tcp://*:5557`

```bash
/home/ubuntu/selfhost-schedular/.venv-langchain/bin/python \
    ucloud-kv-indexer/examples/langchain_multiturn_cache.py
```

Override via env: `GATE_URL`, `ENGINE_URL`, `MODEL`.

### Notes / gotchas baked into the script

- **Per-run nonce in the system prompt.** The residency index builds state *forward*
  from the live event stream; an already-cached prefix emits no new events, so without a
  nonce a re-run would read `hit_ratio=0` on turn 1. The nonce makes turn 1 genuinely
  cold so the climb is real and observable.
- **`enable_thinking:false`** is passed via `extra_body.chat_template_kwargs` because
  qwen3.5 is a thinking model — keeps the demo output clean.
- **`EVENT_SETTLE_S` sleep** between turns lets SGLang's KV events propagate to the
  indexer before the next turn queries the gate.
- The long-prompt threshold (1024) and min hit ratio (0.5) are the live `local-default`
  policy values; ACT 1 is engineered to cross 1024 by the final turn while still hitting
  cache, so you witness the `ordinary_short_prompt` → `long_prompt_high_cache_hit` flip.

### Dependencies

Installed in a dedicated venv `/home/ubuntu/selfhost-schedular/.venv-langchain`:
`langchain-openai 1.2.2`, `langchain-core 1.4.0`, `openai 2.40.0`.
