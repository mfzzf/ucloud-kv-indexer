// API client for the ucloud-kv-indexer Go backend. By default browser requests
// stay same-origin and Next.js proxies them to the gateway. Set
// NEXT_PUBLIC_API_BASE only when the browser can directly reach the gateway.
export const API_BASE =
  process.env.NEXT_PUBLIC_API_BASE || "/api/kvi";

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers || {}) },
    cache: "no-store",
  });
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const msg =
      (data && data.error && (data.error.message || data.error.type)) ||
      `HTTP ${res.status}`;
    throw new Error(msg);
  }
  return data as T;
}

export const api = {
  get: <T>(p: string) => req<T>(p),
  post: <T>(p: string, body: unknown) =>
    req<T>(p, { method: "POST", body: JSON.stringify(body) }),
  patch: <T>(p: string, body: unknown) =>
    req<T>(p, { method: "PATCH", body: JSON.stringify(body) }),
  del: <T>(p: string) => req<T>(p, { method: "DELETE" }),
  raw: req,
};

// ---- multi-cluster federation (gateway) ----

// When the console points at the kvgateway, every fan-out list element is
// tagged with its origin cluster/backend. A plain single backend omits these.
export interface ClusterTag {
  _cluster?: string;
  _backend?: string;
}

export interface BackendHealth {
  id: string;
  url: string;
  healthy: boolean;
  error?: string;
}

export interface ClusterInfo {
  cluster: string;
  backends: BackendHealth[];
}

// ---- Types mirroring the Go API ----

export interface Cluster {
  cluster_id: string;
  display_name: string;
  region?: string;
  environment?: string;
  enabled: boolean;
  maintenance_mode: boolean;
  labels?: Record<string, string>;
  _cluster?: string;
  _backend?: string;
}

export interface Engine {
  engine_id: string;
  cluster_id: string;
  framework: string;
  api_endpoint: string;
  tokenizer_endpoint: string;
  kv_event_endpoint: string;
  replay_endpoint?: string;
  topic: string;
  served_models: string[];
  dp_ranks: number;
  max_num_seqs: number;
  max_model_len: number;
  queue_depth: number;
  healthy: boolean;
  draining: boolean;
  enabled: boolean;
  labels?: Record<string, string>;
  _cluster?: string;
  _backend?: string;
}

export interface ModelProfile {
  model_id: string;
  aliases?: string[];
  framework: string;
  version: number;
  tokenizer_endpoint?: string;
  hash_profile: string;
  block_size: number;
  hash_seed: string;
  supports_lora: boolean;
  supports_multimodal: boolean;
  supports_cache_salt: boolean;
  _cluster?: string;
  _backend?: string;
}

export interface RuleCondition {
  field: string;
  op: string;
  value?: string | number | boolean | Array<string | number | boolean>;
}

export interface RuleAction {
  type: "accept" | "reject" | "require_cache_hit" | string;
  min_hit_ratio?: number;
  on_low_hit?: "accept" | "reject" | "fallback_accept" | string;
  on_uncertain?: "accept" | "reject" | "fallback_accept" | string;
  reject_status?: number;
}

export interface Policy {
  rule_id: string;
  name?: string;
  priority: number;
  conditions?: RuleCondition[];
  action: RuleAction;
  enabled?: boolean;
  _cluster?: string;
  _backend?: string;
}

export interface AdmissionResult {
  decision: string;
  reason: string;
  http_status: number;
  fallback: boolean;
  min_required_hit_ratio: number;
  matched_rule_id?: string;
  matched_rule_name?: string;
  matched_rule_priority?: number;
  evaluated_rule_ids?: string[];
}

export interface PolicyPreview {
  rules: Policy[];
  result: AdmissionResult;
}

export interface StreamHealth {
  engine_id: string;
  endpoint: string;
  replay_endpoint?: string;
  topic: string;
  connected: boolean;
  last_seq: number;
  last_event_unix: number;
  events_total: number;
  gaps_total: number;
  decode_errors: number;
  // Events the listener observed but dropped (e.g. unindexed KV groups). Returned
  // by the backend even though older clients ignored it.
  skipped_total?: number;
  // Recv→apply backpressure buffer. A depth persistently near cap means the
  // index-apply step can't keep up with this engine's event rate (shard the
  // cluster — see docs/scaling.md). Older backends omit these.
  queue_depth?: number;
  queue_cap?: number;
  last_error?: string;
  _cluster?: string;
  _backend?: string;
}

export interface KVEventRecord {
  observed_at: string;
  engine_id: string;
  model: string;
  namespace?: string;
  seq: string;
  batch_ts?: number;
  dp_rank: number;
  kind: string;
  block_hashes?: string[];
  parent_hash?: string;
  token_ids?: number[];
  nested_token_ids?: boolean;
  block_size?: number;
  medium?: string;
  tier?: string;
  lora_id?: number;
  lora_name?: string;
  extra_keys?: string[];
  extra_key_count?: number;
  group_idx?: number;
  spec_kind?: string;
  sliding_window?: number;
  request_keys?: string[];
  indexed: boolean;
  skip_reason?: string;
  _cluster?: string;
  _backend?: string;
}

// Derived admission-relevant health of a KV-event listener. `connected` alone is
// misleading: a stream can be connected yet have gone quiet long enough that
// freshness-gated admission would treat it as stale. We fold connection state,
// time-since-last-event, and error counters into a single status.
export type StreamStatus = "down" | "stale" | "idle" | "healthy" | "degraded";

// staleAfterMs defaults to a conservative window when no policy TTL is known.
export function streamStatus(
  s: StreamHealth,
  nowMs: number = Date.now(),
  staleAfterMs = 60_000,
): StreamStatus {
  if (!s.connected) return "down";
  if (s.decode_errors > 0 || s.gaps_total > 0) return "degraded";
  // No event ever seen on a connected listener — it is up but cold, not stale.
  if (!s.last_event_unix || s.events_total === 0) return "idle";
  const ageMs = nowMs - s.last_event_unix * 1000;
  if (ageMs > staleAfterMs) return "stale";
  return "healthy";
}

export interface InstanceHit {
  longest_matched: number;
  gpu: number;
  cpu: number;
  disk: number;
  dp: Record<string, number>;
}

export interface QueryPrefixResponse {
  model_name: string;
  block_size: number;
  namespace: string;
  hash_profile: string;
  fresh: boolean;
  instances: Record<string, InstanceHit>;
}

export interface RouteRecord {
  timestamp: string;
  protocol: string;
  model: string;
  tenant_id: string;
  decision: string;
  reason: string;
  http_status: number;
  input_tokens: number;
  hit_ratio: number;
  best_hit_tokens: number;
  target_engine?: string;
  fallback: boolean;
  config_version: number;
  namespace: string;
  _cluster?: string;
  _backend?: string;
}

export interface AuditEntry {
  version: number;
  timestamp: string;
  action: string;
  entity: string;
  entity_id: string;
  version_bump: boolean;
  detail?: string;
  _cluster?: string;
  _backend?: string;
}

export interface IndexStat {
  namespace: string;
  request_keys: number;
  bridges: number;
  engines: number;
  last_event_unix: number;
  _cluster?: string;
  _backend?: string;
}

export interface TokenizePreview {
  model: string;
  namespace: string;
  block_size: number;
  count: number;
  tokens: number[];
  request_keys: number[];
}

export interface RouteResponse {
  decision: string;
  reason: string;
  http_status: number;
  target?: { cluster_id?: string; engine_id: string; endpoint?: string; dp_rank: number };
  config: {
    model_profile_version: number;
    namespace: string;
    evaluated_rule_ids?: string[];
    matched_rule_id?: string;
    matched_rule_name?: string;
    matched_rule_priority?: number;
    config_version: number;
  };
  cache: {
    instance_id?: string;
    input_tokens: number;
    best_hit_tokens: number;
    hit_ratio: number;
    gpu_tokens: number;
    cpu_tokens: number;
    disk_tokens: number;
    effective_cached_tokens: number;
  };
  fallback: boolean;
  protocol: string;
  error?: {
    type: string;
    input_tokens: number;
    best_hit_tokens: number;
    hit_ratio: number;
    min_required_hit_ratio: number;
  };
}
