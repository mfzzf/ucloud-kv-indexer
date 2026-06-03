#!/usr/bin/env bash
# Serve qwen3.5-4b on vLLM for the `local-vllm` cluster.
#
# Binds HTTP + ZMQ to 127.0.0.1 ONLY (this box has a public IP; never expose an
# inference engine or its KV-event stream to the internet — the firewall is a
# backstop, loopback binding is the primary defense).
#
#   HTTP API + tokenizer : http://127.0.0.1:8000   (--host 127.0.0.1)
#   KV events (ZMQ PUB)  : tcp://*:5559            (kvindexer connects to :5559)
#   KV events replay     : tcp://127.0.0.1:5560    (ROUTER, gap recovery)
#
# WHY the PUB endpoint is tcp://*:5559 and not tcp://127.0.0.1:5559:
# vLLM's ZmqEventPublisher (vllm/distributed/kv_events.py) BINDS the PUB socket
# only when the endpoint contains a wildcard ("*" / "::" / ipc://); for any other
# address it CONNECTS instead (and a PUB that connects to nothing never listens,
# so the kvindexer can't subscribe). So the publisher must bind with "*". The
# replay ROUTER always binds(), so tcp://127.0.0.1:5560 correctly stays on
# loopback. The "*" PUB binds on all interfaces, but the host firewall (ufw
# deny-incoming, see README "Security") blocks :5559 from the public IP, and the
# kvindexer connects over loopback (tcp://127.0.0.1:5559).
#
# qwen3.5-4b is a hybrid (Mamba + full-attention) model; --mamba-cache-mode align
# keeps the full-attention KV blocks page-aligned so prefix caching + KV events
# work. The full_attention group block size is 528 (this is what the kvindexer's
# vllm-v1-text profile must use — see deploy/config.local.yaml).
set -euo pipefail

ROOT=/home/ubuntu/selfhost-schedular
MODEL="$ROOT/models/qwen3.5-4b"
VENV="$ROOT/.venv-vllm"

source "$VENV/bin/activate"
export LD_LIBRARY_PATH="$VENV/lib/python3.11/site-packages/nvidia/cu13/lib:${LD_LIBRARY_PATH:-}"
export PYTHONHASHSEED=0   # stable namespace hashing across restarts

exec vllm serve "$MODEL" \
  --served-model-name qwen3.5-4b \
  --host 127.0.0.1 \
  --port 8000 \
  --trust-remote-code \
  --dtype bfloat16 \
  --max-model-len 8192 \
  --gpu-memory-utilization 0.60 \
  --max-num-seqs 4 \
  --enable-prefix-caching \
  --mamba-cache-mode align \
  --kv-events-config '{"publisher":"zmq","enable_kv_cache_events":true,"endpoint":"tcp://*:5559","topic":"kv-events","replay_endpoint":"tcp://127.0.0.1:5560"}'
