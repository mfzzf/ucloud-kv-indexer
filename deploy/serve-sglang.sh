#!/usr/bin/env bash
# Serve qwen3-0.6b on SGLang for the `local-sglang` cluster.
#
# Binds HTTP + ZMQ to 127.0.0.1 ONLY (public-IP box; never expose an engine).
#
#   HTTP API + tokenizer : http://127.0.0.1:30000  (--host 127.0.0.1)
#   KV events (ZMQ PUB)  : tcp://*:5557            (kvindexer connects to :5557)
#   KV events replay     : tcp://127.0.0.1:5558    (ROUTER, gap recovery)
#
# WHY the PUB endpoint is tcp://*:5557 and not tcp://127.0.0.1:5557:
# SGLang's ZmqEventPublisher (sglang/srt/disaggregation/kv_events.py) uses the
# same heuristic as vLLM: it BINDS the PUB socket only when the endpoint contains
# a wildcard ("*" / "::" / ipc://), otherwise it CONNECTS (and never listens). So
# the publisher must bind with "*". The replay ROUTER always binds(), so
# tcp://127.0.0.1:5558 stays on loopback. The "*" PUB binds on all interfaces but
# the host firewall (ufw deny-incoming) blocks :5557 from the public IP; the
# kvindexer connects over loopback (tcp://127.0.0.1:5557).
#
# qwen3-0.6b is a standard dense-attention Qwen3 (not hybrid), so SGLang uses a
# normal paged KV cache. --page-size 64 fixes the KV-event block size to 64,
# which the kvindexer's sglang-v1-text profile must match (deploy/config.local.yaml).
# A small model deliberately co-resides with the 4B vLLM engine on one 24GB GPU.
set -euo pipefail

ROOT=/home/ubuntu/selfhost-schedular
VENV="$ROOT/.venv"
# Qwen3-0.6B lives in the HF cache; resolve its snapshot dir.
MODEL=$(ls -d /home/ubuntu/.cache/huggingface/hub/models--Qwen--Qwen3-0.6B/snapshots/*/ | head -1)

source "$VENV/bin/activate"
export PYTHONHASHSEED=0

exec python -m sglang.launch_server \
  --model-path "$MODEL" \
  --served-model-name qwen3-0.6b \
  --host 127.0.0.1 \
  --port 30000 \
  --trust-remote-code \
  --dtype bfloat16 \
  --context-length 8192 \
  --mem-fraction-static 0.20 \
  --page-size 64 \
  --attention-backend triton \
  --sampling-backend pytorch \
  --reasoning-parser qwen3 \
  --kv-events-config '{"publisher":"zmq","topic":"kv-events","endpoint":"tcp://*:5557","replay_endpoint":"tcp://127.0.0.1:5558"}'
