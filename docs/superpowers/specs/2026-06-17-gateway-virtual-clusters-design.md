# Gateway Virtual Clusters Design

## Purpose

Operators need to create and edit model profiles for gateway-local tokenization without registering a real kvindexer backend. Today model profiles inherit their visible cluster from the selected gateway backend. A local tokenizer profile still requires a real backend because the gateway stores tokenizer assets by `(cluster, model_id)` but proxies the model profile write to kvindexer.

This design adds gateway-local virtual indexers. A virtual indexer creates a cluster namespace owned by the gateway. It appears in the console like a normal cluster target, stores its own local tokenizer model profiles and policies, and does not require a reachable kvindexer URL.

## Goals

- Allow users to create a virtual cluster from the existing indexer management surface.
- Allow model profiles to be created directly into a virtual cluster.
- Store virtual cluster model profiles and tokenizer assets independently per cluster.
- Support gateway-local admission for virtual clusters when `tokenizer_mode=local`.
- Keep existing real kvindexer behavior unchanged.

## Non-Goals

- Virtual clusters do not ingest KV events.
- Virtual clusters do not support KV-cache residency queries.
- Virtual clusters do not proxy remote-tokenizer admission to an engine.
- This change does not replace real kvindexer backends for production KV indexing.

## Data Model

`Connection` gets a new `Kind` field:

- `backend`: current behavior. Requires URL, may carry a token, and proxies reads/writes to kvindexer.
- `virtual`: gateway-local behavior. Requires id and cluster, does not require URL or token, and is served from gateway-owned config storage.

The gateway store gains virtual config storage keyed by virtual connection id:

- clusters
- model profiles
- policies

Tokenizer assets remain keyed by `(cluster, model_id)`, so two virtual clusters can upload different tokenizer zips or chat templates for the same model id.

## API Behavior

`POST /admin/connections` accepts `kind` and optional virtual cluster metadata.

- Missing kind defaults to `backend` for backward compatibility.
- `backend` validation stays the same.
- `virtual` requires `id` and `cluster`; URL and token are ignored.
- `virtual` may include `display_name`, `region`, `environment`, and `labels`. These initialize or update the gateway-owned cluster record for that virtual connection.

`GET /clusters-health` includes virtual entries with a backend health record that is always healthy and tagged as virtual.

Fan-out GET routes merge real backend results with virtual store results:

- `/clusters`
- `/model-profiles`
- `/policies`
- `/config/versions`

Single-target writes route by selected backend id:

- Real backend id: proxy as today.
- Virtual backend id: write to gateway virtual store.

Virtual model profile writes require `tokenizer_mode=local`. The gateway rejects `remote` mode for a virtual target because there is no backend tokenizer endpoint or proxy target.

## Admission Behavior

When the selected target is virtual:

- The gateway resolves the profile from its virtual store.
- The profile must be local tokenizer mode.
- The gateway loads tokenizer assets from the gateway store and tokenizes via the local sidecar.
- The gateway evaluates token-only policies from the virtual store.
- `require_cache_hit` policies are ignored for virtual clusters because there is no KV residency index.
- Accepted responses do not include a real engine target unless a separate route-target feature is added.

`/query-prefix` returns a clear 400 for virtual clusters.

## Frontend Behavior

The Indexer Connections panel adds a type selector:

- Real indexer: id, cluster, URL, token, enabled.
- Virtual indexer: id, cluster, display label, enabled.

The Model Profiles sheet adds a target selector for creation. Existing rows still edit by their `_backend`.

For virtual targets:

- Tokenizer mode is locked to local.
- Tokenizer zip is required on first upload.
- The UI can show "Virtual" next to the target so the operator knows no real kvindexer is involved.

## Error Handling

- Creating a virtual connection without id or cluster returns 400.
- Creating a backend connection without URL still returns 400.
- Writing a remote tokenizer profile to a virtual target returns 400.
- Querying KV residency on a virtual target returns 400 with a message explaining that virtual clusters have no KV index.
- Ambiguous writes keep the existing 400 behavior.

## Testing

Backend tests:

- Virtual connections can be created without URL.
- Backend connections still require URL.
- `/clusters-health` returns virtual cluster records.
- Virtual `/model-profiles` writes persist locally and are tagged with `_cluster` and `_backend` on list.
- Virtual local tokenizer admission uses virtual profile and policies without proxying to kvindexer.
- Virtual `query-prefix` returns 400.

Frontend tests or focused component checks:

- Indexer form hides URL/token for virtual targets.
- Model profile creation can target a virtual backend.
- Virtual model profile creation locks tokenizer mode to local.
