"use client";

import * as React from "react";

export type Locale = "en" | "zh";

type Dict = Record<string, string>;

const en: Dict = {
  // brand / chrome
  "app.brand": "kv-indexer",
  "app.brand.subtitle": "admission console",
  "app.section.platform": "Platform",
  "app.section.tools": "Tools",
  "app.section.observability": "Observability",
  "app.locale.toggle": "Language",
  "app.theme.toggle": "Toggle theme",
  "app.theme.light": "Light",
  "app.theme.dark": "Dark",
  "app.theme.system": "System",
  "app.user.account": "Account",
  "app.user.notifications": "Notifications",
  "app.user.logout": "Log out",
  "common.cancel": "Cancel",
  "common.save": "Save",
  "common.edit": "Edit",
  "common.delete": "Delete",
  "common.enabled": "enabled",
  "common.disabled": "disabled",
  "common.on": "on",
  "common.off": "off",
  "common.yes": "yes",
  "common.no": "no",
  "common.up": "up",
  "common.down": "down",
  "common.fresh": "fresh",
  "common.stale": "stale",
  "common.default": "default",
  "common.any": "any",
  "common.global": "global",
  "common.none": "—",
  "common.loading": "Loading…",
  "common.error": "Failed to load",
  "common.retry": "Retry",
  "common.refresh": "Refresh",
  "common.prev": "Prev",
  "common.next": "Next",
  "common.details": "Details",
  "common.never": "never",
  "common.ago": "{n} ago",
  "common.justnow": "just now",
  "common.copied": "Copied",

  // stream status (derived admission health of a KV-event listener)
  "stream.status.healthy": "healthy",
  "stream.status.stale": "stale",
  "stream.status.idle": "idle",
  "stream.status.down": "down",
  "stream.status.degraded": "degraded",
  "stream.status.tip.healthy": "Connected and receiving events.",
  "stream.status.tip.stale": "Connected but no events recently — admission may treat residency as stale.",
  "stream.status.tip.idle": "Connected, no events yet (cold listener).",
  "stream.status.tip.down": "Listener is not connected to the engine.",
  "stream.status.tip.degraded": "Receiving events but gaps or decode errors detected.",
  "streams.col.status": "Status",
  "streams.col.skipped": "Skipped",
  "streams.col.last_event": "Last event",
  "overview.stat.stale_streams": "Stale / down streams",

  // cluster / federation
  "cluster.label": "Cluster",
  "cluster.all": "All clusters",
  "cluster.col": "Cluster",
  "cluster.backend": "Backend",
  "overview.stat.clusters_count": "Clusters",

  // nav
  "nav.overview": "Overview",
  "nav.engines": "Engines",
  "nav.profiles": "Model Profiles",
  "nav.policies": "Admission Policies",
  "nav.streams": "KV Event Streams",
  "nav.simulator": "Prefix Simulator",
  "nav.decisions": "Live Decisions",
  "nav.audit": "Config Audit",
  "nav.api_docs": "API Docs",

  // API docs
  "docs.title": "API Docs",
  "docs.subtitle":
    "OpenAPI view of the kv-indexer and gateway HTTP surface.",
  "docs.raw": "Open JSON",
  "docs.count": "{n} operations",
  "docs.col.method": "Method",
  "docs.col.path": "Path",
  "docs.col.summary": "Summary",
  "docs.col.params": "Parameters",
  "docs.col.body": "Body",
  "docs.empty": "No OpenAPI operations found.",

  // overview page
  "overview.title": "Cluster Overview",
  "overview.subtitle":
    "Length + cache-hit-rate admission judgment across inference clusters",
  "overview.stat.clusters": "Clusters",
  "overview.stat.engines": "Engines",
  "overview.stat.profiles": "Model profiles",
  "overview.stat.healthy_streams": "Healthy streams",
  "overview.stat.indexed_blocks": "Indexed prefix blocks",
  "overview.stat.reject_rate": "429 reject rate",
  "overview.stat.fallback_rate": "Fallback rate",
  "overview.stat.decisions": "Decisions logged",
  "overview.recent.title": "Recent hit ratio",
  "overview.recent.desc":
    "Last {n} admission decisions, prefix-cache hit ratio in percent.",
  "overview.recent.empty":
    "No decisions yet — try the Prefix Simulator or send a request.",
  "overview.clusters.title": "Clusters",
  "overview.clusters.desc": "Region · environment · state",
  "overview.clusters.empty": "No clusters.",
  "overview.cluster.maintenance": "maintenance",
  "overview.latest.title": "Latest decisions",
  "overview.latest.desc":
    "Most recent admission verdicts across all protocols.",
  "overview.latest.empty": "No decisions logged yet.",
  "overview.col.time": "Time",
  "overview.col.protocol": "Protocol",
  "overview.col.model": "Model",
  "overview.col.decision": "Decision",
  "overview.col.reason": "Reason",
  "overview.col.tokens": "Tokens",
  "overview.col.hit": "Hit",

  // engines
  "engines.title": "Engine Registry",
  "engines.subtitle":
    "vLLM/SGLang workers. Hot-toggle enabled / draining / health without losing the index.",
  "engines.btn.register": "Register engine",
  "engines.sheet.title": "Register engine",
  "engines.sheet.desc":
    "Connect a vLLM or SGLang worker. The KV event endpoint is required for prefix-cache awareness.",
  "engines.col.engine": "Engine",
  "engines.col.cluster": "Cluster",
  "engines.col.framework": "Framework",
  "engines.col.models": "Models",
  "engines.col.endpoint": "Endpoint",
  "engines.col.kv_stream": "KV stream",
  "engines.col.indexer": "Indexer",
  "engines.col.state": "State",
  "engines.col.actions": "Actions",
  "engines.empty": "No engines registered.",
  "engines.status.draining": "draining",
  "engines.status.unhealthy": "unhealthy",
  "engines.action.disable": "Disable",
  "engines.action.enable": "Enable",
  "engines.action.drain": "Drain",
  "engines.action.undrain": "Undrain",
  "engines.field.engine_id": "Engine ID",
  "engines.field.cluster": "Cluster",
  "engines.field.framework": "Framework",
  "engines.field.served": "Served models (comma-separated)",
  "engines.field.api": "API endpoint",
  "engines.field.tokenizer": "Tokenizer endpoint",
  "engines.field.kv": "KV event endpoint (ZMQ)",
  "engines.field.replay": "Replay endpoint",
  "engines.field.target_backend": "Target indexer",
  "engines.no_indexers": "Add and enable an indexer connection before registering an engine.",
  "engines.error.no_indexer": "Select an indexer first.",
  "engines.error.no_cluster": "Select or enter the engine cluster.",
  "engines.toast.update_failed": "Update failed",

  // indexers
  "indexers.title": "Indexer Connections",
  "indexers.desc":
    "Gateway-owned registry stored in MongoDB. Each row is one kvindexer backend the gateway can federate.",
  "indexers.btn.add": "Add indexer",
  "indexers.btn.check": "Check",
  "indexers.sheet.new": "Add indexer",
  "indexers.sheet.edit": "Edit indexer: {id}",
  "indexers.sheet.desc":
    "Register the kvindexer HTTP endpoint for one region or cluster. Inference engines are added through the selected indexer.",
  "indexers.field.id": "Indexer ID",
  "indexers.field.kind": "Type",
  "indexers.field.cluster": "Cluster",
  "indexers.field.display_name": "Display name",
  "indexers.field.display_name_ph": "Local tokenizer",
  "indexers.field.url": "Indexer URL",
  "indexers.field.token": "Bearer token",
  "indexers.field.token_keep": "Leave empty to keep existing token",
  "indexers.field.token_optional": "Optional",
  "indexers.field.enabled": "Enabled",
  "indexers.field.enabled_hint":
    "Only enabled indexers receive fan-out reads and targeted writes.",
  "indexers.kind.backend": "Real indexer",
  "indexers.kind.virtual": "Virtual",
  "indexers.col.id": "Indexer",
  "indexers.col.cluster": "Cluster",
  "indexers.col.url": "URL",
  "indexers.col.token": "Token",
  "indexers.col.state": "State",
  "indexers.col.health": "Health",
  "indexers.col.actions": "Actions",
  "indexers.empty": "No indexer connections yet.",
  "indexers.token.set": "set",
  "indexers.token.none": "none",
  "indexers.health.unknown": "not checked",
  "indexers.health.ok": "healthy",
  "indexers.health.failed": "failed",
  "indexers.health.missing": "Indexer is not present in /clusters-health.",
  "indexers.health.unhealthy": "Gateway reported this indexer as unhealthy.",
  "indexers.health.ok_detail": "Reachable at {url}",
  "indexers.health.virtual_detail": "Gateway-local virtual indexer",
  "indexers.health.ok_toast": "Indexer {id} is healthy",
  "indexers.health.fail_toast": "Indexer {id} health check failed",
  "indexers.confirm.delete": "Delete indexer connection {id}?",
  "indexers.toast.saved": "Indexer saved",
  "indexers.toast.deleted": "Indexer deleted",
  "indexers.toast.update_failed": "Indexer update failed",
  "indexers.toast.delete_failed": "Indexer delete failed",

  // profiles
  "profiles.title": "Model Profiles",
  "profiles.subtitle":
    "Tokenization + hash semantics. Changing block size / hash profile / tokenizer bumps the version and isolates the request-key namespace.",
  "profiles.btn.new": "New profile",
  "profiles.col.model": "Model",
  "profiles.col.framework": "Framework",
  "profiles.col.tokenizer_source": "Tokenizer",
  "profiles.col.version": "Version",
  "profiles.col.hash": "Hash profile",
  "profiles.col.block": "Block size",
  "profiles.col.namespace": "Namespace",
  "profiles.col.features": "Features",
  "profiles.empty": "No profiles.",
  "profiles.text_only": "text-only",
  "profiles.sheet.new": "New profile",
  "profiles.sheet.edit": "Edit {id}",
  "profiles.sheet.desc":
    "Tokenizer, hash, and block-size choices namespace the request-key index. Mutating any of them bumps the profile version.",
  "profiles.field.model": "Model ID",
  "profiles.field.framework": "Framework",
  "profiles.field.hash": "Hash profile",
  "profiles.field.block": "Block size",
  "profiles.field.block_hint": "qwen3.5-4b full_attention group = 528",
  "profiles.field.tokenizer": "Tokenizer endpoint",
  "profiles.field.tokenizer_ph": "inherit from engine",
  "profiles.field.target_backend": "Target indexer / cluster",
  "profiles.field.tokenizer_mode": "Tokenizer source",
  "profiles.tokenizer_mode.remote": "Remote engine",
  "profiles.tokenizer_mode.local": "Local sidecar",
  "profiles.field.tokenizer_zip": "Tokenizer zip",
  "profiles.field.template_file": "Chat template file",
  "profiles.field.template": "Chat template",
  "profiles.error.empty_template_file": "Chat template file is empty.",
  "profiles.error.empty_tokenizer_zip": "Tokenizer zip is empty.",
  "profiles.field.seed": "Hash seed (namespace)",
  "profiles.feature.lora": "LoRA",
  "profiles.feature.mm": "Multimodal",
  "profiles.feature.salt": "Cache salt",
  "profiles.bump.title": "Saving will create v{n}",
  "profiles.bump.desc":
    "This change affects tokenization or hashing. A fresh request-key namespace will be allocated; old residency will TTL out, not corrupt new queries.",
  "profiles.btn.save_new": "Save as new version",
  "profiles.btn.move": "Save and move",

  // policies
  "policies.title": "Admission Policies",
  "policies.subtitle":
    "Priority rules for cache-aware admission. Conditions inside one rule are AND; rules are matched by priority.",
  "policies.btn.new": "New rule",
  "policies.btn.test": "Test rule",
  "policies.sheet.title": "New rule",
  "policies.sheet.edit": "Edit rule: {id}",
  "policies.confirm.delete": "Delete rule {id}?",
  "policies.sheet.desc":
    "Build one admission rule from AND conditions, then choose the action to run when it matches.",
  "policies.test.title": "Test rule matching",
  "policies.test.desc":
    "Simulate a request shape, match rules by priority, and inspect the first rule that wins.",
  "policies.list.title": "Policy rules",
  "policies.list.desc": "Rules are evaluated by priority. The first enabled rule whose conditions all match wins.",
  "policies.list.empty": "No policy rules. Requests are accepted when no rule matches.",
  "policies.col.policy": "Rule ID",
  "policies.col.scope": "Applies to",
  "policies.col.long": "Check after",
  "policies.col.hard": "Reject after",
  "policies.col.minhit": "Required hit",
  "policies.col.ttl": "Event age",
  "policies.col.enabled": "Status",
  "policies.col.priority": "Priority",
  "policies.col.name": "Name",
  "policies.col.scope_cluster": "Applies to cluster",
  "policies.col.conditions": "Conditions",
  "policies.col.action": "Action",
  "policies.col.uncertain": "Uncertain",
  "policies.preview.title": "Rule preview",
  "policies.preview.desc":
    "Test which rule would match a request shape before sending traffic.",
  "policies.preview.btn": "Preview",
  "policies.preview.merge": "applied rules",
  "policies.preview.long": "Check-after threshold",
  "policies.preview.hard": "Reject-after threshold",
  "policies.preview.minhit": "Required hit rate",
  "policies.preview.ttl": "KV event max age",
  "policies.preview.stale": "Stale-stream behavior",
  "policies.preview.weights": "GPU / CPU / disk weights",
  "policies.preview.enabled": "Final status",
  "policies.preview.matched": "Matched rule",
  "policies.preview.no_match": "No matching rule",
  "policies.preview.evaluated": "Evaluated",
  "policies.field.id": "Rule ID",
  "policies.field.id_ph": "tenant-a-qwen",
  "policies.field.name": "Display name",
  "policies.field.name_ph": "256+ tokens require KV hit",
  "policies.field.priority": "Priority",
  "policies.field.scope_model": "Model match",
  "policies.field.scope_tenant": "Tenant match",
  "policies.field.scope_cluster": "Cluster match",
  "policies.field.model": "Model",
  "policies.field.tenant": "Tenant",
  "policies.field.cluster": "Cluster",
  "policies.field.ph_any": "All",
  "policies.field.long": "Start checking after tokens",
  "policies.field.hard": "Reject immediately above tokens",
  "policies.field.input_tokens": "Input tokens",
  "policies.field.preview_hit_ratio": "Assumed KV hit rate",
  "policies.field.action": "Matched action",
  "policies.field.low_hit": "Low-hit outcome",
  "policies.field.uncertain": "Uncertain-signal outcome",
  "policies.field.reject_status": "Reject HTTP status",
  "policies.field.minhit": "Required KV hit rate",
  "policies.field.ttl": "KV event max age (ms)",
  "policies.btn.save": "Save rule",
  "policies.btn.add_condition": "Add condition",
  "policies.storage_cluster": "stored on {cluster}",
  "policies.scope_cluster.desc":
    "Choosing a cluster writes a cluster_id = value condition. Any means this rule has no cluster condition.",
  "policies.error.target_cluster_required":
    "Choose an applicable cluster, or switch the top-right cluster selector to one concrete cluster before saving.",
  "policies.help.rule_id":
    "A stable id for this rule. It is used in audit records, API responses, and update/delete paths.",
  "policies.help.name":
    "A readable label for operators. If empty, the rule id is shown.",
  "policies.help.priority":
    "Higher priority rules run first. Once a rule matches, lower-priority rules are not evaluated.",
  "policies.help.cluster":
    "The request cluster_id this rule matches. Choosing local-vllm means only local-vllm admission requests can match this rule; Any leaves the rule unscoped by cluster.",
  "policies.help.scope":
    "Which requests this rule matches. Empty model, tenant, and cluster means the global default rule.",
  "policies.help.model":
    "The request model name, usually the same value clients send in the OpenAI or Anthropic model field.",
  "policies.help.tenant":
    "Your application's tenant, customer, or workspace ID. kv-indexer only uses it when callers pass tenant_id or equivalent metadata; otherwise leave it empty.",
  "policies.help.check_after":
    "Below this input-token count, the request is accepted without enforcing cache-hit rate. At or above it, KV hit rate is checked.",
  "policies.help.reject_after":
    "At or above this input-token count, the request can be rejected immediately if the policy gate fails. Keep it below the model context limit.",
  "policies.help.required_hit":
    "Minimum fraction of prompt tokens that must already be resident in KV cache for long prompts. 0.5 means 50%.",
  "policies.help.event_age":
    "Maximum age of KV-cache events before the listener is treated as stale. Stale streams avoid strict rejection because residency data may be incomplete.",
  "policies.help.status":
    "Disabled rules are ignored. If no enabled rule matches, the request is accepted.",
  "policies.help.enabled":
    "Turn this rule on or off without deleting it.",
  "policies.help.stale_behavior":
    "How admission behaves when KV events are too old or the listener is stale. The current backend value is shown here.",
  "policies.help.weights":
    "Relative credit assigned to KV hits found on GPU, CPU, and disk tiers. Higher weight means that tier contributes more to the hit score.",
  "policies.help.applied_rules":
    "Rules that were merged to produce the final policy, ordered from broader defaults to more specific matches.",
  "policies.help.conditions":
    "Every condition in one rule must be true. Leave the list empty to match every request.",
  "policies.help.action":
    "The operation to run after a rule matches: accept, reject, or require a minimum KV hit rate.",
  "policies.help.input_tokens":
    "Request prompt length after tokenization. This is the common field for starting cache-hit checks at a token threshold.",
  "policies.help.preview_hit_ratio":
    "Synthetic hit ratio used only by this preview. Real admission still queries the live prefix index.",
  "policies.help.low_hit":
    "What to do when the KV hit ratio is below the required threshold.",
  "policies.help.uncertain":
    "What to do when the cache signal cannot be trusted, for example tokenizer failure, unsupported hash features, no serving engine, or an unhealthy KV event stream.",
  "policies.help.reject_status":
    "HTTP status returned when the rule rejects. 429 is recommended for admission backpressure.",
  "policies.help.matched_rule":
    "The first enabled rule whose AND conditions all matched this preview request.",
  "policies.help.result_reason":
    "Backend reason code explaining why the preview accepted, rejected, or fell back.",
  "policies.help.evaluated_rules":
    "Rules considered in priority order until the first match.",
  "policies.form.conditions": "Match conditions",
  "policies.form.conditions_desc":
    "These conditions are ANDed with the cluster scope above. Separate rules are OR by priority.",
  "policies.form.action": "Action",
  "policies.conditions.all": "all requests",
  "policies.conditions.all_desc":
    "No conditions means this rule matches every request.",
  "policies.condition.field": "Field",
  "policies.condition.op": "Operator",
  "policies.condition.value": "Value",
  "policies.placeholder.list": "value1, value2",
  "policies.condition.field.cluster_id": "cluster",
  "policies.condition.field.model_id": "model",
  "policies.condition.field.tenant_id": "tenant",
  "policies.condition.field.input_tokens": "input tokens",
  "policies.condition.field.hit_ratio": "hit ratio",
  "policies.condition.field.best_hit_tokens": "best hit tokens",
  "policies.condition.field.effective_cached_tokens": "effective cached tokens",
  "policies.condition.field.kv_event_state": "KV event state",
  "policies.condition.field.tokenized": "tokenized",
  "policies.condition.field.hash_supported": "hash supported",
  "policies.condition.field.has_candidates": "has candidates",
  "policies.condition.op.eq": "=",
  "policies.condition.op.neq": "!=",
  "policies.condition.op.in": "in",
  "policies.condition.op.not_in": "not in",
  "policies.condition.op.gt": ">",
  "policies.condition.op.gte": ">=",
  "policies.condition.op.lt": "<",
  "policies.condition.op.lte": "<=",
  "policies.condition.op.contains": "contains",
  "policies.action.accept": "Accept",
  "policies.action.reject": "Reject",
  "policies.action.require_cache_hit": "Require KV hit",
  "policies.action.require_hit": "Require KV hit",
  "policies.outcome.accept": "Accept",
  "policies.outcome.reject": "Reject",
  "policies.outcome.fallback_accept": "Fallback accept",

  // streams
  "streams.title": "KV Event Streams",
  "streams.subtitle":
    "Per-engine ZMQ listener health: connection, sequence, gaps, decode errors. Freshness for admission is derived from this.",
  "streams.listeners.title": "Listeners",
  "streams.listeners.desc":
    "Connection state and event throughput per engine.",
  "streams.col.engine": "Engine",
  "streams.col.endpoint": "Endpoint",
  "streams.col.topic": "Topic",
  "streams.col.connected": "Connected",
  "streams.col.last_seq": "Last seq",
  "streams.col.events": "Events",
  "streams.col.gaps": "Gaps",
  "streams.col.decode": "Decode errs",
  "streams.col.queue": "Queue",
  "streams.col.last_err": "Last error",
  "streams.empty.listeners": "No listeners.",
  "streams.events.title": "Live KV events",
  "streams.events.desc":
    "Decoded ZMQ events as they arrive. Recent events are loaded first; live streaming follows the selected cluster.",
  "streams.events.live": "live",
  "streams.events.connecting": "connecting",
  "streams.events.select_cluster": "select cluster",
  "streams.events.query": "Query KV events",
  "streams.events.detail": "KV event details",
  "streams.events.empty": "No KV events observed yet.",
  "streams.events.empty_filtered": "No KV events match the current filter.",
  "streams.events.filter.indexed": "Indexed",
  "streams.events.filter.all": "All",
  "streams.events.page_info": "Page {page}/{pages} · {total} events · 10 per page",
  "streams.events.col.time": "Observed",
  "streams.events.col.kind": "Kind",
  "streams.events.col.model": "Model",
  "streams.events.col.tier": "Tier",
  "streams.events.col.indexed": "Indexed",
  "streams.events.col.tokens": "Tokens",
  "streams.events.col.keys": "Req keys",
  "streams.events.col.skip": "Skip reason",
  "streams.events.col.detail": "Detail",
  "streams.events.raw_json": "Raw JSON",
  "streams.index.title": "Residency index",
  "streams.index.desc": "Prefix-block counts per profile namespace.",
  "streams.col.namespace": "Namespace",
  "streams.col.req_keys": "Prefix blocks",
  "streams.col.bridges": "Engine bridges",
  "streams.col.engines": "Engines",
  "streams.empty.index": "Index empty — no events ingested yet.",

  // decisions
  "decisions.title": "Live Decisions",
  "decisions.subtitle":
    "Recent admission verdicts with reason, hit ratio, target, and config version.",
  "decisions.col.time": "Time",
  "decisions.col.protocol": "Protocol",
  "decisions.col.model": "Model",
  "decisions.col.tenant": "Tenant",
  "decisions.col.decision": "Decision",
  "decisions.col.reason": "Reason",
  "decisions.col.input": "Input",
  "decisions.col.hit": "Hit ratio",
  "decisions.col.target": "Target",
  "decisions.col.cfg": "Cfg",
  "decisions.empty":
    "No decisions yet. Use the Prefix Simulator or POST to /v1/chat/completions, /v1/responses, /v1/messages.",
  "decisions.filter.all": "All decisions",
  "decisions.filter.accept": "Accepted",
  "decisions.filter.reject": "Rejected (429)",
  "decisions.filter.fallback": "Fallback",
  "decisions.filter.none": "No decisions match this filter.",
  "decisions.count": "{shown} of {total}",

  // audit
  "audit.title": "Config Audit",
  "audit.subtitle":
    "Every configuration mutation bumps the global version. Profile version bumps (which isolate the request-key namespace) are flagged.",
  "audit.col.version": "Version",
  "audit.col.time": "Time",
  "audit.col.action": "Action",
  "audit.col.entity": "Entity",
  "audit.col.id": "ID",
  "audit.col.detail": "Detail",
  "audit.col.flag": "Flag",
  "audit.bump_badge": "profile version bump",
  "audit.empty": "No config changes recorded.",

  // simulator
  "sim.title": "Prefix Query Simulator",
  "sim.subtitle":
    "Tokenize via the engine, compute request-keys, query residency, and run the admission judgment — across all three protocols.",
  "sim.req.title": "Request",
  "sim.req.desc": "Construct a prompt and pick a protocol to simulate.",
  "sim.field.model": "Model",
  "sim.field.protocol": "Protocol",
  "sim.field.text": "Prompt / message text",
  "sim.btn.run": "Run full pipeline",
  "sim.btn.tokenize": "Tokenize only",
  "sim.btn.running": "Running…",
  "sim.raw.title": "Request body",
  "sim.raw.desc": "Exact JSON sent to {path}.",
  "sim.needs_cluster":
    "Pick a specific cluster in the top-right switcher — the simulator runs against one backend.",
  "sim.tok.title": "Tokenization",
  "sim.tok.tokens": "{n} tokens",
  "sim.tok.blocks": "{n} prefix blocks",
  "sim.tok.block_size": "block_size {n}",
  "sim.tok.namespace": "namespace",
  "sim.tok.req_keys": "request keys",
  "sim.hits.title": "Per-instance prefix hits",
  "sim.hits.empty": "No residency match (cold prefix).",
  "sim.hits.matched": "matched {n}",
  "sim.dec.reject": "429 Reject",
  "sim.dec.accept": "Accept",
  "sim.dec.input": "input",
  "sim.dec.tok": "tok",
  "sim.dec.best": "best hit",
  "sim.dec.ratio": "ratio",
  "sim.dec.fallback": "fallback",
  "sim.dec.target": "suggested target:",
  "sim.dec.min": "min required ratio: {min} · got {got}%",
  "sim.dec.profile": "profile v{p} · config #{c} · rule: {ids}",

  // protocols
  "protocol.openai.chat": "OpenAI Chat",
  "protocol.openai.responses": "OpenAI Responses",
  "protocol.anthropic.messages": "Anthropic Messages",
};

const zh: Dict = {
  // brand / chrome
  "app.brand": "kv-indexer",
  "app.section.platform": "平台",
  "app.section.tools": "工具",
  "app.section.observability": "观测",
  "app.locale.toggle": "语言",
  "app.theme.toggle": "切换主题",
  "app.theme.light": "浅色",
  "app.theme.dark": "深色",
  "app.theme.system": "跟随系统",
  "app.user.account": "账户",
  "app.user.notifications": "通知",
  "app.user.logout": "退出登录",
  "common.cancel": "取消",
  "common.save": "保存",
  "common.edit": "编辑",
  "common.delete": "删除",
  "common.enabled": "已启用",
  "common.disabled": "已停用",
  "common.on": "开启",
  "common.off": "关闭",
  "common.yes": "是",
  "common.no": "否",
  "common.up": "在线",
  "common.down": "离线",
  "common.fresh": "有效",
  "common.stale": "过期",
  "common.default": "默认",
  "common.any": "任意",
  "common.global": "全局",
  "common.none": "—",
  "common.loading": "加载中…",
  "common.error": "加载失败",
  "common.retry": "重试",
  "common.refresh": "刷新",
  "common.prev": "上一页",
  "common.next": "下一页",
  "common.details": "详情",
  "common.never": "从未",
  "common.ago": "{n}前",
  "common.justnow": "刚刚",
  "common.copied": "已复制",

  // stream status (derived admission health of a KV-event listener)
  "stream.status.healthy": "正常",
  "stream.status.stale": "过期",
  "stream.status.idle": "待事件",
  "stream.status.down": "断开",
  "stream.status.degraded": "异常",
  "stream.status.tip.healthy": "已连接且持续接收事件。",
  "stream.status.tip.stale": "已连接但近期无事件 — 准入可能将驻留视为过期。",
  "stream.status.tip.idle": "已连接，但尚未收到事件（冷启动监听器）。",
  "stream.status.tip.down": "监听器未连接到引擎。",
  "stream.status.tip.degraded": "正在接收事件，但检测到序列缺口或解码错误。",
  "streams.col.status": "状态",
  "streams.col.skipped": "跳过",
  "streams.col.last_event": "最近事件",
  "overview.stat.stale_streams": "过期 / 断开事件流",

  // cluster / federation
  "cluster.label": "集群",
  "cluster.all": "全部集群",
  "cluster.col": "集群",
  "cluster.backend": "后端",
  "overview.stat.clusters_count": "集群数",

  // nav
  "nav.overview": "总览",
  "nav.engines": "推理引擎",
  "nav.profiles": "模型配置",
  "nav.policies": "准入策略",
  "nav.streams": "KV 事件流",
  "nav.simulator": "前缀命中模拟器",
  "nav.decisions": "实时准入决策",
  "nav.audit": "配置审计",
  "nav.api_docs": "API 文档",

  // API docs
  "docs.title": "API 文档",
  "docs.subtitle": "基于 OpenAPI 展示 kv-indexer 和网关的 HTTP 接口。",
  "docs.raw": "打开 JSON",
  "docs.count": "{n} 个接口",
  "docs.col.method": "方法",
  "docs.col.path": "路径",
  "docs.col.summary": "说明",
  "docs.col.params": "参数",
  "docs.col.body": "请求体",
  "docs.empty": "未发现 OpenAPI 接口。",

  // overview
  "overview.title": "集群总览",
  "overview.subtitle": "按输入长度和缓存命中率做跨推理集群准入判定",
  "overview.stat.clusters": "集群数",
  "overview.stat.engines": "引擎数",
  "overview.stat.profiles": "模型配置",
  "overview.stat.healthy_streams": "正常事件流",
  "overview.stat.indexed_blocks": "已索引前缀块",
  "overview.stat.reject_rate": "429 拒绝率",
  "overview.stat.fallback_rate": "保守放行率",
  "overview.stat.decisions": "决策记录数",
  "overview.recent.title": "近期命中率",
  "overview.recent.desc": "最近 {n} 条准入决策的前缀缓存命中率(%)。",
  "overview.recent.empty": "暂无决策 — 请使用前缀模拟器或发送请求。",
  "overview.clusters.title": "集群",
  "overview.clusters.desc": "区域 · 环境 · 状态",
  "overview.clusters.empty": "暂无集群。",
  "overview.cluster.maintenance": "维护中",
  "overview.latest.title": "最新决策",
  "overview.latest.desc": "跨协议的最新准入判定。",
  "overview.latest.empty": "尚无决策记录。",
  "overview.col.time": "时间",
  "overview.col.protocol": "协议",
  "overview.col.model": "模型",
  "overview.col.decision": "决策",
  "overview.col.reason": "原因",
  "overview.col.tokens": "Token 数",
  "overview.col.hit": "命中率",

  // engines
  "engines.title": "引擎注册表",
  "engines.subtitle":
    "vLLM/SGLang 工作节点。可热切换启用、排空、健康状态，不丢失索引。",
  "engines.btn.register": "注册引擎",
  "engines.sheet.title": "注册引擎",
  "engines.sheet.desc":
    "接入一个 vLLM 或 SGLang 工作节点。前缀缓存感知需要 KV 事件端点。",
  "engines.col.engine": "引擎",
  "engines.col.cluster": "集群",
  "engines.col.framework": "框架",
  "engines.col.models": "提供模型",
  "engines.col.endpoint": "服务端点",
  "engines.col.kv_stream": "KV 事件",
  "engines.col.indexer": "Indexer",
  "engines.col.state": "状态",
  "engines.col.actions": "操作",
  "engines.empty": "尚未注册引擎。",
  "engines.status.draining": "排空中",
  "engines.status.unhealthy": "异常",
  "engines.action.disable": "停用",
  "engines.action.enable": "启用",
  "engines.action.drain": "排空",
  "engines.action.undrain": "取消排空",
  "engines.field.engine_id": "引擎 ID",
  "engines.field.cluster": "集群",
  "engines.field.framework": "框架",
  "engines.field.served": "提供的模型（逗号分隔）",
  "engines.field.api": "API 端点",
  "engines.field.tokenizer": "分词器端点",
  "engines.field.kv": "KV 事件端点 (ZMQ)",
  "engines.field.replay": "回放端点",
  "engines.field.target_backend": "目标 Indexer",
  "engines.no_indexers": "请先添加并启用一个 Indexer 连接，再注册推理引擎。",
  "engines.error.no_indexer": "请先选择目标 Indexer。",
  "engines.error.no_cluster": "请选择或填写引擎所属集群。",
  "engines.toast.update_failed": "更新失败",

  // indexers
  "indexers.title": "Indexer 连接",
  "indexers.desc":
    "由 kvgateway 管理并存入 MongoDB 的连接注册表。每一行都是一个可被网关访问的 kvindexer 后端。",
  "indexers.btn.add": "添加 Indexer",
  "indexers.btn.check": "检查",
  "indexers.sheet.new": "添加 Indexer",
  "indexers.sheet.edit": "编辑 Indexer: {id}",
  "indexers.sheet.desc":
    "填写某个地域或集群里的 kvindexer HTTP 地址。推理引擎会注册到你选择的这个 Indexer 内。",
  "indexers.field.id": "Indexer ID",
  "indexers.field.kind": "类型",
  "indexers.field.cluster": "归属集群",
  "indexers.field.display_name": "显示名称",
  "indexers.field.display_name_ph": "本地分词器",
  "indexers.field.url": "Indexer 地址",
  "indexers.field.token": "访问 Token",
  "indexers.field.token_keep": "留空则保留已有 Token",
  "indexers.field.token_optional": "可选",
  "indexers.field.enabled": "启用",
  "indexers.field.enabled_hint": "只有启用的 Indexer 会参与聚合查询和定向写入。",
  "indexers.kind.backend": "真实 Indexer",
  "indexers.kind.virtual": "虚拟",
  "indexers.col.id": "Indexer",
  "indexers.col.cluster": "归属集群",
  "indexers.col.url": "地址",
  "indexers.col.token": "Token",
  "indexers.col.state": "状态",
  "indexers.col.health": "健康检查",
  "indexers.col.actions": "操作",
  "indexers.empty": "尚未添加 Indexer 连接。",
  "indexers.token.set": "已设置",
  "indexers.token.none": "未设置",
  "indexers.health.unknown": "未检查",
  "indexers.health.ok": "正常",
  "indexers.health.failed": "失败",
  "indexers.health.missing": "/clusters-health 中没有这个 Indexer。",
  "indexers.health.unhealthy": "Gateway 返回该 Indexer 不健康。",
  "indexers.health.ok_detail": "可访问: {url}",
  "indexers.health.virtual_detail": "网关本地虚拟 Indexer",
  "indexers.health.ok_toast": "Indexer {id} 健康检查通过",
  "indexers.health.fail_toast": "Indexer {id} 健康检查失败",
  "indexers.confirm.delete": "删除 Indexer 连接 {id}？",
  "indexers.toast.saved": "Indexer 已保存",
  "indexers.toast.deleted": "Indexer 已删除",
  "indexers.toast.update_failed": "Indexer 更新失败",
  "indexers.toast.delete_failed": "Indexer 删除失败",

  // profiles
  "profiles.title": "模型配置",
  "profiles.subtitle":
    "分词与哈希规则。修改块大小、哈希方案或分词器会提升版本号，并隔离 request_key 命名空间。",
  "profiles.btn.new": "新建模型配置",
  "profiles.col.model": "模型",
  "profiles.col.framework": "框架",
  "profiles.col.tokenizer_source": "分词来源",
  "profiles.col.version": "版本",
  "profiles.col.hash": "哈希方案",
  "profiles.col.block": "块大小",
  "profiles.col.namespace": "命名空间",
  "profiles.col.features": "特性",
  "profiles.empty": "暂无模型配置。",
  "profiles.text_only": "纯文本",
  "profiles.sheet.new": "新建模型配置",
  "profiles.sheet.edit": "编辑 {id}",
  "profiles.sheet.desc":
    "分词器、哈希规则与块大小决定 request_key 的命名空间。任一变化都会提升模型配置版本。",
  "profiles.field.model": "模型 ID",
  "profiles.field.framework": "框架",
  "profiles.field.hash": "哈希方案",
  "profiles.field.block": "块大小",
  "profiles.field.block_hint": "qwen3.5-4b full_attention 组 = 528",
  "profiles.field.tokenizer": "分词器端点",
  "profiles.field.tokenizer_ph": "继承自引擎",
  "profiles.field.target_backend": "目标 Indexer / Cluster",
  "profiles.field.tokenizer_mode": "分词来源",
  "profiles.tokenizer_mode.remote": "远端引擎",
  "profiles.tokenizer_mode.local": "本地 sidecar",
  "profiles.field.tokenizer_zip": "Tokenizer zip",
  "profiles.field.template_file": "Chat template 文件",
  "profiles.field.template": "Chat template",
  "profiles.error.empty_template_file": "Chat template 文件为空。",
  "profiles.error.empty_tokenizer_zip": "Tokenizer zip 为空。",
  "profiles.field.seed": "哈希种子（命名空间）",
  "profiles.feature.lora": "LoRA",
  "profiles.feature.mm": "多模态",
  "profiles.feature.salt": "缓存盐值",
  "profiles.bump.title": "保存将创建 v{n}",
  "profiles.bump.desc":
    "本次修改会影响分词或哈希规则，将分配新的 request_key 命名空间；旧缓存驻留会按 TTL 失效，不会污染新查询。",
  "profiles.btn.save_new": "另存为新版本",
  "profiles.btn.move": "保存并移动",

  // policies
  "policies.title": "准入与命中策略",
  "policies.subtitle":
    "按优先级配置准入规则。单条规则内条件是 AND，规则之间按优先级命中第一条。",
  "policies.btn.new": "新建规则",
  "policies.btn.test": "测试规则",
  "policies.sheet.title": "新建策略规则",
  "policies.sheet.edit": "编辑策略：{id}",
  "policies.confirm.delete": "删除策略 {id}？",
  "policies.sheet.desc":
    "用 AND 条件描述要匹配的请求，再设置命中后执行的动作。",
  "policies.test.title": "测试规则命中",
  "policies.test.desc":
    "模拟一次请求形态，按优先级匹配规则，并查看第一条命中的规则。",
  "policies.list.title": "策略规则",
  "policies.list.desc": "规则按优先级从高到低执行。第一条全部条件都满足的启用规则会生效。",
  "policies.list.empty": "暂无策略规则。没有规则命中时，请求默认放行。",
  "policies.col.policy": "规则 ID",
  "policies.col.scope": "适用范围",
  "policies.col.long": "开始检查",
  "policies.col.hard": "直接拒绝",
  "policies.col.minhit": "最低命中率",
  "policies.col.ttl": "事件有效期",
  "policies.col.enabled": "状态",
  "policies.col.priority": "优先级",
  "policies.col.name": "名称",
  "policies.col.scope_cluster": "适用集群",
  "policies.col.conditions": "匹配条件",
  "policies.col.action": "动作",
  "policies.col.uncertain": "信号不确定",
  "policies.preview.title": "规则命中预览",
  "policies.preview.desc": "按请求形态测试会命中哪条规则，再真实发流量。",
  "policies.preview.btn": "预览",
  "policies.preview.merge": "应用规则",
  "policies.preview.long": "开始检查阈值",
  "policies.preview.hard": "直接拒绝阈值",
  "policies.preview.minhit": "最低命中率",
  "policies.preview.ttl": "KV 事件有效期",
  "policies.preview.stale": "事件过期处理",
  "policies.preview.weights": "GPU / CPU / 磁盘权重",
  "policies.preview.enabled": "最终状态",
  "policies.preview.matched": "命中规则",
  "policies.preview.no_match": "没有命中规则",
  "policies.preview.evaluated": "已检查规则",
  "policies.field.id": "规则 ID",
  "policies.field.id_ph": "tenant-a-qwen",
  "policies.field.name": "显示名称",
  "policies.field.name_ph": "256 Token 以上要求 KV 命中",
  "policies.field.priority": "优先级",
  "policies.field.scope_model": "适用模型",
  "policies.field.scope_tenant": "租户",
  "policies.field.scope_cluster": "适用集群",
  "policies.field.model": "模型",
  "policies.field.tenant": "租户",
  "policies.field.cluster": "集群",
  "policies.field.ph_any": "全部",
  "policies.field.long": "输入达到多少 Token 后检查",
  "policies.field.hard": "输入达到多少 Token 后直接拒绝",
  "policies.field.input_tokens": "输入 Token 数",
  "policies.field.preview_hit_ratio": "假设 KV 命中率",
  "policies.field.action": "命中后动作",
  "policies.field.low_hit": "命中率不足时",
  "policies.field.uncertain": "信号不确定时",
  "policies.field.reject_status": "拒绝状态码",
  "policies.field.minhit": "要求的 KV 最低命中率",
  "policies.field.ttl": "KV 事件有效期 (ms)",
  "policies.btn.save": "保存规则",
  "policies.btn.add_condition": "添加条件",
  "policies.storage_cluster": "存储: {cluster}",
  "policies.scope_cluster.desc":
    "选择集群会写入 cluster_id = 当前值的匹配条件；选择任意则不按集群限制。",
  "policies.error.target_cluster_required":
    "请先选择适用集群，或把右上角集群切换到某一个具体集群后再保存。",
  "policies.help.rule_id":
    "这条规则的稳定 ID，用于配置审计、API 响应以及更新/删除路径。",
  "policies.help.name":
    "给运维看的可读名称。留空时页面会显示规则 ID。",
  "policies.help.priority":
    "数值越大越先执行。一条规则命中后，后续低优先级规则不会再判断。",
  "policies.help.cluster":
    "这条规则匹配请求里的 cluster_id。选择 local-vllm 就只有 local-vllm 的准入请求能命中；选择任意则不按集群限制。",
  "policies.help.scope":
    "这条规则匹配哪些请求。模型、租户和集群都留空时，就是全局默认规则。",
  "policies.help.model":
    "请求里的模型名，通常就是客户端发给 OpenAI 或 Anthropic 接口的 model 字段。",
  "policies.help.tenant":
    "业务侧传入的租户、客户或工作空间 ID。kv-indexer 不会自己识别租户，只有请求里带 tenant_id 或等价元数据时才会用于匹配；没有多租户就留空。",
  "policies.help.check_after":
    "输入 Token 数低于这个值时，不强制检查 KV 命中率；达到或超过这个值后，会按最低命中率做准入判断。",
  "policies.help.reject_after":
    "输入 Token 数达到或超过这个值后，如果准入规则不通过，可以直接拒绝请求。这个值应低于模型上下文长度上限。",
  "policies.help.required_hit":
    "长请求必须已有多少比例的提示词 Token 命中 KV Cache。0.5 表示至少 50% 命中。",
  "policies.help.event_age":
    "KV 事件超过这个时间未更新时，监听器会被视为过期。过期事件流通常不做严格拒绝，因为驻留数据可能不完整。",
  "policies.help.status":
    "停用的规则会被忽略。没有任何启用规则命中时，请求默认放行。",
  "policies.help.enabled":
    "临时启用或停用这条规则，不需要删除规则本身。",
  "policies.help.stale_behavior":
    "当 KV 事件太旧或监听器过期时，准入逻辑如何处理。这里展示后端当前生效值。",
  "policies.help.weights":
    "GPU、CPU、磁盘三种 KV 驻留层级的命中计分权重。权重越高，该层级对命中分数贡献越大。",
  "policies.help.applied_rules":
    "生成最终策略时参与合并的规则，顺序从更宽泛的默认规则到更具体的匹配规则。",
  "policies.help.conditions":
    "同一条规则内的所有条件必须同时满足。条件列表为空表示匹配所有请求。",
  "policies.help.action":
    "规则命中后执行的动作：放行、拒绝，或要求 KV 命中率达到阈值。",
  "policies.help.input_tokens":
    "请求完成分词后的输入长度。通常用它来配置“达到多少 Token 后开始要求缓存命中率”。",
  "policies.help.preview_hit_ratio":
    "仅用于规则预览的假设命中率。真实准入仍会查询实时 prefix index。",
  "policies.help.low_hit":
    "KV 命中率低于最低要求时怎么处理。",
  "policies.help.uncertain":
    "当缓存信号不可信时怎么处理，例如分词失败、hash 特性不支持、没有可用引擎，或 KV 事件流不可用。",
  "policies.help.reject_status":
    "规则拒绝请求时返回的 HTTP 状态码。准入限流建议使用 429。",
  "policies.help.matched_rule":
    "这次预览中第一条全部 AND 条件都满足的启用规则。",
  "policies.help.result_reason":
    "后端返回的原因码，说明为什么放行、拒绝或保守放行。",
  "policies.help.evaluated_rules":
    "按优先级依次检查过的规则，直到命中第一条为止。",
  "policies.form.conditions": "匹配条件",
  "policies.form.conditions_desc":
    "这些条件会和上面的适用集群一起按 AND 匹配。不同规则之间按优先级 OR 命中。",
  "policies.form.action": "执行动作",
  "policies.conditions.all": "全部请求",
  "policies.conditions.all_desc": "没有条件时，这条规则会匹配所有请求。",
  "policies.condition.field": "字段",
  "policies.condition.op": "关系",
  "policies.condition.value": "值",
  "policies.placeholder.list": "值1, 值2",
  "policies.condition.field.cluster_id": "集群",
  "policies.condition.field.model_id": "模型",
  "policies.condition.field.tenant_id": "业务租户",
  "policies.condition.field.input_tokens": "输入 Token 数",
  "policies.condition.field.hit_ratio": "KV 命中率",
  "policies.condition.field.best_hit_tokens": "最长命中 Token",
  "policies.condition.field.effective_cached_tokens": "有效命中 Token",
  "policies.condition.field.kv_event_state": "KV 事件状态",
  "policies.condition.field.tokenized": "已分词",
  "policies.condition.field.hash_supported": "Hash 特性支持",
  "policies.condition.field.has_candidates": "存在可用引擎",
  "policies.condition.op.eq": "=",
  "policies.condition.op.neq": "!=",
  "policies.condition.op.in": "属于",
  "policies.condition.op.not_in": "不属于",
  "policies.condition.op.gt": ">",
  "policies.condition.op.gte": ">=",
  "policies.condition.op.lt": "<",
  "policies.condition.op.lte": "<=",
  "policies.condition.op.contains": "包含",
  "policies.action.accept": "放行",
  "policies.action.reject": "拒绝",
  "policies.action.require_cache_hit": "要求 KV 命中",
  "policies.action.require_hit": "要求 KV 命中",
  "policies.outcome.accept": "放行",
  "policies.outcome.reject": "拒绝",
  "policies.outcome.fallback_accept": "保守放行",

  // streams
  "streams.title": "KV 事件流",
  "streams.subtitle":
    "每个引擎的 ZMQ 监听状态：连接、序列、序列缺口、解码错误。准入所需的事件有效性据此推导。",
  "streams.listeners.title": "监听器",
  "streams.listeners.desc": "每个引擎的连接状态与事件吞吐。",
  "streams.col.engine": "引擎",
  "streams.col.endpoint": "监听端点",
  "streams.col.topic": "主题",
  "streams.col.connected": "连接",
  "streams.col.last_seq": "最后序列",
  "streams.col.events": "事件",
  "streams.col.gaps": "序列缺口",
  "streams.col.decode": "解码错误",
  "streams.col.queue": "积压队列",
  "streams.col.last_err": "最近错误",
  "streams.empty.listeners": "暂无监听器。",
  "streams.events.title": "实时 KV 事件",
  "streams.events.desc":
    "展示已解码的 ZMQ 事件。先加载近期事件，再跟随当前选中集群实时追加。",
  "streams.events.live": "实时",
  "streams.events.connecting": "连接中",
  "streams.events.select_cluster": "请选择集群",
  "streams.events.query": "查询 KV 事件",
  "streams.events.detail": "KV 事件详情",
  "streams.events.empty": "尚未观察到 KV 事件。",
  "streams.events.empty_filtered": "当前筛选条件下没有 KV 事件。",
  "streams.events.filter.indexed": "已入索引",
  "streams.events.filter.all": "全部",
  "streams.events.page_info": "第 {page}/{pages} 页 · 共 {total} 条 · 每页 10 条",
  "streams.events.col.time": "接收时间",
  "streams.events.col.kind": "类型",
  "streams.events.col.model": "模型",
  "streams.events.col.tier": "驻留层级",
  "streams.events.col.indexed": "已入索引",
  "streams.events.col.tokens": "Token 数",
  "streams.events.col.keys": "request_key",
  "streams.events.col.skip": "跳过原因",
  "streams.events.col.detail": "详情",
  "streams.events.raw_json": "原始 JSON",
  "streams.index.title": "缓存驻留索引",
  "streams.index.desc": "每个模型配置命名空间的前缀块计数。",
  "streams.col.namespace": "命名空间",
  "streams.col.req_keys": "前缀键",
  "streams.col.bridges": "引擎映射",
  "streams.col.engines": "引擎数",
  "streams.empty.index": "索引为空 — 尚未摄入事件。",

  // decisions
  "decisions.title": "实时准入决策",
  "decisions.subtitle":
    "近期准入判定：原因、命中率、目标引擎与配置版本。",
  "decisions.col.time": "时间",
  "decisions.col.protocol": "协议",
  "decisions.col.model": "模型",
  "decisions.col.tenant": "租户",
  "decisions.col.decision": "决策",
  "decisions.col.reason": "原因",
  "decisions.col.input": "输入 Token",
  "decisions.col.hit": "命中率",
  "decisions.col.target": "目标引擎",
  "decisions.col.cfg": "配置",
  "decisions.empty":
    "暂无决策。请使用前缀命中模拟器，或向 /v1/chat/completions、/v1/responses、/v1/messages 发送请求。",
  "decisions.filter.all": "全部决策",
  "decisions.filter.accept": "已接受",
  "decisions.filter.reject": "已拒绝（429）",
  "decisions.filter.fallback": "保守放行",
  "decisions.filter.none": "没有符合该筛选条件的决策。",
  "decisions.count": "显示 {shown}/{total} 条",

  // audit
  "audit.title": "配置审计",
  "audit.subtitle":
    "每次配置变更都会提升全局版本号。模型配置版本（隔离 request_key 命名空间）的提升会被标记。",
  "audit.col.version": "版本",
  "audit.col.time": "时间",
  "audit.col.action": "操作",
  "audit.col.entity": "对象",
  "audit.col.id": "ID",
  "audit.col.detail": "详情",
  "audit.col.flag": "提示",
  "audit.bump_badge": "模型配置版本提升",
  "audit.empty": "暂无配置变更记录。",

  // simulator
  "sim.title": "前缀命中模拟器",
  "sim.subtitle":
    "通过引擎分词、计算 request_key、查询缓存驻留情况，并运行准入判定 — 覆盖三种协议。",
  "sim.req.title": "请求",
  "sim.req.desc": "构造提示词并选择要模拟的协议。",
  "sim.field.model": "模型",
  "sim.field.protocol": "协议",
  "sim.field.text": "提示词 / 消息文本",
  "sim.btn.run": "运行完整流水线",
  "sim.btn.tokenize": "仅分词",
  "sim.btn.running": "运行中…",
  "sim.raw.title": "请求体",
  "sim.raw.desc": "发送到 {path} 的实际 JSON。",
  "sim.needs_cluster":
    "请先在右上角切换到具体集群 — 模拟器针对单个后端运行。",
  "sim.tok.title": "分词结果",
  "sim.tok.tokens": "{n} 个 Token",
  "sim.tok.blocks": "{n} 个前缀块",
  "sim.tok.block_size": "block_size {n}",
  "sim.tok.namespace": "命名空间",
  "sim.tok.req_keys": "request_key",
  "sim.hits.title": "各实例前缀命中",
  "sim.hits.empty": "无缓存驻留匹配（冷前缀）。",
  "sim.hits.matched": "已匹配 {n}",
  "sim.dec.reject": "429 拒绝",
  "sim.dec.accept": "接受",
  "sim.dec.input": "输入",
  "sim.dec.tok": "Token",
  "sim.dec.best": "最佳命中",
  "sim.dec.ratio": "命中率",
  "sim.dec.fallback": "保守放行",
  "sim.dec.target": "建议目标：",
  "sim.dec.min": "最低要求命中率: {min} · 实际 {got}%",
  "sim.dec.profile": "模型配置 v{p} · 配置 #{c} · 规则: {ids}",

  // protocols
  "protocol.openai.chat": "OpenAI Chat",
  "protocol.openai.responses": "OpenAI Responses",
  "protocol.anthropic.messages": "Anthropic Messages",
};

const dicts: Record<Locale, Dict> = { en, zh };

type Ctx = {
  locale: Locale;
  setLocale: (l: Locale) => void;
  t: (key: string, vars?: Record<string, string | number>) => string;
};

const I18nCtx = React.createContext<Ctx | null>(null);

const STORAGE_KEY = "kvi.locale";

function detect(): Locale {
  if (typeof window === "undefined") return "en";
  const saved = window.localStorage.getItem(STORAGE_KEY) as Locale | null;
  if (saved === "en" || saved === "zh") return saved;
  const lang = window.navigator.language.toLowerCase();
  return lang.startsWith("zh") ? "zh" : "en";
}

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = React.useState<Locale>("en");

  React.useEffect(() => {
    setLocaleState(detect());
  }, []);

  React.useEffect(() => {
    if (typeof document !== "undefined") {
      document.documentElement.lang = locale === "zh" ? "zh-CN" : "en";
    }
  }, [locale]);

  const setLocale = React.useCallback((l: Locale) => {
    setLocaleState(l);
    if (typeof window !== "undefined") {
      window.localStorage.setItem(STORAGE_KEY, l);
    }
  }, []);

  const t = React.useCallback(
    (key: string, vars?: Record<string, string | number>) => {
      const dict = dicts[locale];
      let s = dict[key] ?? dicts.en[key] ?? key;
      if (vars) {
        for (const [k, v] of Object.entries(vars)) {
          s = s.replace(new RegExp(`\\{${k}\\}`, "g"), String(v));
        }
      }
      return s;
    },
    [locale],
  );

  const value = React.useMemo(() => ({ locale, setLocale, t }), [locale, setLocale, t]);

  return <I18nCtx.Provider value={value}>{children}</I18nCtx.Provider>;
}

export function useI18n() {
  const ctx = React.useContext(I18nCtx);
  if (!ctx) throw new Error("useI18n must be used inside I18nProvider");
  return ctx;
}

export function useT() {
  return useI18n().t;
}
