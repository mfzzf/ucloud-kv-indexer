# ucloud-kv-indexer — local dev stack (TWO co-resident clusters on one box)
#
# Topology:  frontend (:3000) ─▶ gateway (:8095) ─▶ kvindexer local-vllm   (:8090)
#                                              └────▶ kvindexer local-sglang (:8091)
#
# Each kvindexer sits next to ONE inference engine and SUBs its ZMQ KV-event
# stream locally; the gateway federates both for the console. Everything binds
# 127.0.0.1 ONLY (this box has a public IP — never expose a port).
#
#   local-vllm    vLLM   qwen3.5-4b  :8000   ZMQ 5559/5560   (deploy/serve-vllm.sh)
#   local-sglang  SGLang qwen3-0.6b  :30000  ZMQ 5557/5558   (deploy/serve-sglang.sh)
#
# Inference engines are managed SEPARATELY — `make inference` starts them,
# `make down` NEVER stops them. Each kvindexer is STATELESS (`-store memory`):
# it loads its one cluster from ONE nested file (deploy/config.local.yaml) into
# memory each boot. The GATEWAY owns the connection registry (SQLite) and sends a
# bearer token to each kvindexer (which requires it).
#
# Quick start (from zero):
#   make inference     # start vLLM + SGLang on the GPU (wait for ready)
#   make build         # compile Go binaries + web prod build
#   make up            # start gateway + both kvindexer backends + frontend
#   make status        # show what's listening + cluster health
#   make smoke         # tokenize+query both clusters end-to-end
#   make down          # stop the control plane (NOT inference)

SHELL := /bin/bash
ROOT  := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))

# --- toolchain ---
GO       := go
# Next 16 needs Node 20 (system node is 18). Keep this assignment comment-free:
NODE_BIN := /home/ubuntu/.local/node20/bin
NPM      := $(NODE_BIN)/npm
export PATH := $(NODE_BIN):$(PATH)

# --- control-plane ports (all bind 127.0.0.1) ---
PORT_VLLM_KVI   := 8090
PORT_SGLANG_KVI := 8091
PORT_GW         := 8095
PORT_WEB        := 3000

# --- cluster ids (must match deploy/config.local.yaml) ---
CLUSTER_VLLM   := local-vllm
CLUSTER_SGLANG := local-sglang

# --- runtime + build artifacts ---
RUN      := $(ROOT)/run
BIN      := $(RUN)/bin
KVINDEXER:= $(BIN)/kvindexer
KVGATEWAY:= $(BIN)/kvgateway

# --- config ---
# ONE nested file describes the topology. Each kvindexer is STATELESS: it loads
# its single cluster from this file into memory (-store memory) every boot. The
# GATEWAY owns the connection registry in SQLite (which kvindexers exist + their
# bearer tokens), seeded once from this file.
CONFIG       := $(ROOT)/deploy/config.local.yaml
GW_SQLITE    := $(RUN)/gateway-connections.db

# Bearer token for the gateway↔kvindexer hop (crosses the network in prod). For
# local dev a fixed dev token is fine; override with `make up AUTH_TOKEN=...`.
# The gateway sends it (-backend-token); each kvindexer requires it (-auth-token).
AUTH_TOKEN   := dev-local-token

# Bind host for the control plane. 127.0.0.1 keeps everything off the public IP.
BIND := 127.0.0.1

# start-svc <name> <port> <logfile> -- <command...>
# Skips if the port is already listening; otherwise launches detached, records a pid.
define start-svc
	@if ss -ltn 2>/dev/null | grep -qP '127\.0\.0\.1:$(2)\b|0\.0\.0\.0:$(2)\b|\*:$(2)\b'; then \
		echo "  [skip] $(1) already listening on :$(2)"; \
	else \
		echo "  [start] $(1) on $(BIND):$(2)  (log: $(3))"; \
		setsid $(4) < /dev/null > $(3) 2>&1 & echo $$! > $(RUN)/$(1).pid; \
		disown || true; \
	fi
endef

# stop-svc <name> <port> -- kill recorded process GROUP, then sweep the port.
define stop-svc
	@stopped=0; \
	pid=$$(cat $(RUN)/$(1).pid 2>/dev/null); \
	if [ -n "$$pid" ] && kill -0 $$pid 2>/dev/null; then \
		kill -- -$$pid 2>/dev/null || kill $$pid 2>/dev/null; \
		echo "  [stop] $(1) (pgid $$pid)"; stopped=1; \
	fi; \
	port_pid=$$(ss -ltnp 2>/dev/null | grep -P ':$(2)\b' | grep -oP 'pid=\K[0-9]+' | head -1); \
	if [ -n "$$port_pid" ]; then kill $$port_pid 2>/dev/null && echo "  [stop] $(1) leftover on :$(2) (pid $$port_pid)"; stopped=1; fi; \
	[ $$stopped -eq 1 ] || echo "  [skip] $(1) not running"; \
	rm -f $(RUN)/$(1).pid
endef

.PHONY: help build build-go build-web test \
        up down restart status logs clean smoke \
        backend-vllm backend-sglang gateway frontend \
        stop-backend-vllm stop-backend-sglang stop-gateway stop-frontend \
        inference stop-inference inference-status

help:
	@echo "ucloud-kv-indexer local stack (2 clusters: $(CLUSTER_VLLM), $(CLUSTER_SGLANG))"
	@echo ""
	@echo "  make inference   start vLLM + SGLang engines on the GPU (separate lifecycle)"
	@echo "  make build       compile Go binaries + web production build"
	@echo "  make up          start gateway + both kvindexer backends + frontend"
	@echo "  make down        stop the control plane (inference untouched)"
	@echo "  make restart     down then up"
	@echo "  make status      show listening ports + per-cluster health"
	@echo "  make smoke       tokenize + query-prefix both clusters end-to-end"
	@echo "  make logs        tail -f all control-plane logs"
	@echo "  make test        go test ./..."
	@echo "  make clean       down + remove run/ (drops the gateway connection DB → re-seed)"

# ---------- build ----------
build: build-go build-web

build-go:
	@mkdir -p $(BIN)
	@echo "==> building Go binaries"
	cd $(ROOT) && $(GO) build -o $(KVINDEXER) ./cmd/kvindexer
	cd $(ROOT) && $(GO) build -o $(KVGATEWAY) ./cmd/kvgateway

build-web:
	@echo "==> web production build (Node 20)"
	@cd $(ROOT)/web && [ -d node_modules ] || PATH="$(NODE_BIN):$$PATH" $(NPM) install
	@cd $(ROOT)/web && grep -q ':$(PORT_GW)' .env.local 2>/dev/null || \
		echo 'NEXT_PUBLIC_API_BASE=http://127.0.0.1:$(PORT_GW)' > .env.local
	cd $(ROOT)/web && PATH="$(NODE_BIN):$$PATH" $(NPM) run build

test:
	cd $(ROOT) && $(GO) test ./...

$(RUN):
	@mkdir -p $(RUN)

# ---------- start individual services ----------
# Each kvindexer is STATELESS: -store memory loads its one cluster from
# config.local.yaml into memory every boot, and requires the bearer token.
backend-vllm: $(KVINDEXER) | $(RUN)
	$(call start-svc,backend-vllm,$(PORT_VLLM_KVI),$(RUN)/backend-vllm.log,$(KVINDEXER) -addr $(BIND):$(PORT_VLLM_KVI) -store memory -bootstrap $(CONFIG) -cluster $(CLUSTER_VLLM) -auth-token $(AUTH_TOKEN))

backend-sglang: $(KVINDEXER) | $(RUN)
	$(call start-svc,backend-sglang,$(PORT_SGLANG_KVI),$(RUN)/backend-sglang.log,$(KVINDEXER) -addr $(BIND):$(PORT_SGLANG_KVI) -store memory -bootstrap $(CONFIG) -cluster $(CLUSTER_SGLANG) -auth-token $(AUTH_TOKEN))

# The gateway OWNS the connection registry (SQLite), seeded once from config with
# the shared bearer token; it attaches that token to every call to a kvindexer.
gateway: $(KVGATEWAY)
	$(call start-svc,gateway,$(PORT_GW),$(RUN)/gateway.log,$(KVGATEWAY) -addr $(BIND):$(PORT_GW) -sqlite-path $(GW_SQLITE) -config $(CONFIG) -backend-token $(AUTH_TOKEN))

frontend:
	@[ -d $(ROOT)/web/.next ] || $(MAKE) build-web
	$(call start-svc,frontend,$(PORT_WEB),$(RUN)/frontend.log,$(NPM) --prefix $(ROOT)/web run start -- -H $(BIND) -p $(PORT_WEB))

$(KVINDEXER) $(KVGATEWAY): build-go

# ---------- up / down ----------
# Order: backends first, then gateway (so its first health probe finds them), then frontend.
up: backend-vllm backend-sglang gateway frontend
	@sleep 1
	@echo "==> stack up.  frontend http://$(BIND):$(PORT_WEB)  gateway $(BIND):$(PORT_GW)"

down: stop-frontend stop-gateway stop-backend-sglang stop-backend-vllm
	@echo "==> control plane stopped (inference left running)"

restart: down up

stop-backend-vllm:
	$(call stop-svc,backend-vllm,$(PORT_VLLM_KVI))
stop-backend-sglang:
	$(call stop-svc,backend-sglang,$(PORT_SGLANG_KVI))
stop-gateway:
	$(call stop-svc,gateway,$(PORT_GW))
stop-frontend:
	$(call stop-svc,frontend,$(PORT_WEB))

# ---------- inference (separate lifecycle; NOT touched by up/down) ----------
# Start vLLM FIRST and wait until it's up, THEN SGLang. The two engines share one
# GPU and each reserves a fraction of *currently free* memory: if SGLang grabs its
# share first, vLLM (the larger model) can be left with too little KV cache and
# fail to initialize. vLLM-first (measured against an empty GPU) avoids that race.
inference:
	@echo "==> starting vLLM (:8000) on the GPU, then SGLang (:30000)"
	$(call start-svc,serve-vllm,8000,$(RUN)/serve-vllm.log,bash $(ROOT)/deploy/serve-vllm.sh)
	@echo "  waiting for vLLM to be ready before starting SGLang (avoids GPU-mem race)..."
	@for i in $$(seq 1 60); do \
		if curl -s --max-time 2 -o /dev/null http://127.0.0.1:8000/health 2>/dev/null; then echo "  vLLM ready"; break; fi; \
		sleep 3; \
	done
	$(call start-svc,serve-sglang,30000,$(RUN)/serve-sglang.log,bash $(ROOT)/deploy/serve-sglang.sh)
	@echo "  (SGLang loads in ~10-20s; check 'make inference-status')"

stop-inference:
	$(call stop-svc,serve-vllm,8000)
	$(call stop-svc,serve-sglang,30000)

inference-status:
	@curl -s --max-time 3 http://127.0.0.1:8000/health  -o /dev/null -w "  vLLM   :8000  HTTP %{http_code}\n" 2>/dev/null || echo "  vLLM   :8000  unreachable"
	@curl -s --max-time 3 http://127.0.0.1:30000/health -o /dev/null -w "  SGLang :30000 HTTP %{http_code}\n" 2>/dev/null || echo "  SGLang :30000 unreachable"

# ---------- observability ----------
status:
	@echo "== control plane =="
	@for pp in "frontend:$(PORT_WEB)" "gateway:$(PORT_GW)" "$(CLUSTER_VLLM):$(PORT_VLLM_KVI)" "$(CLUSTER_SGLANG):$(PORT_SGLANG_KVI)"; do \
		name=$${pp%:*}; port=$${pp##*:}; \
		if ss -ltn 2>/dev/null | grep -qP ":$$port\b"; then echo "  UP    $$name  :$$port"; else echo "  down  $$name  :$$port"; fi; \
	done
	@echo "== inference (independent) =="
	@$(MAKE) -s inference-status
	@echo "== gateway clusters =="
	@curl -s --max-time 3 http://127.0.0.1:$(PORT_GW)/clusters-health 2>/dev/null | python3 -c "import sys,json;[print('  ',c['cluster'],[(b['id'],b['healthy']) for b in c['backends']]) for c in json.load(sys.stdin)]" 2>/dev/null || echo "  (gateway not up)"

# ---------- smoke test (real engines, both clusters) ----------
smoke:
	@bash $(ROOT)/deploy/smoke.sh

logs:
	@echo "tailing $(RUN)/*.log (Ctrl-C to stop)"
	@tail -n 20 -f $(RUN)/backend-vllm.log $(RUN)/backend-sglang.log $(RUN)/gateway.log $(RUN)/frontend.log 2>/dev/null

clean: down
	@rm -rf $(RUN)
	@echo "==> removed $(RUN) (gateway connection DB dropped; kvindexers are stateless)"
