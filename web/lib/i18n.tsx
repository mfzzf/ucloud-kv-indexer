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
  "common.enabled": "enabled",
  "common.disabled": "disabled",
  "common.on": "on",
  "common.off": "off",
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
  "nav.policies": "Routing Policy",
  "nav.streams": "KV Event Streams",
  "nav.simulator": "Prefix Simulator",
  "nav.decisions": "Live Decisions",
  "nav.audit": "Config Audit",

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
  "engines.field.target_backend": "Target cluster / backend",
  "engines.toast.update_failed": "Update failed",

  // profiles
  "profiles.title": "Model Profiles",
  "profiles.subtitle":
    "Tokenization + hash semantics. Changing block size / hash profile / tokenizer bumps the version and isolates the request-key namespace.",
  "profiles.btn.new": "New profile",
  "profiles.col.model": "Model",
  "profiles.col.framework": "Framework",
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
  "profiles.field.seed": "Hash seed (namespace)",
  "profiles.feature.lora": "LoRA",
  "profiles.feature.mm": "Multimodal",
  "profiles.feature.salt": "Cache salt",
  "profiles.bump.title": "Saving will create v{n}",
  "profiles.bump.desc":
    "This change affects tokenization or hashing. A fresh request-key namespace will be allocated; old residency will TTL out, not corrupt new queries.",
  "profiles.btn.save_new": "Save as new version",

  // policies
  "policies.title": "Routing Policy",
  "policies.subtitle":
    "Per global / cluster / model / tenant scope. Resolution order: global < cluster < model < tenant. Preview merges before saving.",
  "policies.btn.new": "New policy",
  "policies.sheet.title": "New policy",
  "policies.sheet.desc":
    "Set thresholds for one scope. Empty fields fall back to parent-scope values.",
  "policies.list.title": "Configured policies",
  "policies.list.desc": "Empty rows mean global defaults apply.",
  "policies.list.empty": "No policies — global defaults apply.",
  "policies.col.policy": "Policy",
  "policies.col.scope": "Scope",
  "policies.col.long": "Long≥",
  "policies.col.hard": "Hard≥",
  "policies.col.minhit": "Min hit",
  "policies.col.ttl": "TTL",
  "policies.col.enabled": "Enabled",
  "policies.preview.title": "Effective policy preview",
  "policies.preview.desc":
    "Resolves merge chain across scopes for a hypothetical request.",
  "policies.preview.btn": "Resolve",
  "policies.preview.merge": "merge chain",
  "policies.field.id": "Policy ID",
  "policies.field.id_ph": "tenant-a-qwen",
  "policies.field.scope_model": "Scope: model",
  "policies.field.scope_tenant": "Scope: tenant",
  "policies.field.scope_cluster": "Scope: cluster",
  "policies.field.model": "Model",
  "policies.field.tenant": "Tenant",
  "policies.field.cluster": "Cluster",
  "policies.field.ph_any": "(any)",
  "policies.field.long": "Long prompt threshold (tokens)",
  "policies.field.hard": "Hard cap (tokens)",
  "policies.field.minhit": "Min hit ratio (long)",
  "policies.field.ttl": "Event freshness TTL (ms)",
  "policies.btn.save": "Save policy",

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
  "sim.dec.profile": "profile v{p} · config #{c} · policies: {ids}",

  // protocols
  "protocol.openai.chat": "OpenAI Chat",
  "protocol.openai.responses": "OpenAI Responses",
  "protocol.anthropic.messages": "Anthropic Messages",
};

const zh: Dict = {
  // brand / chrome
  "app.brand": "kv-indexer",
  "app.brand.subtitle": "准入控制台",
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
  "common.enabled": "已启用",
  "common.disabled": "已停用",
  "common.on": "开启",
  "common.off": "关闭",
  "common.up": "在线",
  "common.down": "离线",
  "common.fresh": "新鲜",
  "common.stale": "过期",
  "common.default": "默认",
  "common.any": "任意",
  "common.global": "全局",
  "common.none": "—",
  "common.loading": "加载中…",
  "common.error": "加载失败",
  "common.retry": "重试",
  "common.refresh": "刷新",
  "common.never": "从未",
  "common.ago": "{n}前",
  "common.justnow": "刚刚",
  "common.copied": "已复制",

  // stream status (derived admission health of a KV-event listener)
  "stream.status.healthy": "健康",
  "stream.status.stale": "过期",
  "stream.status.idle": "空闲",
  "stream.status.down": "离线",
  "stream.status.degraded": "降级",
  "stream.status.tip.healthy": "已连接且持续接收事件。",
  "stream.status.tip.stale": "已连接但近期无事件 — 准入可能将驻留视为过期。",
  "stream.status.tip.idle": "已连接，尚无事件(冷监听器)。",
  "stream.status.tip.down": "监听器未连接到引擎。",
  "stream.status.tip.degraded": "在接收事件，但检测到间隙或解码错误。",
  "streams.col.status": "状态",
  "streams.col.skipped": "已跳过",
  "streams.col.last_event": "最近事件",
  "overview.stat.stale_streams": "过期 / 离线 事件流",

  // cluster / federation
  "cluster.label": "集群",
  "cluster.all": "全部集群",
  "cluster.col": "集群",
  "cluster.backend": "后端",
  "overview.stat.clusters_count": "集群数",

  // nav
  "nav.overview": "总览",
  "nav.engines": "推理引擎",
  "nav.profiles": "模型档案",
  "nav.policies": "路由策略",
  "nav.streams": "KV 事件流",
  "nav.simulator": "前缀模拟器",
  "nav.decisions": "实时决策",
  "nav.audit": "配置审计",

  // overview
  "overview.title": "集群总览",
  "overview.subtitle": "跨推理集群的长度 + 缓存命中率准入判定",
  "overview.stat.clusters": "集群数",
  "overview.stat.engines": "引擎数",
  "overview.stat.profiles": "模型档案",
  "overview.stat.healthy_streams": "健康事件流",
  "overview.stat.indexed_blocks": "已索引前缀块",
  "overview.stat.reject_rate": "429 拒绝率",
  "overview.stat.fallback_rate": "降级率",
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
  "overview.col.tokens": "Token",
  "overview.col.hit": "命中",

  // engines
  "engines.title": "引擎注册表",
  "engines.subtitle":
    "vLLM/SGLang 工作节点。在不丢索引的前提下热切换 启用/排空/健康 状态。",
  "engines.btn.register": "注册引擎",
  "engines.sheet.title": "注册引擎",
  "engines.sheet.desc":
    "接入一个 vLLM 或 SGLang 工作节点。前缀缓存感知需要 KV 事件端点。",
  "engines.col.engine": "引擎",
  "engines.col.cluster": "集群",
  "engines.col.framework": "框架",
  "engines.col.models": "模型",
  "engines.col.endpoint": "端点",
  "engines.col.kv_stream": "KV 事件",
  "engines.col.state": "状态",
  "engines.col.actions": "操作",
  "engines.empty": "尚未注册引擎。",
  "engines.status.draining": "排空中",
  "engines.status.unhealthy": "不健康",
  "engines.action.disable": "停用",
  "engines.action.enable": "启用",
  "engines.action.drain": "排空",
  "engines.action.undrain": "取消排空",
  "engines.field.engine_id": "引擎 ID",
  "engines.field.cluster": "集群",
  "engines.field.framework": "框架",
  "engines.field.served": "服务模型(逗号分隔)",
  "engines.field.api": "API 端点",
  "engines.field.tokenizer": "Tokenizer 端点",
  "engines.field.kv": "KV 事件端点 (ZMQ)",
  "engines.field.replay": "Replay 端点",
  "engines.field.target_backend": "目标集群 / 后端",
  "engines.toast.update_failed": "更新失败",

  // profiles
  "profiles.title": "模型档案",
  "profiles.subtitle":
    "Tokenization 与哈希语义。修改块大小 / 哈希档案 / Tokenizer 会提升版本号并隔离 request-key 命名空间。",
  "profiles.btn.new": "新建档案",
  "profiles.col.model": "模型",
  "profiles.col.framework": "框架",
  "profiles.col.version": "版本",
  "profiles.col.hash": "哈希档案",
  "profiles.col.block": "块大小",
  "profiles.col.namespace": "命名空间",
  "profiles.col.features": "特性",
  "profiles.empty": "暂无档案。",
  "profiles.text_only": "纯文本",
  "profiles.sheet.new": "新建档案",
  "profiles.sheet.edit": "编辑 {id}",
  "profiles.sheet.desc":
    "Tokenizer、哈希与块大小决定 request-key 的命名空间。任一变化都会提升档案版本。",
  "profiles.field.model": "模型 ID",
  "profiles.field.framework": "框架",
  "profiles.field.hash": "哈希档案",
  "profiles.field.block": "块大小",
  "profiles.field.block_hint": "qwen3.5-4b full_attention 组 = 528",
  "profiles.field.tokenizer": "Tokenizer 端点",
  "profiles.field.tokenizer_ph": "继承自引擎",
  "profiles.field.seed": "哈希种子(命名空间)",
  "profiles.feature.lora": "LoRA",
  "profiles.feature.mm": "多模态",
  "profiles.feature.salt": "Cache salt",
  "profiles.bump.title": "保存将创建 v{n}",
  "profiles.bump.desc":
    "本次修改会影响 tokenization 或哈希,新的 request-key 命名空间将被分配;旧驻留会按 TTL 失效,不会污染新查询。",
  "profiles.btn.save_new": "另存为新版本",

  // policies
  "policies.title": "路由策略",
  "policies.subtitle":
    "支持 全局/集群/模型/租户 作用域。优先级:全局 < 集群 < 模型 < 租户。保存前可预览合并结果。",
  "policies.btn.new": "新建策略",
  "policies.sheet.title": "新建策略",
  "policies.sheet.desc": "为单一作用域设置阈值。留空则继承上层作用域。",
  "policies.list.title": "已配置策略",
  "policies.list.desc": "空行表示沿用全局默认值。",
  "policies.list.empty": "暂无策略 — 沿用全局默认。",
  "policies.col.policy": "策略",
  "policies.col.scope": "作用域",
  "policies.col.long": "长≥",
  "policies.col.hard": "硬≥",
  "policies.col.minhit": "最低命中",
  "policies.col.ttl": "TTL",
  "policies.col.enabled": "启用",
  "policies.preview.title": "有效策略预览",
  "policies.preview.desc": "针对一次假设请求,解析跨作用域的合并链。",
  "policies.preview.btn": "解析",
  "policies.preview.merge": "合并链",
  "policies.field.id": "策略 ID",
  "policies.field.id_ph": "tenant-a-qwen",
  "policies.field.scope_model": "作用域: 模型",
  "policies.field.scope_tenant": "作用域: 租户",
  "policies.field.scope_cluster": "作用域: 集群",
  "policies.field.model": "模型",
  "policies.field.tenant": "租户",
  "policies.field.cluster": "集群",
  "policies.field.ph_any": "(任意)",
  "policies.field.long": "长 prompt 阈值 (tokens)",
  "policies.field.hard": "硬上限 (tokens)",
  "policies.field.minhit": "长 prompt 最低命中率",
  "policies.field.ttl": "事件新鲜度 TTL (ms)",
  "policies.btn.save": "保存策略",

  // streams
  "streams.title": "KV 事件流",
  "streams.subtitle":
    "每个引擎的 ZMQ 监听健康度: 连接、序列、间隙、解码错误。准入新鲜度据此推导。",
  "streams.listeners.title": "监听器",
  "streams.listeners.desc": "每个引擎的连接状态与事件吞吐。",
  "streams.col.engine": "引擎",
  "streams.col.endpoint": "端点",
  "streams.col.topic": "Topic",
  "streams.col.connected": "连接",
  "streams.col.last_seq": "最后序列",
  "streams.col.events": "事件",
  "streams.col.gaps": "间隙",
  "streams.col.decode": "解码错误",
  "streams.col.queue": "队列",
  "streams.col.last_err": "最近错误",
  "streams.empty.listeners": "暂无监听器。",
  "streams.index.title": "驻留索引",
  "streams.index.desc": "每个档案命名空间的前缀块计数。",
  "streams.col.namespace": "命名空间",
  "streams.col.req_keys": "前缀块",
  "streams.col.bridges": "引擎桥接",
  "streams.col.engines": "引擎数",
  "streams.empty.index": "索引为空 — 尚未摄入事件。",

  // decisions
  "decisions.title": "实时决策",
  "decisions.subtitle":
    "近期准入判定: 原因、命中率、目标引擎与配置版本。",
  "decisions.col.time": "时间",
  "decisions.col.protocol": "协议",
  "decisions.col.model": "模型",
  "decisions.col.tenant": "租户",
  "decisions.col.decision": "决策",
  "decisions.col.reason": "原因",
  "decisions.col.input": "输入",
  "decisions.col.hit": "命中率",
  "decisions.col.target": "目标",
  "decisions.col.cfg": "配置",
  "decisions.empty":
    "暂无决策。请使用前缀模拟器或向 /v1/chat/completions、/v1/responses、/v1/messages 发送请求。",
  "decisions.filter.all": "全部决策",
  "decisions.filter.accept": "已接受",
  "decisions.filter.reject": "已拒绝 (429)",
  "decisions.filter.fallback": "降级",
  "decisions.filter.none": "没有符合该筛选条件的决策。",
  "decisions.count": "{total} 条中的 {shown} 条",

  // audit
  "audit.title": "配置审计",
  "audit.subtitle":
    "每次配置变更都会提升全局版本号。档案版本(隔离 request-key 命名空间)的提升会被标记。",
  "audit.col.version": "版本",
  "audit.col.time": "时间",
  "audit.col.action": "动作",
  "audit.col.entity": "实体",
  "audit.col.id": "ID",
  "audit.col.detail": "详情",
  "audit.col.flag": "标记",
  "audit.bump_badge": "档案版本提升",
  "audit.empty": "暂无配置变更记录。",

  // simulator
  "sim.title": "前缀查询模拟器",
  "sim.subtitle":
    "通过引擎做 tokenize、计算 request-key、查询驻留情况、运行准入判定 — 覆盖三种协议。",
  "sim.req.title": "请求",
  "sim.req.desc": "构造 prompt 并选择要模拟的协议。",
  "sim.field.model": "模型",
  "sim.field.protocol": "协议",
  "sim.field.text": "Prompt / 消息文本",
  "sim.btn.run": "运行完整流水线",
  "sim.btn.tokenize": "仅 Tokenize",
  "sim.btn.running": "运行中…",
  "sim.raw.title": "请求体",
  "sim.raw.desc": "发送到 {path} 的实际 JSON。",
  "sim.needs_cluster":
    "请先在右上角切换到具体集群 — 模拟器针对单个后端运行。",
  "sim.tok.title": "Tokenization",
  "sim.tok.tokens": "{n} tokens",
  "sim.tok.blocks": "{n} 个前缀块",
  "sim.tok.block_size": "block_size {n}",
  "sim.tok.namespace": "命名空间",
  "sim.tok.req_keys": "request keys",
  "sim.hits.title": "各实例前缀命中",
  "sim.hits.empty": "无驻留匹配 (冷前缀)。",
  "sim.hits.matched": "已匹配 {n}",
  "sim.dec.reject": "429 拒绝",
  "sim.dec.accept": "接受",
  "sim.dec.input": "输入",
  "sim.dec.tok": "tok",
  "sim.dec.best": "最佳命中",
  "sim.dec.ratio": "比率",
  "sim.dec.fallback": "降级",
  "sim.dec.target": "建议目标:",
  "sim.dec.min": "最低要求命中率: {min} · 实际 {got}%",
  "sim.dec.profile": "档案 v{p} · 配置 #{c} · 策略: {ids}",

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
