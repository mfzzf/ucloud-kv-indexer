import assert from "node:assert/strict";
import test from "node:test";

import { api } from "./api.ts";

test("api client reports plain-text HTTP errors without JSON parse failures", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () =>
    new Response("404 page not found", {
      status: 404,
      headers: { "content-type": "text/plain" },
    });

  try {
    await assert.rejects(
      () => api.del("/model-profiles/glm-5.1?backend=th-gb200"),
      (err) => {
        assert(err instanceof Error);
        assert.equal(err.message, "HTTP 404: 404 page not found");
        return true;
      },
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});
