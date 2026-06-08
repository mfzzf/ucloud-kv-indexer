# OpenAPI

The runtime services serve these same documents at `/openapi.json`.

Run:

```sh
make openapi
```

This regenerates:

- `api/kvindexer.openapi.json`
- `api/gateway.openapi.json`

The target also runs `oapi-codegen` in scratch output mode. That gives us a
strict parser/codegen check without committing generated server code before the
HTTP layer is migrated to spec-first routing.
