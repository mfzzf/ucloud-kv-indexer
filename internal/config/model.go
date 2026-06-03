// Package config holds the dynamically-editable configuration model:
// clusters, engines, model profiles, and routing/admission policies. Every
// mutation bumps a global config version and appends an audit entry. The store
// is in-memory with optional JSON snapshot persistence.
package config

import "time"

// Framework identifies the serving engine kind.
type Framework string

const (
	FrameworkVLLM   Framework = "vllm"
	FrameworkSGLang Framework = "sglang"
)

// Cluster is a placement / management boundary (a site or logical cluster).
type Cluster struct {
	ClusterID       string            `json:"cluster_id"`
	DisplayName     string            `json:"display_name"`
	Region          string            `json:"region,omitempty"`
	Environment     string            `json:"environment,omitempty"`
	Enabled         bool              `json:"enabled"`
	MaintenanceMode bool              `json:"maintenance_mode"`
	Labels          map[string]string `json:"labels,omitempty"`
}

// Engine is a single vLLM/SGLang worker (or DP group).
type Engine struct {
	EngineID          string    `json:"engine_id"`
	ClusterID         string    `json:"cluster_id"`
	Framework         Framework `json:"framework"`
	APIEndpoint       string    `json:"api_endpoint"`       // OpenAI-compatible base
	TokenizerEndpoint string    `json:"tokenizer_endpoint"` // e.g. http://host:8000
	KVEventEndpoint   string    `json:"kv_event_endpoint"`  // ZMQ SUB connect addr
	ReplayEndpoint    string    `json:"replay_endpoint,omitempty"`
	Topic             string    `json:"topic"`
	ServedModels      []string  `json:"served_models"`
	DPRanks           int       `json:"dp_ranks"`
	// Capacity / load (hot-updatable, used by admission capacity checks).
	MaxNumSeqs  int               `json:"max_num_seqs"`
	MaxModelLen int               `json:"max_model_len"`
	QueueDepth  int               `json:"queue_depth"` // current, hot-updated
	Healthy     bool              `json:"healthy"`
	Draining    bool              `json:"draining"`
	Enabled     bool              `json:"enabled"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// ModelProfile captures tokenization + hash semantics. Changes that affect
// token/hash meaning must bump Version (the UI enforces create-new-version),
// because Version participates in the request_key namespace.
type ModelProfile struct {
	ModelID   string    `json:"model_id"`
	Aliases   []string  `json:"aliases,omitempty"`
	Framework Framework `json:"framework"`
	Version   int       `json:"version"`
	// TokenizerEndpoint may be empty to inherit from the resolved engine.
	TokenizerEndpoint string `json:"tokenizer_endpoint,omitempty"`
	// HashProfile is an opaque label recorded on decisions, e.g. "vllm-v1-text".
	HashProfile string `json:"hash_profile"`
	BlockSize   int    `json:"block_size"`
	// HashSeed seeds the deterministic request_key chain. Part of the namespace.
	HashSeed           string `json:"hash_seed"`
	SupportsLoRA       bool   `json:"supports_lora"`
	SupportsMultimodal bool   `json:"supports_multimodal"`
	SupportsCacheSalt  bool   `json:"supports_cache_salt"`
}

// Namespace is the request_key isolation key. Any change to a token/hash-
// affecting field must change this string (via Version bump).
func (p ModelProfile) Namespace() string {
	return p.ModelID + "/v" + itoa(p.Version) + "/" + p.HashProfile + "/" + itoa(p.BlockSize)
}

// Scope identifies what a policy applies to. Empty fields are wildcards.
// Resolution order (lowest→highest precedence): global < cluster < model <
// tenant. The most specific matching policy's fields win field-by-field.
type Scope struct {
	ClusterID string `json:"cluster_id,omitempty"`
	ModelID   string `json:"model_id,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`
}

// Policy is a routing + admission policy. Pointer fields allow partial
// overrides during effective-policy merge (nil = inherit).
type Policy struct {
	PolicyID string `json:"policy_id"`
	Scope    Scope  `json:"scope"`

	LongPromptThresholdTokens     *int     `json:"long_prompt_threshold_tokens,omitempty"`
	HardLongPromptThresholdTokens *int     `json:"hard_long_prompt_threshold_tokens,omitempty"`
	MinHitRatioForLongPrompt      *float64 `json:"min_hit_ratio_for_long_prompt,omitempty"`
	EventFreshnessTTLMs           *int     `json:"event_freshness_ttl_ms,omitempty"`
	// StaleEventBehavior: "fallback" (accept) or "reject_hard" (429 only above
	// hard threshold).
	StaleEventBehavior *string `json:"stale_event_behavior,omitempty"`
	// LowHitRejectStatus is the HTTP status for low-hit long prompts (429).
	LowHitRejectStatus *int `json:"low_hit_reject_status,omitempty"`
	// Tier weights for hit-token credit when computing effective cached tokens.
	GPUHitWeight  *float64 `json:"gpu_hit_weight,omitempty"`
	CPUHitWeight  *float64 `json:"cpu_hit_weight,omitempty"`
	DiskHitWeight *float64 `json:"disk_hit_weight,omitempty"`
	// Enabled toggles the whole admission judgment; when false, always accept.
	Enabled *bool `json:"enabled,omitempty"`
}

// EffectivePolicy is a fully-resolved policy with no nil fields.
type EffectivePolicy struct {
	LongPromptThresholdTokens     int     `json:"long_prompt_threshold_tokens"`
	HardLongPromptThresholdTokens int     `json:"hard_long_prompt_threshold_tokens"`
	MinHitRatioForLongPrompt      float64 `json:"min_hit_ratio_for_long_prompt"`
	EventFreshnessTTLMs           int     `json:"event_freshness_ttl_ms"`
	StaleEventBehavior            string  `json:"stale_event_behavior"`
	LowHitRejectStatus            int     `json:"low_hit_reject_status"`
	GPUHitWeight                  float64 `json:"gpu_hit_weight"`
	CPUHitWeight                  float64 `json:"cpu_hit_weight"`
	DiskHitWeight                 float64 `json:"disk_hit_weight"`
	Enabled                       bool    `json:"enabled"`
	// SourcePolicyIDs lists the policies that contributed, in merge order.
	SourcePolicyIDs []string `json:"source_policy_ids"`
}

// DefaultEffectivePolicy is the global default (PLAN.md recommended config).
func DefaultEffectivePolicy() EffectivePolicy {
	return EffectivePolicy{
		LongPromptThresholdTokens:     8192,
		HardLongPromptThresholdTokens: 32768,
		MinHitRatioForLongPrompt:      0.50,
		EventFreshnessTTLMs:           5000,
		StaleEventBehavior:            "fallback",
		LowHitRejectStatus:            429,
		GPUHitWeight:                  1.0,
		CPUHitWeight:                  0.75,
		DiskHitWeight:                 0.25,
		Enabled:                       true,
		SourcePolicyIDs:               []string{"__global_default__"},
	}
}

// AuditEntry records one configuration mutation.
type AuditEntry struct {
	Version   int       `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Entity    string    `json:"entity"`
	EntityID  string    `json:"entity_id"`
	// VersionBump is true when the change forced a ModelProfile version bump.
	VersionBump bool   `json:"version_bump"`
	Detail      string `json:"detail,omitempty"`
}

func itoa(i int) string {
	// small, allocation-light int->string for namespace keys
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
