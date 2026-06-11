package openapi

const version = "0.2.0"

// KVIndexerSpec returns the OpenAPI document served by a single kvindexer
// backend. It intentionally describes the stable HTTP surface, not internal Go
// handler names.
func KVIndexerSpec() map[string]any {
	return spec("ucloud-kv-indexer API", false)
}

// GatewaySpec returns the OpenAPI document served by kvgateway. It includes the
// same backend API plus gateway-native federation/admin endpoints.
func GatewaySpec() map[string]any {
	return spec("ucloud-kv-indexer Gateway API", true)
}

func spec(title string, gateway bool) map[string]any {
	selectorParams := []any{}
	if gateway {
		selectorParams = []any{ref("#/components/parameters/Cluster"), ref("#/components/parameters/Backend")}
	}

	paths := map[string]any{
		"/healthz": map[string]any{
			"get": op([]string{"System"}, "Health check", "Liveness endpoint. It is not authenticated on kvindexer.", nil, nil),
		},
		"/openapi.json": map[string]any{
			"get": op([]string{"System"}, "OpenAPI document", "Returns this OpenAPI 3 document.", nil, nil),
		},

		"/route": map[string]any{
			"post": op([]string{"Admission"}, "Judge OpenAI chat admission", "Alias of /v1/chat/completions for admission judgment.", jsonBody("OpenAI-compatible chat request", chatSchema()), selectorParams),
		},
		"/v1/chat/completions": map[string]any{
			"post": op([]string{"Admission"}, "Judge OpenAI chat admission", "Normalizes and tokenizes an OpenAI chat request, queries prefix residency, and applies policy. It judges; it does not proxy generation.", jsonBody("OpenAI-compatible chat request", chatSchema()), selectorParams),
		},
		"/v1/responses": map[string]any{
			"post": op([]string{"Admission"}, "Judge OpenAI responses admission", "Admission judgment for OpenAI Responses-compatible requests.", jsonBody("OpenAI Responses-compatible request", responsesSchema()), selectorParams),
		},
		"/v1/messages": map[string]any{
			"post": op([]string{"Admission"}, "Judge Anthropic messages admission", "Admission judgment for Anthropic Messages-compatible requests.", jsonBody("Anthropic Messages-compatible request", anthropicSchema()), selectorParams),
		},
		"/query-prefix": map[string]any{
			"post": op([]string{"Prefix"}, "Query prefix cache hits", "Mooncake/Dynamo-style per-instance prefix hit query. Accepts model plus token_ids, or model plus prompt.", jsonBody("Prefix query request", queryPrefixSchema()), selectorParams),
		},
		"/tokenize/preview": map[string]any{
			"post": op([]string{"Prefix"}, "Preview tokenization", "Shows normalized tokens and request_keys for a protocol request.", jsonBody("Tokenization preview request", tokenizePreviewSchema()), selectorParams),
		},
		"/config/effective-policy/preview": map[string]any{
			"post": op([]string{"Policies"}, "Preview admission rules", "Evaluates the priority-ordered rule list for a request shape. Rule conditions are AND clauses; rules are OR by priority.", jsonBody("Rule preview request", effectivePolicyPreviewSchema()), selectorParams),
		},

		"/clusters": map[string]any{
			"get":  op([]string{"Config"}, "List clusters", "Lists configured clusters.", nil, selectorParams),
			"post": op([]string{"Config"}, "Create or update cluster", "Creates or replaces a cluster config.", jsonBody("Cluster", clusterSchema()), selectorParams),
		},
		"/clusters/{id}": map[string]any{
			"patch": op([]string{"Config"}, "Patch cluster", "Hot-updates mutable cluster fields.", jsonBody("Cluster patch", clusterPatchSchema()), append([]any{pathID()}, selectorParams...)),
		},
		"/engines": map[string]any{
			"get": op([]string{"Config"}, "List engines", "Lists registered inference engines.", nil, selectorParams),
		},
		"/engines/register": map[string]any{
			"post": op([]string{"Config"}, "Register engine", "Registers or replaces an inference engine and starts its KV-event listener if configured.", jsonBody("Engine", engineSchema()), selectorParams),
		},
		"/engines/unregister": map[string]any{
			"post": op([]string{"Config"}, "Unregister engine", "Removes an engine by engine_id.", jsonBody("Engine unregister request", objectSchema([]string{"engine_id"}, field("engine_id", "string", "Engine identifier."))), selectorParams),
		},
		"/engines/{id}": map[string]any{
			"patch": op([]string{"Config"}, "Patch engine", "Hot-updates mutable engine state such as enabled, draining, health, and queue depth.", jsonBody("Engine patch", enginePatchSchema()), append([]any{pathID()}, selectorParams...)),
		},
		"/model-profiles": map[string]any{
			"get":  op([]string{"Config"}, "List model profiles", "Lists tokenization/hash profiles.", nil, selectorParams),
			"post": op([]string{"Config"}, "Create or update model profile", "Creates a new profile version when hash-affecting fields change.", jsonBody("Model profile", modelProfileSchema()), selectorParams),
		},
		"/policies": map[string]any{
			"get":  op([]string{"Policies"}, "List policy rules", "Lists admission policy rules.", nil, selectorParams),
			"post": op([]string{"Policies"}, "Create or update policy rule", "Creates or replaces a policy rule.", jsonBody("Policy", policySchema()), selectorParams),
		},
		"/policies/{id}": map[string]any{
			"patch":  op([]string{"Policies"}, "Patch policy rule", "Updates selected fields on a policy rule.", jsonBody("Policy patch", policySchema()), append([]any{pathID()}, selectorParams...)),
			"delete": op([]string{"Policies"}, "Delete policy rule", "Deletes a policy rule.", nil, append([]any{pathID()}, selectorParams...)),
		},

		"/event-streams": map[string]any{
			"get": op([]string{"Observability"}, "List KV-event listener health", "Per-engine ZMQ listener connection, sequence, gap, decode, and queue health.", nil, selectorParams),
		},
		"/kv-events/recent": map[string]any{
			"get": op([]string{"Observability"}, "Recent decoded KV events", "Returns recent decoded ZMQ KV-cache events. Query parameter limit defaults to 100.", nil, append([]any{ref("#/components/parameters/Limit")}, selectorParams...)),
		},
		"/kv-events/stream": map[string]any{
			"get": op([]string{"Observability"}, "Live decoded KV-event stream", "Server-Sent Events stream of decoded KV-cache events. Select exactly one backend or cluster through the gateway.", nil, selectorParams),
		},
		"/decisions": map[string]any{
			"get": op([]string{"Observability"}, "Recent admission decisions", "Returns recent admission verdicts with reason, hit ratio, target, and config version.", nil, selectorParams),
		},
		"/config/audit-log": map[string]any{
			"get": op([]string{"Observability"}, "Config audit log", "Lists configuration mutations and profile version bumps.", nil, selectorParams),
		},
		"/config/versions": map[string]any{
			"get": op([]string{"Observability"}, "Config versions", "Returns the current config version. On gateway, returns one item per backend.", nil, selectorParams),
		},
		"/index/stats": map[string]any{
			"get": op([]string{"Observability"}, "Residency index stats", "Lists request_key, bridge, engine, and last-event counts per namespace.", nil, selectorParams),
		},
	}

	if gateway {
		paths["/clusters-health"] = map[string]any{
			"get": op([]string{"Gateway"}, "Cluster backend health", "Groups configured backend connections by cluster and live-probes each backend.", nil, nil),
		}
		paths["/admin/connections"] = map[string]any{
			"get":  op([]string{"Gateway"}, "List backend connections", "Lists gateway backend connections. Tokens are redacted.", nil, nil),
			"post": op([]string{"Gateway"}, "Create or update backend connection", "Creates or updates a kvindexer backend connection.", jsonBody("Gateway connection", connectionSchema()), nil),
		}
		paths["/admin/connections/{id}"] = map[string]any{
			"delete": op([]string{"Gateway"}, "Delete backend connection", "Deletes a gateway backend connection.", nil, []any{pathID()}),
		}
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       title,
			"version":     version,
			"description": "Admission control, prefix-cache residency, KV-event observability, and configuration API.",
		},
		"servers": []any{
			map[string]any{"url": "/"},
		},
		"tags": []any{
			map[string]any{"name": "Admission"},
			map[string]any{"name": "Prefix"},
			map[string]any{"name": "Config"},
			map[string]any{"name": "Policies"},
			map[string]any{"name": "Observability"},
			map[string]any{"name": "Gateway"},
			map[string]any{"name": "System"},
		},
		"paths":      paths,
		"components": components(),
	}
}

func op(tags []string, summary, description string, requestBody map[string]any, params []any) map[string]any {
	out := map[string]any{
		"tags":        tags,
		"summary":     summary,
		"description": description,
		"responses": map[string]any{
			"200": map[string]any{
				"description": "OK",
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{},
					},
				},
			},
			"400": ref("#/components/responses/Error"),
			"401": ref("#/components/responses/Error"),
			"404": ref("#/components/responses/Error"),
			"429": ref("#/components/responses/Error"),
		},
	}
	if requestBody != nil {
		out["requestBody"] = requestBody
	}
	if len(params) > 0 {
		out["parameters"] = params
	}
	return out
}

func jsonBody(description string, schema map[string]any) map[string]any {
	return map[string]any{
		"description": description,
		"required":    true,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": schema,
			},
		},
	}
}

func objectSchema(required []string, fields ...map[string]any) map[string]any {
	props := map[string]any{}
	for _, f := range fields {
		name, _ := f["name"].(string)
		delete(f, "name")
		props[name] = f
	}
	out := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func field(name, typ, desc string) map[string]any {
	return map[string]any{"name": name, "type": typ, "description": desc}
}

func arrayField(name, desc string, items map[string]any) map[string]any {
	return map[string]any{"name": name, "type": "array", "description": desc, "items": items}
}

func objectField(name, desc string) map[string]any {
	return map[string]any{"name": name, "type": "object", "description": desc, "additionalProperties": true}
}

func chatSchema() map[string]any {
	return objectSchema([]string{"model", "messages"},
		field("model", "string", "Model name, for example qwen3.5-4b."),
		arrayField("messages", "OpenAI chat messages.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"role":    map[string]any{"type": "string"},
				"content": map[string]any{},
			},
		}),
		field("tenant_id", "string", "Optional business tenant/customer/workspace id used for policy matching."),
		arrayField("tools", "Optional OpenAI tool definitions.", map[string]any{"type": "object", "additionalProperties": true}),
		field("max_tokens", "integer", "Optional generation limit; accepted for compatibility."),
		field("temperature", "number", "Optional sampling temperature; accepted for compatibility."),
	)
}

func responsesSchema() map[string]any {
	return objectSchema([]string{"model", "input"},
		field("model", "string", "Model name."),
		field("input", "string", "Responses API input. Object/array inputs are accepted by compatible clients but omitted from this compact schema."),
		field("tenant_id", "string", "Optional business tenant/customer/workspace id used for policy matching."),
	)
}

func anthropicSchema() map[string]any {
	return objectSchema([]string{"model", "messages"},
		field("model", "string", "Model name."),
		arrayField("messages", "Anthropic messages.", map[string]any{"type": "object", "additionalProperties": true}),
		field("system", "string", "Optional system prompt."),
		field("tenant_id", "string", "Optional business tenant/customer/workspace id used for policy matching."),
		field("max_tokens", "integer", "Anthropic max_tokens."),
	)
}

func queryPrefixSchema() map[string]any {
	return objectSchema([]string{"model"},
		field("model", "string", "Model name used to resolve profile/namespace."),
		field("tenant_id", "string", "Optional tenant id."),
		arrayField("token_ids", "Token ids to query. If omitted, prompt is tokenized by the engine.", map[string]any{"type": "integer", "format": "int32"}),
		field("prompt", "string", "Plain prompt to tokenize when token_ids are not provided."),
		field("block_size", "integer", "Optional override for profile block size."),
	)
}

func tokenizePreviewSchema() map[string]any {
	return objectSchema([]string{"model", "protocol", "raw"},
		field("model", "string", "Model name."),
		field("protocol", "string", "Protocol id: openai.chat, openai.responses, or anthropic.messages."),
		objectField("raw", "Original protocol request body."),
	)
}

func effectivePolicyPreviewSchema() map[string]any {
	return objectSchema(nil,
		field("cluster_id", "string", "Cluster scope to preview."),
		field("model_id", "string", "Model scope to preview."),
		field("tenant_id", "string", "Tenant/customer/workspace scope to preview."),
		field("input_tokens", "integer", "Input token count to test against token-count conditions."),
		field("hit_ratio", "number", "Synthetic KV hit ratio for previewing require_cache_hit actions."),
		field("fresh", "boolean", "Whether KV event signals are trusted for this preview. Defaults true."),
		field("tokenized", "boolean", "Whether tokenization succeeded for this preview. Defaults true."),
		field("hash_supported", "boolean", "Whether the request features are hash-supported. Defaults true."),
		field("has_candidates", "boolean", "Whether at least one engine can serve the request. Defaults true."),
	)
}

func clusterSchema() map[string]any {
	return objectSchema([]string{"cluster_id"},
		field("cluster_id", "string", "Cluster id."),
		field("display_name", "string", "Human-readable name."),
		field("region", "string", "Region label."),
		field("environment", "string", "Environment label."),
		field("enabled", "boolean", "Whether the cluster is enabled."),
		field("maintenance_mode", "boolean", "Whether the cluster is in maintenance mode."),
		objectField("labels", "Free-form labels."),
	)
}

func clusterPatchSchema() map[string]any {
	return objectSchema(nil,
		field("display_name", "string", "Human-readable name."),
		field("enabled", "boolean", "Whether the cluster is enabled."),
		field("maintenance_mode", "boolean", "Whether the cluster is in maintenance mode."),
		objectField("labels", "Free-form labels."),
	)
}

func engineSchema() map[string]any {
	return objectSchema([]string{"engine_id", "cluster_id", "framework", "served_models"},
		field("engine_id", "string", "Engine id."),
		field("cluster_id", "string", "Owning cluster id."),
		field("framework", "string", "Serving framework, for example vllm or sglang."),
		field("api_endpoint", "string", "OpenAI-compatible serving endpoint."),
		field("tokenizer_endpoint", "string", "Tokenizer endpoint. Defaults to api_endpoint when omitted."),
		field("kv_event_endpoint", "string", "ZMQ KV-event endpoint for prefix-cache awareness."),
		field("replay_endpoint", "string", "Optional KV replay endpoint."),
		field("topic", "string", "ZMQ topic, default kv-events."),
		arrayField("served_models", "Model ids served by this engine.", map[string]any{"type": "string"}),
		field("dp_ranks", "integer", "Number of data-parallel ranks."),
		field("max_num_seqs", "integer", "Max active sequences."),
		field("max_model_len", "integer", "Model context length."),
		field("enabled", "boolean", "Whether this engine is eligible."),
		field("draining", "boolean", "Whether this engine should stop receiving new work."),
		field("healthy", "boolean", "Manual health flag."),
		objectField("labels", "Free-form labels."),
	)
}

func enginePatchSchema() map[string]any {
	return objectSchema(nil,
		field("healthy", "boolean", "Manual health flag."),
		field("draining", "boolean", "Whether this engine should stop receiving new work."),
		field("enabled", "boolean", "Whether this engine is eligible."),
		field("queue_depth", "integer", "Current serving queue depth."),
		field("max_num_seqs", "integer", "Max active sequences."),
		objectField("labels", "Free-form labels."),
	)
}

func modelProfileSchema() map[string]any {
	return objectSchema([]string{"model_id", "framework", "hash_profile", "block_size"},
		field("model_id", "string", "Model id."),
		arrayField("aliases", "Optional aliases.", map[string]any{"type": "string"}),
		field("framework", "string", "Serving framework."),
		field("version", "integer", "Profile version."),
		field("tokenizer_endpoint", "string", "Optional tokenizer endpoint override."),
		field("tokenizer_mode", "string", "Tokenizer source: remote (engine endpoint) or local (gateway sidecar)."),
		field("chat_template", "string", "Optional chat_template override when registering a local tokenizer."),
		field("chat_template_sha256", "string", "SHA-256 of the active local chat_template."),
		field("hash_profile", "string", "Hash semantics profile."),
		field("block_size", "integer", "Prefix block size in tokens."),
		field("hash_seed", "string", "Hash seed for namespace isolation."),
		field("supports_lora", "boolean", "Whether LoRA requests are hash-supported."),
		field("supports_multimodal", "boolean", "Whether multimodal requests are hash-supported."),
		field("supports_cache_salt", "boolean", "Whether cache salt is hash-supported."),
	)
}

func policySchema() map[string]any {
	return objectSchema([]string{"rule_id", "action"},
		field("rule_id", "string", "Stable admission rule id."),
		field("name", "string", "Human-readable rule name."),
		field("priority", "integer", "Higher priority rules are evaluated first."),
		arrayField("conditions", "AND conditions. Empty means match every request.", ruleConditionSchema()),
		map[string]any{"name": "action", "description": "Action to execute when all conditions match.", "allOf": []any{ruleActionSchema()}},
		field("enabled", "boolean", "Whether this rule is enabled. Omitted means enabled."),
	)
}

func ruleConditionSchema() map[string]any {
	return objectSchema([]string{"field", "op"},
		map[string]any{"name": "field", "type": "string", "description": "Request/observability field to compare.", "enum": []string{
			"cluster_id", "model_id", "tenant_id", "input_tokens", "hit_ratio",
			"best_hit_tokens", "effective_cached_tokens", "kv_event_state",
			"tokenized", "hash_supported", "has_candidates",
		}},
		map[string]any{"name": "op", "type": "string", "description": "Comparison operator.", "enum": []string{"eq", "neq", "in", "not_in", "gt", "gte", "lt", "lte", "contains"}},
		map[string]any{"name": "value", "description": "Comparison value. For in/not_in, pass an array."},
	)
}

func ruleActionSchema() map[string]any {
	return objectSchema([]string{"type"},
		map[string]any{"name": "type", "type": "string", "description": "Action type.", "enum": []string{"accept", "reject", "require_cache_hit"}},
		field("min_hit_ratio", "number", "Required KV hit ratio when type=require_cache_hit. 0.5 means 50%."),
		map[string]any{"name": "on_low_hit", "type": "string", "description": "Outcome when cache hit ratio is below min_hit_ratio.", "enum": []string{"accept", "reject", "fallback_accept"}},
		map[string]any{"name": "on_uncertain", "type": "string", "description": "Outcome when tokenization/hash/events/candidate signal is unavailable.", "enum": []string{"accept", "reject", "fallback_accept"}},
		field("reject_status", "integer", "HTTP status used for reject outcomes. Defaults to 429."),
	)
}

func connectionSchema() map[string]any {
	return objectSchema([]string{"id", "cluster", "url"},
		field("id", "string", "Gateway backend id."),
		field("cluster", "string", "Cluster this backend serves."),
		field("url", "string", "Base URL of the kvindexer backend."),
		field("token", "string", "Optional bearer token used by the gateway when calling this backend."),
		field("enabled", "boolean", "Whether this connection is enabled."),
	)
}

func pathID() map[string]any {
	return map[string]any{
		"name":        "id",
		"in":          "path",
		"required":    true,
		"description": "Resource identifier.",
		"schema":      map[string]any{"type": "string"},
	}
}

func ref(path string) map[string]any {
	return map[string]any{"$ref": path}
}

func components() map[string]any {
	return map[string]any{
		"parameters": map[string]any{
			"Cluster": map[string]any{
				"name":        "cluster",
				"in":          "query",
				"required":    false,
				"description": "Gateway selector. Use a concrete cluster for writes, admission, queries, and SSE; omit or use all for fan-out GET endpoints.",
				"schema":      map[string]any{"type": "string"},
			},
			"Backend": map[string]any{
				"name":        "backend",
				"in":          "query",
				"required":    false,
				"description": "Gateway selector for one exact backend id.",
				"schema":      map[string]any{"type": "string"},
			},
			"Limit": map[string]any{
				"name":        "limit",
				"in":          "query",
				"required":    false,
				"description": "Maximum number of items to return.",
				"schema":      map[string]any{"type": "integer", "minimum": 1, "default": 100},
			},
		},
		"responses": map[string]any{
			"Error": map[string]any{
				"description": "Error response",
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"error": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"message": map[string]any{"type": "string"},
										"type":    map[string]any{"type": "string"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
