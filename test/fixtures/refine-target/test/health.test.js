import { describe, it, after } from "node:test";
import assert from "node:assert/strict";
import server from "../src/index.js";

describe("GET /health", () => {
  after(() => server.close());

  it("returns 200 with status ok", async () => {
    const res = await fetch("http://localhost:3999/health");
    assert.equal(res.status, 200);
    const body = await res.json();
    assert.equal(body.status, "ok");
    assert.equal(typeof body.uptime, "number");
  });
});
