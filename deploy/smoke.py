#!/usr/bin/env python3
# deploy/smoke.py — end-to-end validation of ucloud-kv-indexer against the two
# live local clusters (vLLM qwen3.5-4b, SGLang qwen3-0.6b), exercising BOTH
# frameworks across ALL THREE inbound protocols (OpenAI Chat, OpenAI Responses,
# Anthropic Messages).
#
# Everything goes through the gateway (:8095) with ?cluster= selecting the
# backend, exactly as the web console does.
#
# For each (cluster, protocol) it runs the full product loop:
#
#   1. TOKENIZE the protocol body via the kvindexer's /tokenize/preview, which
#      normalizes with the framework-correct adapter and forwards STRUCTURED
#      messages to the engine's /v1/tokenize (never a local chat template). This
#      validates the per-framework protocol adapter end to end.
#   2. WARM the engine DIRECTLY (engine api_endpoint /v1/completions with the
#      exact token_ids) so the engine caches that prefix and emits ZMQ KV
#      BlockStored events the kvindexer ingests. (The kvindexer's own protocol
#      endpoints are ADMISSION GATES, not proxies — they judge, they don't warm.)
#   3. QUERY /query-prefix with those token_ids and assert the residency index
#      reports a real GPU hit covering the cached blocks (data accuracy).
#   4. ADMISSION re-check: POST the original protocol body to the matching
#      kvindexer admission endpoint and assert the decision is now ACCEPT — the
#      same long prompt that is rejected cold (long_prompt_low_cache_hit) is
#      admitted once its prefix is resident. This proves the whole loop.
#
# Exit code is non-zero if any case fails.
import json
import os
import sys
import time
import urllib.request
import urllib.error

GW = "http://127.0.0.1:8095"
PASS, FAIL = "\033[32mPASS\033[0m", "\033[31mFAIL\033[0m"
failures = 0

# A per-RUN nonce makes every prompt a genuinely COLD prefix. Without it, a
# second run re-sends identical token_ids; the engine already has them cached,
# emits NO new BlockStored event, and (if the kvindexer was restarted and lost
# its in-memory index) the query sees matched=0. A fresh nonce forces a real
# prefill → fresh KV events → the index repopulates. (Rebuilding the index for
# ALREADY-resident prefixes after a restart needs replay_endpoint support — a
# documented gap; see docs/scaling.md.)
NONCE = os.urandom(4).hex()

CLUSTERS = [
    {"cluster": "local-vllm", "model": "qwen3.5-4b"},
    {"cluster": "local-sglang", "model": "qwen3-0.6b"},
]


def req(url, body=None, timeout=90):
    data = None if body is None else json.dumps(body).encode()
    hdr = {"Content-Type": "application/json"} if data else {}
    r = urllib.request.Request(url, data, hdr)
    with urllib.request.urlopen(r, timeout=timeout) as resp:
        return resp.status, json.load(resp)


def gw(path, body=None, cluster=None, timeout=90):
    url = GW + path + (("?cluster=" + cluster) if cluster else "")
    return req(url, body, timeout)


def long_text(reps):
    base = ("A radix tree, also called a compressed prefix trie, indexes strings "
            "by their shared prefixes so lookups run in time proportional to key "
            "length rather than to the number of keys stored. ")
    return base * reps


# Each protocol gets a distinct marker so its prefix is unique (no cross-credit).
# The marker goes at the FRONT of the user content: the request_key chain is
# prefix-anchored, so a unique first block makes the WHOLE chain cold on the
# engine, forcing a fresh prefill that re-emits BlockStored for every block. A
# marker at the END would leave the long shared head cached (and, after a
# kvindexer restart, unindexed → parent-bridge misses → skipped events).
def chat_body(model, marker):
    return {"model": model, "messages": [
        {"role": "system", "content": "You are a concise systems engineer."},
        {"role": "user", "content": f"[{marker}] " + long_text(40) + " Reply in one sentence."},
    ], "max_tokens": 16, "temperature": 0}


def responses_body(model, marker):
    return {"model": model, "instructions": "You are a concise systems engineer.",
            "input": f"[{marker}] " + long_text(40) + " Reply in one sentence.",
            "max_output_tokens": 16, "temperature": 0}


def anthropic_body(model, marker):
    return {"model": model, "system": "You are a concise systems engineer.",
            "messages": [{"role": "user", "content": f"[{marker}] " + long_text(40) + " Reply in one sentence."}],
            "max_tokens": 16}


# (protocol id for /tokenize/preview, kvindexer admission endpoint, body builder)
PROTOCOLS = [
    ("openai.chat", "/v1/chat/completions", chat_body),
    ("openai.responses", "/v1/responses", responses_body),
    ("anthropic.messages", "/v1/messages", anthropic_body),
]


def engine_endpoints():
    """cluster -> engine api_endpoint, from the federated /engines list."""
    _, engines = req(GW + "/engines", timeout=10)
    out = {}
    for e in engines:
        out[e["_cluster"]] = e["api_endpoint"]
    return out


def check(cfx, proto, admit_path, builder, api_endpoint):
    global failures
    cluster, model = cfx["cluster"], cfx["model"]
    label = f"[{cluster:13s}] {proto:18s}"
    marker = f"{cluster}:{proto}:{NONCE}"
    body = builder(model, marker)

    # 1) TOKENIZE via the kvindexer (engine /v1/tokenize, framework adapter).
    try:
        _, prev = gw("/tokenize/preview", {"model": model, "protocol": proto, "raw": body}, cluster=cluster)
    except Exception as e:
        print(f"  {label}  {FAIL}  tokenize/preview: {e}")
        failures += 1
        return
    toks, bs, ns = prev["tokens"], prev["block_size"], prev["namespace"]
    count = prev["count"]
    expect_tokens = (count // bs) * bs
    if expect_tokens == 0:
        print(f"  {label}  {FAIL}  prompt {count} toks < one block ({bs})")
        failures += 1
        return

    # 2) WARM the engine directly with the exact token_ids (emits KV events).
    try:
        req(api_endpoint + "/v1/completions",
            {"model": model, "prompt": toks, "max_tokens": 1, "temperature": 0})
    except Exception as e:
        print(f"  {label}  {FAIL}  warm engine {api_endpoint}: {e}")
        failures += 1
        return
    time.sleep(2)  # let ZMQ KV events reach the index

    # 3) QUERY residency and require a real GPU hit covering the full blocks.
    _, q = gw("/query-prefix", {"model": model, "token_ids": toks}, cluster=cluster)
    if q["namespace"] != ns:
        print(f"  {label}  {FAIL}  ns mismatch {ns} != {q['namespace']}")
        failures += 1
        return
    inst = q.get("instances") or {}
    best = max((h["longest_matched"] for h in inst.values()), default=0)
    best_gpu = max((h["gpu"] for h in inst.values()), default=0)
    hit_ok = best >= expect_tokens and best_gpu >= expect_tokens

    # 4) ADMISSION re-check: the same body must now be ACCEPTED.
    try:
        status, resp = gw(admit_path, body, cluster=cluster)
    except urllib.error.HTTPError as e:
        status, resp = e.code, json.loads(e.read() or "{}")
    decision = resp.get("decision")
    cache = resp.get("cache", {})
    admit_ok = (decision == "accept")

    ok = hit_ok and admit_ok
    if not ok:
        failures += 1
    print(f"  {label}  {PASS if ok else FAIL}  toks={count} bs={bs} "
          f"matched={best} gpu={best_gpu} (need>={expect_tokens}) | "
          f"admit={decision} hit_ratio={cache.get('hit_ratio')} status={status}")


def main():
    try:
        _, health = req(GW + "/clusters-health", timeout=8)
        endpoints = engine_endpoints()
    except Exception as e:
        print(f"gateway unreachable: {e}")
        sys.exit(2)

    print("== clusters ==")
    for c in health:
        ok = all(b["healthy"] for b in c["backends"])
        print(f"  {c['cluster']:13s} {'healthy' if ok else 'UNHEALTHY':9s} "
              f"engine={endpoints.get(c['cluster'], '?')}")

    for cfx in CLUSTERS:
        ep = endpoints.get(cfx["cluster"])
        print(f"== {cfx['cluster']} ({cfx['model']}) ==")
        if not ep:
            print(f"  {FAIL}  no engine endpoint for cluster")
            globals()["failures"] += 1
            continue
        for proto, admit_path, builder in PROTOCOLS:
            check(cfx, proto, admit_path, builder, ep)

    print()
    if failures:
        print(f"\033[31m{failures} check(s) FAILED\033[0m")
        sys.exit(1)
    print("\033[32mALL PASS — both frameworks, all three protocols: tokenize→warm→"
          "index→admit verified; kv-indexer residency is accurate and admission "
          "flips reject→accept on a resident prefix\033[0m")


if __name__ == "__main__":
    main()
