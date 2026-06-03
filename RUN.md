  cd ucloud-kv-indexer
  make inference
  make build
  make up
  run/bin/kvindexer -addr 127.0.0.1:8090 -store memory -bootstrap deploy/config.local.yaml -cluster local-vllm -auth-token dev-local-token
  run/bin/kvindexer -addr 127.0.0.1:8091 -store memory -bootstrap deploy/config.local.yaml -cluster local-sglang -auth-token dev-local-token
  run/bin/kvgateway -addr 127.0.0.1:8095 -sqlite-path run/gateway-connections.db -config deploy/config.local.yaml -backend-token dev-local-token
  npm --prefix web run start -- -H 127.0.0.1 -p 3000