# ucloud-kv-indexer — local dev stack (TWO co-resident clusters on one box)
#
# Topology:  frontend (:3000) ─▶ gateway (:8095) ─▶ kvindexer local-vllm   (:8090)
#                                              └────▶ kvindexer local-sglang (:8091)
#
# Each kvindexer sits next to ONE inference engine and SUBs its ZMQ KV-event
# stream locally; the gateway federates both for the console. Only the frontend
# is exposed by default; gateway, kvindexer, MongoDB, and inference stay loopback.
#
#   local-vllm    vLLM   qwen3.5-4b  :8000   ZMQ 5559/5560   (deploy/serve-vllm.sh)
#   local-sglang  SGLang qwen3-0.6b  :30000  ZMQ 5557/5558   (deploy/serve-sglang.sh)
#
# Inference engines are managed SEPARATELY — `make inference` starts them,
# `make down` NEVER stops them. Each kvindexer has its OWN bootstrap file and
# persists dynamic config/policies plus decoded prefix-cache events in local
# MongoDB. The GATEWAY owns only the connection registry (local SQLite here;
# MySQL in Kubernetes/production) and sends a
# bearer token to each kvindexer (which requires it).
#
# Quick start (from zero):
#   make inference     # start vLLM + SGLang on the GPU (wait for ready)
#   make build         # compile Go binaries + web prod build
#   make up            # start MongoDB + gateway + both kvindexer backends + frontend
#   make status        # show what's listening + cluster health
#   make smoke         # tokenize+query both clusters end-to-end
#   make down          # stop the control plane (NOT inference)

SHELL := /bin/bash
ROOT  := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
RUNTIME_ROOT := $(abspath $(ROOT)/../runtime)

# --- toolchain ---
GO       := go
OAPI_CODEGEN_VERSION := v2.7.1
OAPI_CODEGEN := $(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION)
# Next 16 needs Node 20 (system node is 18). Keep this assignment comment-free:
NODE_BIN := /home/ubuntu/.local/node20/bin
NPM      := $(NODE_BIN)/npm
export PATH := $(NODE_BIN):$(PATH)

# --- control-plane ports ---
PORT_VLLM_KVI   := 8090
PORT_SGLANG_KVI := 8091
PORT_GW         := 8095
PORT_WEB        := 3000
PORT_MOONCAKE_MASTER := 50051
PORT_MOONCAKE_CLIENT := 50052

# --- cluster ids (must match deploy/local-*.yaml) ---
CLUSTER_VLLM   := local-vllm
CLUSTER_SGLANG := local-sglang

# --- runtime + build artifacts ---
RUN      := $(ROOT)/run
BIN      := $(RUN)/bin
KVINDEXER:= $(BIN)/kvindexer
KVGATEWAY:= $(BIN)/kvgateway

# --- OpenAPI artifacts ---
OPENAPI_DIR := $(ROOT)/api
OPENAPI_CHECK_DIR := /tmp/ucloud-kv-indexer-openapi-check
KVINDEXER_OPENAPI := $(OPENAPI_DIR)/kvindexer.openapi.json
GATEWAY_OPENAPI := $(OPENAPI_DIR)/gateway.openapi.json

# --- config ---
# Each kvindexer gets its own cluster-local bootstrap file. The gateway reads
# both files only to seed its federated backend registry.
CONFIG_VLLM        := $(ROOT)/deploy/local-vllm.yaml
CONFIG_SGLANG      := $(ROOT)/deploy/local-sglang.yaml
GATEWAY_CONFIGS    := $(CONFIG_VLLM),$(CONFIG_SGLANG)
GW_SQLITE          := $(RUN)/gateway-connections.db

# --- MongoDB (local Docker) ---
DOCKER             := $(shell if docker ps >/dev/null 2>&1; then echo docker; elif sudo -n docker ps >/dev/null 2>&1; then echo "sudo -n docker"; else echo docker; fi)
MONGO_IMAGE        := mongo:8
MONGO_CONTAINER    := ucloud-kv-indexer-mongo
MONGO_VOLUME       := ucloud-kv-indexer-mongo
MONGO_URI          := mongodb://127.0.0.1:27017
MONGO_DB_VLLM      := kvindexer_local_vllm
MONGO_DB_SGLANG    := kvindexer_local_sglang

# --- container image ---
IMAGE              := uhub.service.ucloud.cn/uminfer-proxy/ucloud-kv-indexer:latest
WEB_IMAGE          := uhub.service.ucloud.cn/uminfer-proxy/ucloud-kv-indexer-web:latest
IMAGE_PLATFORMS    := linux/amd64,linux/arm64

# Bearer token for the gateway↔kvindexer hop (crosses the network in prod). For
# local dev a fixed dev token is fine; override with `make up AUTH_TOKEN=...`.
# The gateway sends it (-backend-token); each kvindexer requires it (-auth-token).
AUTH_TOKEN   := dev-local-token

# Bind hosts. Keep APIs loopback-only; expose the Next.js frontend so remote
# browsers can use the UI while Next proxies /api/kvi to the local gateway.
BIND     := 127.0.0.1
WEB_BIND := 0.0.0.0

# start-svc <name> <port> <logfile> -- <command...>
# Skips if the port is already listening; otherwise launches detached, records a pid.
define start-svc
	@if ss -ltn 2>/dev/null | grep -qP '127\.0\.0\.1:$(2)\b|0\.0\.0\.0:$(2)\b|\*:$(2)\b'; then \
		echo "  [skip] $(1) already listening on :$(2)"; \
	else \
		echo "  [start] $(1) on $(if $(5),$(5),$(BIND)):$(2)  (log: $(3))"; \
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

.PHONY: help build build-go build-web image image-local image-web images test \
        openapi openapi-check \
        up down restart status logs clean clean-mongo smoke \
        backend-vllm backend-sglang gateway frontend \
        mongo stop-mongo mongo-status \
        stop-backend-vllm stop-backend-sglang stop-gateway stop-frontend \
        mooncake-master mooncake-client stop-mooncake-master stop-mooncake-client \
        serve-vllm serve-sglang inference-vllm inference stop-inference inference-status

help:
	@echo "ucloud-kv-indexer local stack (2 clusters: $(CLUSTER_VLLM), $(CLUSTER_SGLANG))"
	@echo ""
	@echo "  make inference   start Mooncake + vLLM + SGLang engines (separate lifecycle)"
	@echo "  make inference-vllm start Mooncake + vLLM only"
	@echo "  make build       compile Go binaries + web production build"
	@echo "  make image       build+push multi-arch Docker image (override IMAGE=repo/name:tag IMAGE_PLATFORMS=linux/amd64,linux/arm64)"
	@echo "  make image-local build local-arch Docker image only"
	@echo "  make image-web   build frontend Docker image (override with WEB_IMAGE=repo/name:tag)"
	@echo "  make up          start MongoDB + gateway + both kvindexer backends + frontend"
	@echo "  make down        stop the control plane (inference untouched)"
	@echo "  make restart     down then up"
	@echo "  make status      show listening ports + per-cluster health"
	@echo "  make smoke       tokenize + query-prefix both clusters end-to-end"
	@echo "  make logs        tail -f all control-plane logs"
	@echo "  make test        go test ./..."
	@echo "  make openapi     regenerate api/*.openapi.json and validate with oapi-codegen"
	@echo "  make openapi-check verify checked-in OpenAPI JSON is current"
	@echo "  make clean       down + remove run/ (keeps MongoDB data)"
	@echo "  make clean-mongo stop MongoDB and delete its Docker volume"

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
	@cd $(ROOT)/web && { \
		grep -vE '^(NEXT_PUBLIC_API_BASE|KVI_API_BASE)=' .env.local 2>/dev/null || true; \
		echo 'NEXT_PUBLIC_API_BASE=/api/kvi'; \
		echo 'KVI_API_BASE=http://127.0.0.1:$(PORT_GW)'; \
	} > .env.local.tmp && mv .env.local.tmp .env.local
	cd $(ROOT)/web && PATH="$(NODE_BIN):$$PATH" $(NPM) run build

image:
	@echo "==> building and pushing Docker image $(IMAGE) for $(IMAGE_PLATFORMS)"
	cd $(ROOT) && $(DOCKER) buildx build --platform $(IMAGE_PLATFORMS) --provenance=false -t $(IMAGE) --push .

image-local:
	@echo "==> building local-arch Docker image $(IMAGE)"
	cd $(ROOT) && $(DOCKER) build -t $(IMAGE) .

image-web:
	@echo "==> building frontend Docker image $(WEB_IMAGE)"
	cd $(ROOT) && $(DOCKER) build -f web/Dockerfile -t $(WEB_IMAGE) web

images: image image-web

test:
	cd $(ROOT) && $(GO) test ./...

$(RUN):
	@mkdir -p $(RUN)

openapi:
	@mkdir -p $(OPENAPI_DIR) $(OPENAPI_CHECK_DIR)
	cd $(ROOT) && $(GO) run ./cmd/openapi -kind kvindexer -out $(KVINDEXER_OPENAPI)
	cd $(ROOT) && $(GO) run ./cmd/openapi -kind gateway -out $(GATEWAY_OPENAPI)
	cd $(ROOT) && $(OAPI_CODEGEN) -generate types,spec -package kvindexeropenapi -o $(OPENAPI_CHECK_DIR)/kvindexer.gen.go $(KVINDEXER_OPENAPI)
	cd $(ROOT) && $(OAPI_CODEGEN) -generate types,spec -package gatewayopenapi -o $(OPENAPI_CHECK_DIR)/gateway.gen.go $(GATEWAY_OPENAPI)

openapi-check: | $(RUN)
	@mkdir -p $(OPENAPI_CHECK_DIR)
	cd $(ROOT) && $(GO) run ./cmd/openapi -kind kvindexer -out $(OPENAPI_CHECK_DIR)/kvindexer.openapi.json
	cd $(ROOT) && $(GO) run ./cmd/openapi -kind gateway -out $(OPENAPI_CHECK_DIR)/gateway.openapi.json
	diff -u $(KVINDEXER_OPENAPI) $(OPENAPI_CHECK_DIR)/kvindexer.openapi.json
	diff -u $(GATEWAY_OPENAPI) $(OPENAPI_CHECK_DIR)/gateway.openapi.json
	cd $(ROOT) && $(OAPI_CODEGEN) -generate types,spec -package kvindexeropenapi -o $(OPENAPI_CHECK_DIR)/kvindexer.gen.go $(OPENAPI_CHECK_DIR)/kvindexer.openapi.json
	cd $(ROOT) && $(OAPI_CODEGEN) -generate types,spec -package gatewayopenapi -o $(OPENAPI_CHECK_DIR)/gateway.gen.go $(OPENAPI_CHECK_DIR)/gateway.openapi.json

# ---------- MongoDB ----------
mongo:
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "  docker CLI not found; install/start Docker before running MongoDB"; \
		exit 127; \
	fi
	@if $(DOCKER) ps --format '{{.Names}}' | grep -qx '$(MONGO_CONTAINER)'; then \
		echo "  [skip] MongoDB already running ($(MONGO_CONTAINER))"; \
	elif $(DOCKER) ps -a --format '{{.Names}}' | grep -qx '$(MONGO_CONTAINER)'; then \
		echo "  [start] MongoDB container $(MONGO_CONTAINER)"; \
		$(DOCKER) start $(MONGO_CONTAINER) >/dev/null; \
	else \
		echo "  [start] MongoDB $(MONGO_IMAGE) on 127.0.0.1:27017"; \
		$(DOCKER) run -d --name $(MONGO_CONTAINER) \
			-p 127.0.0.1:27017:27017 \
			-v $(MONGO_VOLUME):/data/db \
			$(MONGO_IMAGE) >/dev/null; \
	fi
	@for i in $$(seq 1 30); do \
		if $(DOCKER) exec $(MONGO_CONTAINER) mongosh --quiet --eval 'db.adminCommand({ping:1}).ok' 2>/dev/null | grep -qx '1'; then \
			echo "  MongoDB ready"; exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "  MongoDB did not become ready"; exit 1

stop-mongo:
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "  [skip] MongoDB not available (docker CLI not found)"; \
		exit 0; \
	fi
	@if $(DOCKER) ps --format '{{.Names}}' | grep -qx '$(MONGO_CONTAINER)'; then \
		echo "  [stop] MongoDB $(MONGO_CONTAINER)"; \
		$(DOCKER) stop $(MONGO_CONTAINER) >/dev/null; \
	else \
		echo "  [skip] MongoDB not running"; \
	fi

mongo-status:
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "  MongoDB unavailable (docker CLI not found)"; \
		exit 0; \
	fi
	@if $(DOCKER) ps --format '{{.Names}}' | grep -qx '$(MONGO_CONTAINER)'; then \
		echo "  MongoDB $(MONGO_CONTAINER) running on 127.0.0.1:27017"; \
	else \
		echo "  MongoDB $(MONGO_CONTAINER) down"; \
	fi

# ---------- start individual services ----------
# Each kvindexer loads its own cluster bootstrap once, then MongoDB is
# authoritative for frontend policy edits and persisted prefix-cache events.
backend-vllm: $(KVINDEXER) | $(RUN)
	$(call start-svc,backend-vllm,$(PORT_VLLM_KVI),$(RUN)/backend-vllm.log,$(KVINDEXER) -addr $(BIND):$(PORT_VLLM_KVI) -store mongo -mongo-uri $(MONGO_URI) -mongo-db $(MONGO_DB_VLLM) -bootstrap $(CONFIG_VLLM) -cluster $(CLUSTER_VLLM) -auth-token $(AUTH_TOKEN))

backend-sglang: $(KVINDEXER) | $(RUN)
	$(call start-svc,backend-sglang,$(PORT_SGLANG_KVI),$(RUN)/backend-sglang.log,$(KVINDEXER) -addr $(BIND):$(PORT_SGLANG_KVI) -store mongo -mongo-uri $(MONGO_URI) -mongo-db $(MONGO_DB_SGLANG) -bootstrap $(CONFIG_SGLANG) -cluster $(CLUSTER_SGLANG) -auth-token $(AUTH_TOKEN))

# The gateway OWNS the connection registry (SQLite), seeded once from config with
# the shared bearer token; it attaches that token to every call to a kvindexer.
gateway: $(KVGATEWAY)
	$(call start-svc,gateway,$(PORT_GW),$(RUN)/gateway.log,$(KVGATEWAY) -addr $(BIND):$(PORT_GW) -sqlite-path $(GW_SQLITE) -configs $(GATEWAY_CONFIGS) -backend-token $(AUTH_TOKEN))

frontend:
	@[ -d $(ROOT)/web/.next ] || $(MAKE) build-web
	$(call start-svc,frontend,$(PORT_WEB),$(RUN)/frontend.log,$(NPM) --prefix $(ROOT)/web run start -- -H $(WEB_BIND) -p $(PORT_WEB),$(WEB_BIND))

$(KVINDEXER) $(KVGATEWAY): build-go

# ---------- up / down ----------
# Order: Mongo first, then backends, then gateway (so its first health probe finds them), then frontend.
up: mongo backend-vllm backend-sglang gateway frontend
	@sleep 1
	@echo "==> stack up.  frontend http://$(WEB_BIND):$(PORT_WEB)  gateway $(BIND):$(PORT_GW)"

down: stop-frontend stop-gateway stop-backend-sglang stop-backend-vllm stop-mongo
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

mooncake-master: | $(RUN)
	$(call start-svc,mooncake-master,$(PORT_MOONCAKE_MASTER),$(RUN)/mooncake-master.log,bash $(RUNTIME_ROOT)/start-mooncake-master-qwen.sh)
	@for i in $$(seq 1 20); do \
		if ss -ltn 2>/dev/null | grep -qP ':$(PORT_MOONCAKE_MASTER)\b'; then echo "  Mooncake master ready"; exit 0; fi; \
		sleep 1; \
	done; \
	echo "  Mooncake master did not become ready"; exit 1

mooncake-client: mooncake-master | $(RUN)
	$(call start-svc,mooncake-client,$(PORT_MOONCAKE_CLIENT),$(RUN)/mooncake-client.log,bash $(RUNTIME_ROOT)/start-mooncake-client-qwen.sh)
	@for i in $$(seq 1 20); do \
		if ss -ltn 2>/dev/null | grep -qP ':$(PORT_MOONCAKE_CLIENT)\b'; then echo "  Mooncake client ready"; exit 0; fi; \
		sleep 1; \
	done; \
	echo "  Mooncake client did not become ready"; exit 1

serve-vllm: mooncake-client | $(RUN)
	$(call start-svc,serve-vllm,8000,$(RUN)/serve-vllm.log,bash $(ROOT)/deploy/serve-vllm.sh)

serve-sglang: | $(RUN)
	$(call start-svc,serve-sglang,30000,$(RUN)/serve-sglang.log,bash $(ROOT)/deploy/serve-sglang.sh)

inference-vllm: serve-vllm
	@echo "  waiting for vLLM to be ready..."
	@for i in $$(seq 1 60); do \
		if curl -s --max-time 2 -o /dev/null http://127.0.0.1:8000/health 2>/dev/null; then echo "  vLLM ready"; exit 0; fi; \
		sleep 3; \
	done; \
	echo "  vLLM did not become ready"; exit 1

inference:
	@echo "==> starting Mooncake + vLLM (:8000), then SGLang (:30000)"
	@$(MAKE) -s inference-vllm
	@$(MAKE) -s serve-sglang
	@echo "  (SGLang loads in ~10-20s; check 'make inference-status')"

stop-inference:
	$(call stop-svc,serve-vllm,8000)
	$(call stop-svc,serve-sglang,30000)
	$(call stop-svc,mooncake-client,$(PORT_MOONCAKE_CLIENT))
	$(call stop-svc,mooncake-master,$(PORT_MOONCAKE_MASTER))

stop-mooncake-client:
	$(call stop-svc,mooncake-client,$(PORT_MOONCAKE_CLIENT))
stop-mooncake-master:
	$(call stop-svc,mooncake-master,$(PORT_MOONCAKE_MASTER))

inference-status:
	@if ss -ltn 2>/dev/null | grep -qP ':$(PORT_MOONCAKE_MASTER)\b'; then echo "  Mooncake master :$(PORT_MOONCAKE_MASTER) listening"; else echo "  Mooncake master :$(PORT_MOONCAKE_MASTER) down"; fi
	@if ss -ltn 2>/dev/null | grep -qP ':$(PORT_MOONCAKE_CLIENT)\b'; then echo "  Mooncake client :$(PORT_MOONCAKE_CLIENT) listening"; else echo "  Mooncake client :$(PORT_MOONCAKE_CLIENT) down"; fi
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
	@echo "== mongodb =="
	@$(MAKE) -s mongo-status
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
	@echo "==> removed $(RUN) (gateway connection DB dropped; MongoDB data kept)"

clean-mongo: stop-mongo
	@$(DOCKER) rm $(MONGO_CONTAINER) >/dev/null 2>&1 || true
	@$(DOCKER) volume rm $(MONGO_VOLUME) >/dev/null 2>&1 || true
	@echo "==> removed MongoDB container and volume"
