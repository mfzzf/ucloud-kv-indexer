package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Bootstrap is the human-authored seed configuration. It is the single
// YAML that describes topology: every cluster (each with the
// gateway backend URL(s) that front its kvindexer), every engine, model
// profile, and policy.
//
// Two write styles are accepted and produce the SAME normalized result:
//
//   - NESTED (preferred): write `engines:` and `models:` UNDER each cluster, and
//     a cluster-level `framework:`. Engines inherit the cluster's id + framework;
//     models inherit the cluster's framework and a default hash_profile. This
//     reads like "one cluster configures everything it owns".
//   - FLAT (legacy): top-level `engines:` / `profiles:` lists, each repeating
//     `cluster_id` / `framework`.
//
// LoadBootstrap flattens the nested form into the flat lists, so downstream code
// (and the normalized clusters/engines/profiles SQLite tables) only ever sees
// the flat, de-duplicated representation.
//
// Two consumers read the same file:
//   - kvgateway reads clusters[].backends to build its federated backend list,
//     keyed by cluster (the console selects a cluster with ?cluster=).
//   - kvindexer applies it ONCE when its store is empty (fresh backend); after
//     that the runtime store (memory/SQLite/file) is the source of truth and is
//     what the API/console mutate. A kvindexer may be scoped to one cluster with
//     -cluster <id>, in which case only that cluster + its engines are seeded and
//     only those ZMQ streams are subscribed; without -cluster it seeds the whole
//     topology (single process fronting several clusters over TCP).
//
// Backward compatibility: the legacy single `cluster:` block is still accepted
// and merged into the cluster list.
type Bootstrap struct {
	// Cluster is the legacy single-cluster form (kept for old seed files).
	Cluster BootstrapCluster `yaml:"cluster"`
	// Clusters is the multi-cluster form. Prefer this.
	Clusters []BootstrapCluster `yaml:"clusters"`
	// Engines / Profiles are the FLAT (legacy) lists. After LoadBootstrap they
	// also contain everything flattened out of clusters[].engines / .models.
	Engines  []BootstrapEngine  `yaml:"engines"`
	Profiles []BootstrapProfile `yaml:"profiles"`
	Policies []BootstrapPolicy  `yaml:"policies"`
}

type BootstrapCluster struct {
	ClusterID   string `yaml:"cluster_id"`
	DisplayName string `yaml:"display_name"`
	Region      string `yaml:"region"`
	Environment string `yaml:"environment"`
	Enabled     *bool  `yaml:"enabled"`
	// Backends are the kvindexer base URLs that serve this cluster, consumed by
	// kvgateway (ignored by kvindexer). e.g. ["http://10.0.0.1:8090"].
	Backends []string `yaml:"backends"`
	// Framework is the cluster-level default ("vllm"/"sglang") inherited by
	// nested engines and models that don't set their own.
	Framework string `yaml:"framework"`
	// Engines / Models are the NESTED form: entries here are flattened into the
	// top-level lists by LoadBootstrap, inheriting ClusterID + Framework.
	Engines []BootstrapEngine  `yaml:"engines"`
	Models  []BootstrapProfile `yaml:"models"`
}

type BootstrapEngine struct {
	EngineID          string   `yaml:"engine_id"`
	ClusterID         string   `yaml:"cluster_id"`
	Framework         string   `yaml:"framework"`
	APIEndpoint       string   `yaml:"api_endpoint"`
	TokenizerEndpoint string   `yaml:"tokenizer_endpoint"`
	KVEventEndpoint   string   `yaml:"kv_event_endpoint"`
	ReplayEndpoint    string   `yaml:"replay_endpoint"`
	Topic             string   `yaml:"topic"`
	ServedModels      []string `yaml:"served_models"`
	DPRanks           int      `yaml:"dp_ranks"`
	MaxNumSeqs        int      `yaml:"max_num_seqs"`
	MaxModelLen       int      `yaml:"max_model_len"`
}

type BootstrapProfile struct {
	ModelID            string `yaml:"model_id"`
	Framework          string `yaml:"framework"`
	HashProfile        string `yaml:"hash_profile"`
	BlockSize          int    `yaml:"block_size"`
	HashSeed           string `yaml:"hash_seed"`
	TokenizerEndpoint  string `yaml:"tokenizer_endpoint"`
	TokenizerMode      string `yaml:"tokenizer_mode"`
	ChatTemplateSHA256 string `yaml:"chat_template_sha256"`
}

type BootstrapPolicy struct {
	RuleID     string          `yaml:"rule_id"`
	Name       string          `yaml:"name"`
	Priority   int             `yaml:"priority"`
	Conditions []RuleCondition `yaml:"conditions"`
	Action     RuleAction      `yaml:"action"`
	Enabled    *bool           `yaml:"enabled"`
}

// LoadBootstrap parses a YAML bootstrap/config file and flattens the nested form.
func LoadBootstrap(path string) (*Bootstrap, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bs Bootstrap
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true) // reject typos / unknown keys so a bad seed fails loudly
	if err := dec.Decode(&bs); err != nil {
		return nil, fmt.Errorf("parse bootstrap %s: %w", path, err)
	}
	bs.flattenNested()
	return &bs, nil
}

// flattenNested moves clusters[].engines and clusters[].models into the flat
// bs.Engines / bs.Profiles lists, inheriting the cluster's id and framework (and
// a default hash_profile of "<framework>-v1-text"). It is idempotent-safe to
// call once after decode. Nested fields are then cleared so AllClusters reports
// clean cluster rows.
func (bs *Bootstrap) flattenNested() {
	apply := func(c *BootstrapCluster) {
		for _, e := range c.Engines {
			if e.ClusterID == "" {
				e.ClusterID = c.ClusterID
			}
			if e.Framework == "" {
				e.Framework = c.Framework
			}
			bs.Engines = append(bs.Engines, e)
		}
		for _, m := range c.Models {
			if m.Framework == "" {
				m.Framework = c.Framework
			}
			if m.HashProfile == "" && m.Framework != "" {
				m.HashProfile = m.Framework + "-v1-text"
			}
			bs.Profiles = append(bs.Profiles, m)
		}
		c.Engines = nil
		c.Models = nil
	}
	for i := range bs.Clusters {
		apply(&bs.Clusters[i])
	}
	apply(&bs.Cluster)
}

// AllClusters returns the merged cluster list (legacy `cluster:` + `clusters:`),
// de-duplicated by id with the list form taking precedence.
func (bs *Bootstrap) AllClusters() []BootstrapCluster {
	var out []BootstrapCluster
	seen := map[string]bool{}
	for _, c := range bs.Clusters {
		if c.ClusterID == "" || seen[c.ClusterID] {
			continue
		}
		seen[c.ClusterID] = true
		out = append(out, c)
	}
	if bs.Cluster.ClusterID != "" && !seen[bs.Cluster.ClusterID] {
		out = append(out, bs.Cluster)
	}
	return out
}

// Apply seeds the store from the bootstrap. It is a no-op (returns false) if the
// store is already non-empty, so it never clobbers live runtime state on restart.
// The whole topology is seeded; use ApplyBootstrapForCluster to scope a kvindexer
// to a single cluster.
func (s *Store) ApplyBootstrap(bs *Bootstrap) bool {
	return s.ApplyBootstrapForCluster(bs, "")
}

// ApplyBootstrapForCluster seeds the store, optionally scoped to a single
// cluster. When clusterID != "", only that cluster, the engines whose
// cluster_id matches, the profiles served by those engines, and policies scoped
// to that cluster (or to a served model, or global) are seeded — so a per-cluster
// kvindexer only subscribes to its own engines. clusterID == "" seeds everything.
func (s *Store) ApplyBootstrapForCluster(bs *Bootstrap, clusterID string) bool {
	if s.Version() != 0 {
		return false
	}

	clusters := bs.AllClusters()

	// Seed clusters (filtered).
	for _, c := range clusters {
		if clusterID != "" && c.ClusterID != clusterID {
			continue
		}
		enabled := true
		if c.Enabled != nil {
			enabled = *c.Enabled
		}
		s.UpsertCluster(Cluster{
			ClusterID:   c.ClusterID,
			DisplayName: c.DisplayName,
			Region:      c.Region,
			Environment: c.Environment,
			Enabled:     enabled,
		})
	}

	// Seed engines (filtered by cluster) and remember which models they serve so
	// we only seed the relevant profiles when scoped.
	servedModels := map[string]bool{}
	for _, e := range bs.Engines {
		if clusterID != "" && e.ClusterID != clusterID {
			continue
		}
		for _, m := range e.ServedModels {
			servedModels[m] = true
		}
		s.UpsertEngine(Engine{
			EngineID:          e.EngineID,
			ClusterID:         e.ClusterID,
			Framework:         Framework(e.Framework),
			APIEndpoint:       e.APIEndpoint,
			TokenizerEndpoint: e.TokenizerEndpoint,
			KVEventEndpoint:   e.KVEventEndpoint,
			ReplayEndpoint:    e.ReplayEndpoint,
			Topic:             e.Topic,
			ServedModels:      e.ServedModels,
			DPRanks:           e.DPRanks,
			MaxNumSeqs:        e.MaxNumSeqs,
			MaxModelLen:       e.MaxModelLen,
			Healthy:           true,
			Enabled:           true,
		})
	}

	// Seed profiles. When scoped, only those served by the cluster's engines.
	for _, p := range bs.Profiles {
		if clusterID != "" && !servedModels[p.ModelID] {
			continue
		}
		s.UpsertModelProfile(ModelProfile{
			ModelID:            p.ModelID,
			Framework:          Framework(p.Framework),
			HashProfile:        p.HashProfile,
			BlockSize:          p.BlockSize,
			HashSeed:           p.HashSeed,
			TokenizerEndpoint:  p.TokenizerEndpoint,
			TokenizerMode:      TokenizerMode(p.TokenizerMode),
			ChatTemplateSHA256: p.ChatTemplateSHA256,
		})
	}

	// Seed policies. When scoped, keep policies that target this cluster, a model
	// the cluster serves, or are global (no cluster/model scope).
	for _, p := range bs.Policies {
		if clusterID != "" && !policyInScope(p, clusterID, servedModels) {
			continue
		}
		s.UpsertPolicy(Policy{
			RuleID:     p.RuleID,
			Name:       p.Name,
			Priority:   p.Priority,
			Conditions: p.Conditions,
			Action:     p.Action,
			Enabled:    p.Enabled,
		})
	}
	return true
}

// policyInScope reports whether a policy is relevant to a scoped cluster: it
// targets the cluster, targets a model the cluster serves, or is global.
func policyInScope(p BootstrapPolicy, clusterID string, servedModels map[string]bool) bool {
	for _, c := range p.Conditions {
		switch c.Field {
		case ConditionFieldClusterID:
			if conditionExcludesString(c, clusterID) {
				return false
			}
		case ConditionFieldModelID:
			if conditionExcludesServedModel(c, servedModels) {
				return false
			}
		}
	}
	return true
}

func conditionExcludesString(c RuleCondition, value string) bool {
	switch c.Op {
	case ConditionOpEq:
		return fmt.Sprint(c.Value) != value
	case ConditionOpIn:
		for _, v := range valueList(c.Value) {
			if fmt.Sprint(v) == value {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func conditionExcludesServedModel(c RuleCondition, servedModels map[string]bool) bool {
	switch c.Op {
	case ConditionOpEq:
		return !servedModels[fmt.Sprint(c.Value)]
	case ConditionOpIn:
		for _, v := range valueList(c.Value) {
			if servedModels[fmt.Sprint(v)] {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func valueList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []string:
		out := make([]any, 0, len(x))
		for _, s := range x {
			out = append(out, s)
		}
		return out
	default:
		return []any{x}
	}
}
