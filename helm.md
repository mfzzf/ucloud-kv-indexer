helm upgrade --install kvindexer-th-gb200 ./kubernetes/indexer \
  -n ucloud-kv-indexers --create-namespace \
  -f kubernetes/indexer/regions/th-gb200/values.yaml


kubectl rollout restart deploy/kvindexer-th-gb200 -n ucloud-kv-indexers


helm upgrade --install ucloud-kv-gateway ./kubernetes/gateway \
  -n ucloud-kv-indexers --create-namespace \
  -f kubernetes/gateway/values.yaml \
  --set localTokenizer.enabled=true




helm upgrade --install ucloud-kv-web ./kubernetes/web \
  -n ucloud-kv-indexers --create-namespace \
  -f kubernetes/web/values.yaml

kubectl rollout restart deploy/ucloud-kv-web -n ucloud-kv-indexers

kubectl rollout restart deploy/kvgateway -n ucloud-kv-indexers