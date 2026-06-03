  cd ucloud-kv-indexer
  make inference
  make build
  make up
  run/bin/kvindexer -addr 127.0.0.1:8090 -store mongo -mongo-uri mongodb://127.0.0.1:27017 -mongo-db kvindexer_local_vllm -bootstrap deploy/local-vllm.yaml -cluster local-vllm -auth-token dev-local-token
  run/bin/kvindexer -addr 127.0.0.1:8091 -store mongo -mongo-uri mongodb://127.0.0.1:27017 -mongo-db kvindexer_local_sglang -bootstrap deploy/local-sglang.yaml -cluster local-sglang -auth-token dev-local-token
  run/bin/kvgateway -addr 127.0.0.1:8095 -sqlite-path run/gateway-connections.db -configs deploy/local-vllm.yaml,deploy/local-sglang.yaml -backend-token dev-local-token
  npm --prefix web run start -- -H 127.0.0.1 -p 3000
