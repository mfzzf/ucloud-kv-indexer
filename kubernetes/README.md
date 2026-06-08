# Kubernetes Helm charts

This directory contains separate Helm charts:

- `gateway/` - deploys the central `kvgateway`.
- `indexer/` - deploys one or more regional/per-cluster `kvindexer` instances.
- `mongodb/` - deploys one regional MongoDB StatefulSet backed by local NVMe.
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
  mongodb/
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
make image

docker build -f web/Dockerfile -t uhub.service.ucloud.cn/uminfer-proxy/ucloud-kv-indexer-web:latest web
docker push uhub.service.ucloud.cn/uminfer-proxy/ucloud-kv-indexer-web:latest
```

The Go image contains both `/usr/local/bin/kvgateway` and
`/usr/local/bin/kvindexer`; the gateway/indexer charts select the right command.
The gateway, indexer, and web charts render workload resources into the existing
`ucloud-kv-indexers` namespace and do not create the namespace. The MongoDB
chart is installed separately into the `mongodb` namespace.

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

The default indexer values include one `th-gb200` SGLang DeepSeek-V4-Pro
example:

- `th-gb200`

Its `clusters[].backends` points at the indexer Service. Replace it
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
  - name: kvindexer-th-gb200
    region: th
    clusterID: th-gb200
    store: mongo
    bootstrapFile: th-gb200.yaml
    mongo:
      uri: mongodb://...
      db: kvindexer_th_gb200
```

For a single-region install, pass a region-specific values file with just that
region's `indexers[]` entry and matching `bootstrap.files`.

Examples are included under `indexer/regions/`:

```sh
helm upgrade --install kvindexer-th-gb200 ./kubernetes/indexer \
  --namespace ucloud-kv-indexers \
  -f ./kubernetes/indexer/regions/th-gb200/values.yaml
```

The `secrets.backendToken` value must match the gateway chart's token. The
default values already use the same generated token in both charts.

## Regional MongoDB

MongoDB is deployed separately per region. The chart does not create the
namespace; create it and the root Secret first:

```sh
kubectl create ns mongodb

export MONGO_ROOT_PASSWORD='change-me'
kubectl -n mongodb create secret generic mongo-root \
  --from-literal=username=admin \
  --from-literal=password="$MONGO_ROOT_PASSWORD"
```

Prepare the local NVMe directory on the target node:

```sh
mkdir -p /mnt/nvme1n1/mongodb
chown -R 999:999 /mnt/nvme1n1/mongodb
```

Find the Kubernetes node name for the local PV:

```sh
kubectl get nodes -o wide | awk '/10\.255\.240\.113/{print $1; exit}'
```

Set that name in `mongodb/regions/<region>/values.yaml`, then install:

```sh
helm upgrade --install mongodb-th-gb200 ./kubernetes/mongodb \
  --namespace mongodb \
  -f ./kubernetes/mongodb/regions/th-gb200/values.yaml
```

Use the same root credentials in the region indexer URI:

```sh
helm upgrade --install kvindexer-th-gb200 ./kubernetes/indexer \
  --namespace ucloud-kv-indexers \
  -f ./kubernetes/indexer/regions/th-gb200/values.yaml \
  --set-string 'indexers[0].mongo.uri=mongodb://admin:'"$MONGO_ROOT_PASSWORD"'@mongodb.mongodb.svc.cluster.local:27017/?authSource=admin'
```

If you prefer Helm to create the MongoDB Secret, pass:

```sh
--set auth.createSecret=true --set-string auth.password="$MONGO_ROOT_PASSWORD"
```

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
helm lint ./kubernetes/mongodb
helm lint ./kubernetes/web
helm template ucloud-kv-gateway ./kubernetes/gateway
helm template ucloud-kv-indexers ./kubernetes/indexer
helm template mongodb-th-gb200 ./kubernetes/mongodb --namespace mongodb -f ./kubernetes/mongodb/regions/th-gb200/values.yaml
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
