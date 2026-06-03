"""Multi-turn chat that demonstrates KV-cache prefix hits through the kvindexer gate.

Topology (two separate services — the whole point of this demo):

    LangChain (ChatOpenAI)
        │
        │  1. ask the GATE: "should this request be admitted?"
        ▼
    kvindexer  :8090   ──►  admission decision only (accept / 429)
        │                    {decision, reason, cache:{hit_ratio, best_hit_tokens...}, target}
        │                    It JUDGES on prompt length + prefix-cache hit ratio.
        │                    It does NOT generate text.
        │  2. if accepted, generate at the engine the gate picked (target.endpoint)
        ▼
    SGLang     :30000  ──►  the actual chat completion (qwen3.5-4b)
        │                    SGLang prefills the prompt -> emits KV-cache events over ZMQ
        ▼                    -> kvindexer ingests them -> next turn's prefix is "resident"

What you should SEE as the conversation grows:

  - Turn 1 is cold (a per-run nonce makes the system prompt unseen), so best_hit
    is small and the gate admits it as an ordinary short prompt.
  - Every later turn re-sends [system + all prior turns] as its prefix. SGLang
    already prefilled and cached that prefix while generating the previous turn,
    so `best_hit_tokens` climbs and `hit_ratio` stays high. Once the running
    prompt crosses the long-prompt threshold (1024 tokens) the gate's reason
    flips to `long_prompt_high_cache_hit` — admitted *because* it hit cache.

  - ACT 2 (the control): we take a brand-new long conversation of similar size
    that SGLang has never seen. Same length, but cold -> the gate returns 429
    `long_prompt_low_cache_hit`. This proves the gate isn't rubber-stamping by
    length; cache residency is what earns admission.

Run:
    /home/ubuntu/selfhost-schedular/.venv-langchain/bin/python \
        ucloud-kv-indexer/examples/langchain_multiturn_cache.py
"""
from __future__ import annotations

import json
import os
import time
import urllib.error
import urllib.request

from langchain_core.messages import AIMessage, HumanMessage, SystemMessage
from langchain_openai import ChatOpenAI

# --- live config (matches the running services; verified against /engines + /policies) ---
GATE = os.environ.get("GATE_URL", "http://127.0.0.1:8090")
ENGINE = os.environ.get("ENGINE_URL", "http://127.0.0.1:30000")
MODEL = os.environ.get("MODEL", "qwen3.5-4b")
LONG_THRESHOLD = 1024  # local-default policy: prompts >= this are "long" and must hit cache
EVENT_SETTLE_S = 2.0   # let SGLang's KV events propagate to kvindexer before the next gate query

# A per-run nonce makes this run's prefixes COLD (the index builds state forward from the
# live event stream; an already-cached prefix emits no new events, so it would read ratio=0
# on a fresh run). With the nonce, turn 1 is genuinely cold and the climb is observable.
RUN = f"{os.getpid()}-{int(time.time())}"
SYSTEM = (
    f"[session {RUN}] You are a precise senior systems engineer helping design a "
    "KV-cache-aware LLM serving stack. Answer in at most three sentences, concretely."
)

# Each user turn carries enough real context that the running prompt crosses the 1024-token
# long-prompt threshold within a few turns — so we can watch the gate's reason flip from
# `ordinary_short_prompt` to `long_prompt_high_cache_hit` without padding.
USER_TURNS = [
    "We're serving qwen3.5-4b on a single 24GB GPU with SGLang, and we publish KV-cache "
    "events over ZMQ. I want to admit or reject requests based on prompt length and prefix "
    "cache-hit rate. As a first step, what signal best predicts that a long prompt will be "
    "cheap to prefill, and why is the resident KV prefix the right thing to measure?",

    "Good. Now suppose a request's prompt is 4000 tokens but 3500 of those tokens are already "
    "resident in the KV cache from earlier turns of the same conversation. Walk me through why "
    "admitting it is far cheaper than a cold 4000-token prompt, in terms of prefill FLOPs and "
    "time-to-first-token, and what hit-ratio threshold you'd start with.",

    "Makes sense. In a multi-turn chat, each new turn re-sends the entire history as its prefix. "
    "Explain precisely why the gate sees the hit ratio climb turn over turn, and what specifically "
    "the engine must emit after each generation for the very next turn to score as a cache hit.",

    "Last design question: if the event stream stalls or the listener disconnects, what should "
    "the admission policy do with a long, low-apparent-hit prompt — reject it as a miss, or fall "
    "back to accept? Justify the safe default and how you'd detect staleness reliably.",

    "Now let's pressure-test the numbers. With a 1024-token long-prompt threshold and a 0.5 "
    "minimum hit ratio, work through a worked example: a 2000-token prompt with 1300 cached "
    "tokens versus a 2000-token cold prompt. Which is admitted, which is rejected, and roughly "
    "how much prefill compute does admitting the warm one save relative to the cold one?",

    "Finally, summarize the end-to-end control loop in order: a client sends a multi-turn "
    "request, the gate tokenizes via the engine and scores the resident prefix, it admits or "
    "rejects, the engine generates and emits fresh KV-cache events, and the indexer ingests them "
    "so the next turn scores higher. Call out the one place a bug would silently cause spurious "
    "429s, and how you'd catch it in monitoring.",
]


def _post(url: str, body: dict, timeout: int = 120):
    data = json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method="POST",
                                 headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read())
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"_raw": raw.decode(errors="replace")}


def lc_to_openai(messages) -> list[dict]:
    """Serialize LangChain messages into OpenAI chat `messages` (what both the gate and
    the engine consume). The gate tokenizes this via the engine's /tokenize, never locally."""
    role = {"system": "system", "human": "user", "ai": "assistant"}
    return [{"role": role[m.type], "content": m.content} for m in messages]


def ask_gate(messages, max_tokens: int) -> tuple[int, dict]:
    """Step 1: admission judgment. Returns (http_status, RouteResponse)."""
    body = {"model": MODEL, "max_tokens": max_tokens, "messages": lc_to_openai(messages)}
    return _post(f"{GATE}/v1/chat/completions", body)


# Step 2: generation happens on the engine the gate chose. We point ChatOpenAI at the
# gate's `target.endpoint` to make the "gate decides where, client generates there" flow literal.
def make_llm(endpoint: str, max_tokens: int) -> ChatOpenAI:
    return ChatOpenAI(
        base_url=f"{endpoint}/v1",
        api_key="EMPTY",
        model=MODEL,
        temperature=0,
        max_tokens=max_tokens,
        timeout=120,
        # qwen3.5 is a thinking model; disable the <think> preamble for clean demo output.
        extra_body={"chat_template_kwargs": {"enable_thinking": False}},
    )


def fmt_decision(status: int, resp: dict) -> str:
    c = resp.get("cache", {}) or {}
    ratio = c.get("hit_ratio", 0) or 0
    line = (f"HTTP {status}  {resp.get('decision','?').upper():7} "
            f"{resp.get('reason',''):28} "
            f"tokens={c.get('input_tokens','?'):>5}  "
            f"best_hit={c.get('best_hit_tokens','?'):>5}  "
            f"hit_ratio={ratio:5.1%}")
    tgt = resp.get("target") or {}
    if tgt.get("engine_id"):
        line += f"  -> {tgt['engine_id']}"
    return line


def banner(text: str):
    print("\n" + "=" * 78)
    print(text)
    print("=" * 78)


def run_conversation():
    banner(f"ACT 1 — multi-turn conversation (run nonce {RUN}); watch hit_ratio climb")
    history = [SystemMessage(SYSTEM)]
    summary = []

    for i, user_text in enumerate(USER_TURNS, 1):
        gen_budget = 160
        history.append(HumanMessage(user_text))

        # --- Step 1: ask the gate whether to admit this (growing) prompt ---
        status, resp = ask_gate(history, max_tokens=gen_budget)
        cache = resp.get("cache", {}) or {}
        is_long = (cache.get("input_tokens", 0) or 0) >= LONG_THRESHOLD
        print(f"\nTurn {i}  [{'LONG' if is_long else 'short'} prompt]")
        print("  gate :", fmt_decision(status, resp))
        summary.append((i, cache.get("input_tokens", 0), cache.get("best_hit_tokens", 0),
                        cache.get("hit_ratio", 0) or 0, resp.get("reason", "")))

        if status != 200 or resp.get("decision") != "accept":
            print("  -> REJECTED by gate; not generating. (reason:", resp.get("reason"), ")")
            history.pop()  # don't keep a turn we never completed
            continue

        # --- Step 2: gate accepted -> generate on the engine it picked ---
        endpoint = (resp.get("target") or {}).get("endpoint") or ENGINE
        llm = make_llm(endpoint, max_tokens=gen_budget)
        reply = llm.invoke(history)
        history.append(AIMessage(reply.content))
        u = reply.usage_metadata or {}
        print(f"  engine: generated {u.get('output_tokens','?')} tok "
              f"(prompt {u.get('input_tokens','?')} tok)  via {endpoint}")
        snippet = reply.content.strip().replace("\n", " ")
        print(f"  assistant: {snippet[:140]}{'…' if len(snippet) > 140 else ''}")

        # Let SGLang's KV-cache events for the prefix we just prefilled reach the indexer,
        # so the NEXT turn's prefix scores as resident.
        time.sleep(EVENT_SETTLE_S)

    banner("ACT 1 summary — best_hit and hit_ratio climb as the cached prefix grows")
    print(f"{'turn':>4} {'input_tok':>10} {'best_hit':>9} {'hit_ratio':>10}  reason")
    for i, inp, best, ratio, reason in summary:
        print(f"{i:>4} {inp:>10} {best:>9} {ratio:>9.1%}  {reason}")
    return history


def run_cold_control():
    """ACT 2: a brand-new long conversation SGLang has never seen. Same kind of content,
    similar size, but COLD -> the gate must reject it with long_prompt_low_cache_hit.
    This is the contrast that proves cache residency (not length) is what earns admission."""
    banner("ACT 2 — control: an UNSEEN long prompt of similar size must be 429'd")
    cold_nonce = f"COLD-{os.getpid()}-{int(time.time())}"
    cold_user = (
        f"[unseen {cold_nonce}] Evaluate this previously-unseen design for a distributed "
        "KV-cache-aware admission controller that scores prefix locality and prompt length "
        "to accept or reject inbound requests, listening to engine cache events over ZMQ "
        "and maintaining a dual-key residency index across GPU, host, and disk tiers. "
    ) * 18  # well over the 1024-token long threshold, but never prefilled -> cold
    msgs = [SystemMessage(f"[session {cold_nonce}] You are a precise systems engineer."),
            HumanMessage(cold_user)]
    status, resp = ask_gate(msgs, max_tokens=64)
    print("  gate :", fmt_decision(status, resp))
    ok = status == 429 and resp.get("reason") == "long_prompt_low_cache_hit"
    print(f"  -> {'EXPECTED 429 (cold long prompt rejected)' if ok else 'UNEXPECTED — see above'}")


if __name__ == "__main__":
    print(f"gate   = {GATE}   (admission judge — returns accept/429, never text)")
    print(f"engine = {ENGINE} (SGLang qwen3.5-4b — the actual generator)")
    run_conversation()
    run_cold_control()
    print("\nDone. The same growing conversation is admitted because its prefix is cache-resident;")
    print("an equally-long but unseen prompt is rejected. That is the gate doing its one job.")
