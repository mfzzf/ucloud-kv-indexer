# Kubernetes Helm charts

This directory contains two separate Helm charts:

- `gateway/` - deploys the central `kvgateway`.
- `indexer/` - deploys one or more regional/per-cluster `kvindexer` instances.
- `web/` - deploys the Next.js console.

MySQL is not deployed by these charts. The gateway connects to an external MySQL
through `KVGATEWAY_MYSQL_DSN`, stored in the gateway Secret.

## Layout

```text
kubernetes/
  gateway/
    Chart.yaml
    values.yaml
    templates/
  indexer/
    Chart.yaml
    values.yaml
    templates/
  web/
    Chart.yaml
    values.yaml
    templates/
```

## Gateway

Build and push the shared control-plane image first:

```sh
docker build -t uhub.service.ucloud.cn/uminfer/ucloud-kv-indexer:latest .
docker push uhub.service.ucloud.cn/uminfer/ucloud-kv-indexer:latest

docker build -f web/Dockerfile -t uhub.service.ucloud.cn/uminfer-proxy/ucloud-kv-indexer-web:latest web
docker push uhub.service.ucloud.cn/uminfer-proxy/ucloud-kv-indexer-web:latest
```

The Go image contains both `/usr/local/bin/kvgateway` and
`/usr/local/bin/kvindexer`; the gateway/indexer charts select the right command.
All charts render workload resources into the existing `ucloud-kv-indexers`
namespace and do not create the namespace.

Install the gateway chart in the central control-plane cluster:

```sh
helm upgrade --install ucloud-kv-gateway ./kubernetes/gateway \
  --namespace ucloud-kv-indexers \
  --set image.repository=uhub.service.ucloud.cn/uminfer/ucloud-kv-indexer \
  --set image.tag=latest
```

Important values:

- `secrets.mysqlDSN` - external MySQL DSN used by `kvgateway`.
- `secrets.backendToken` - shared bearer token that gateway sends to every indexer.
- `bootstrap.files` - topology files used only to seed gateway MySQL
  `connections` from `clusters[].backends` when the table is empty.
- `gateway.configFiles` - which bootstrap files the gateway reads.

The default gateway values include two region examples:

- `cn-shanghai-vllm`
- `cn-guangzhou-sglang`

Their `clusters[].backends` point at regional indexer Services. Replace these
with the real cross-region DNS names or load balancer URLs.

## Indexers

Install the indexer chart in a regional cluster, or use one values file to render
multiple indexers:

```sh
helm upgrade --install ucloud-kv-indexers ./kubernetes/indexer \
  --namespace ucloud-kv-indexers \
  --set image.repository=uhub.service.ucloud.cn/uminfer/ucloud-kv-indexer \
  --set image.tag=latest
```

`indexer/values.yaml` uses an `indexers[]` list. Each item renders its own
Deployment and Service:

```yaml
indexers:
  - name: kvindexer-vllm-cn-shanghai
    region: cn-shanghai
    clusterID: cn-shanghai-vllm
    bootstrapFile: cn-shanghai-vllm.yaml
    mongo:
      uri: mongodb://...
      db: kvindexer_cn_shanghai_vllm
```

For a single-region install, pass a region-specific values file with just that
region's `indexers[]` entry and matching `bootstrap.files`.

Examples are included under `indexer/regions/`:

```sh
helm upgrade --install kvindexer-cn-shanghai ./kubernetes/indexer \
  --namespace ucloud-kv-indexers \
  -f ./kubernetes/indexer/regions/cn-shanghai/values.yaml

helm upgrade --install kvindexer-cn-guangzhou ./kubernetes/indexer \
  --namespace ucloud-kv-indexers \
  -f ./kubernetes/indexer/regions/cn-guangzhou/values.yaml
```

The `secrets.backendToken` value must match the gateway chart's token. The
default values already use the same generated token in both charts.

## Web console

Install the web chart where it can reach the gateway Service:

```sh
helm upgrade --install ucloud-kv-web ./kubernetes/web \
  --namespace ucloud-kv-indexers \
  --set image.repository=uhub.service.ucloud.cn/uminfer-proxy/ucloud-kv-indexer-web \
  --set image.tag=latest \
  --set web.gatewayBaseURL=http://kvgateway.ucloud-kv-indexers.svc.cluster.local:8095
```

The browser talks to the web service only. The Next.js server proxies
same-origin `/api/kvi/*` to `web.gatewayBaseURL`, so gateway does not need to be
publicly exposed.

## Secret defaults

The default `values.yaml` files include generated strong random strings for:

- gateway -> indexer bearer token
- example external MySQL DSN password
- example per-region MongoDB URI passwords

Replace them before production if this repository is shared.

## Render and validate

```sh
helm lint ./kubernetes/gateway
helm lint ./kubernetes/indexer
helm lint ./kubernetes/web
helm template ucloud-kv-gateway ./kubernetes/gateway
helm template ucloud-kv-indexers ./kubernetes/indexer
helm template ucloud-kv-web ./kubernetes/web
```

## Runtime notes

- Gateway never listens to ZMQ. It only federates HTTP calls and owns the MySQL
  connection registry.
- Each indexer should be deployed near its serving engines so ZMQ stays local to
  that region/cluster.
- Gateway `clusters[].backends` should use a DNS/LB address reachable from the
  gateway cluster, not a region-local Kubernetes Service DNS unless gateway and
  indexer are in the same cluster.
- Policies are intentionally omitted from bootstrap. Create and edit them via
  the frontend/API so they persist in each indexer's MongoDB config store.
