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

// RuleCondition is one AND clause inside an admission rule. Every condition in
// a rule must match. Rules themselves are evaluated as an ordered OR list: the
// first enabled matching rule wins.
type RuleCondition struct {
	Field string `json:"field" bson:"field" yaml:"field"`
	Op    string `json:"op" bson:"op" yaml:"op"`
	Value any    `json:"value,omitempty" bson:"value,omitempty" yaml:"value,omitempty"`
}

const (
	ConditionOpEq       = "eq"
	ConditionOpNeq      = "neq"
	ConditionOpIn       = "in"
	ConditionOpNotIn    = "not_in"
	ConditionOpGT       = "gt"
	ConditionOpGTE      = "gte"
	ConditionOpLT       = "lt"
	ConditionOpLTE      = "lte"
	ConditionOpContains = "contains"
)

const (
	ConditionFieldClusterID             = "cluster_id"
	ConditionFieldModelID               = "model_id"
	ConditionFieldTenantID              = "tenant_id"
	ConditionFieldInputTokens           = "input_tokens"
	ConditionFieldHitRatio              = "hit_ratio"
	ConditionFieldBestHitTokens         = "best_hit_tokens"
	ConditionFieldEffectiveCachedTokens = "effective_cached_tokens"
	ConditionFieldKVEventState          = "kv_event_state"
	ConditionFieldTokenized             = "tokenized"
	ConditionFieldHashSupported         = "hash_supported"
	ConditionFieldHasCandidates         = "has_candidates"
)

const (
	KVEventStateAvailable = "available"
	KVEventStateStale     = "stale"
)

// RuleAction is executed when its parent rule matches.
type RuleAction struct {
	Type         string   `json:"type" bson:"type" yaml:"type"`
	MinHitRatio  *float64 `json:"min_hit_ratio,omitempty" bson:"min_hit_ratio,omitempty" yaml:"min_hit_ratio,omitempty"`
	OnLowHit     string   `json:"on_low_hit,omitempty" bson:"on_low_hit,omitempty" yaml:"on_low_hit,omitempty"`
	OnUncertain  string   `json:"on_uncertain,omitempty" bson:"on_uncertain,omitempty" yaml:"on_uncertain,omitempty"`
	RejectStatus int      `json:"reject_status,omitempty" bson:"reject_status,omitempty" yaml:"reject_status,omitempty"`
}

const (
	ActionAccept          = "accept"
	ActionReject          = "reject"
	ActionRequireCacheHit = "require_cache_hit"
)

const (
	RuleOutcomeAccept         = "accept"
	RuleOutcomeReject         = "reject"
	RuleOutcomeFallbackAccept = "fallback_accept"
)

// Policy is one admission rule. The persisted policy set is a priority-ordered
// OR list; each rule's Conditions are AND clauses. An empty condition list is a
// catch-all rule.
type Policy struct {
	RuleID     string          `json:"rule_id" bson:"rule_id" yaml:"rule_id"`
	Name       string          `json:"name,omitempty" bson:"name,omitempty" yaml:"name,omitempty"`
	Priority   int             `json:"priority" bson:"priority" yaml:"priority"`
	Conditions []RuleCondition `json:"conditions,omitempty" bson:"conditions,omitempty" yaml:"conditions,omitempty"`
	Action     RuleAction      `json:"action" bson:"action" yaml:"action"`
	// nil means enabled. This keeps API PATCH ergonomic while making newly
	// authored rules active unless they explicitly opt out.
	Enabled *bool `json:"enabled,omitempty" bson:"enabled,omitempty" yaml:"enabled,omitempty"`
}

func (p Policy) IsEnabled() bool {
	return p.Enabled == nil || *p.Enabled
}

func (p Policy) DisplayName() string {
	if p.Name != "" {
		return p.Name
	}
	return p.RuleID
}

func (a RuleAction) TypeOrDefault() string {
	if a.Type == "" {
		return ActionAccept
	}
	return a.Type
}

func (a RuleAction) RejectStatusOrDefault() int {
	if a.RejectStatus > 0 {
		return a.RejectStatus
	}
	return 429
}

func (a RuleAction) MinHitRatioOrDefault() float64 {
	if a.MinHitRatio != nil {
		return *a.MinHitRatio
	}
	return 0
}

func (a RuleAction) OnLowHitOrDefault() string {
	if a.OnLowHit != "" {
		return a.OnLowHit
	}
	return RuleOutcomeReject
}

func (a RuleAction) OnUncertainOrDefault() string {
	if a.OnUncertain != "" {
		return a.OnUncertain
	}
	return RuleOutcomeFallbackAccept
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
