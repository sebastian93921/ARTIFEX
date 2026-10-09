import assert from "node:assert/strict";
import { createServer } from "node:http";
import { once } from "node:events";
import test from "node:test";
import { configFromEnv, ARTIFEXClient } from "../src/client.js";
import { createTools, executeTool } from "../src/tools.js";
import extension from "../pi-extension.js";

async function fixture(t, handler) {
  const server = createServer(handler);
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  t.after(() => { server.closeAllConnections(); server.close(); });
  return `http://127.0.0.1:${server.address().port}`;
}

test("configuration rejects unsafe origins and invalid options without printing credentials", () => {
  for (const url of ["http://example.com", "https://secret@example.com", "https://example.com/api/", "https://example.com?token=secret", "file:///tmp/x"]) {
    assert.throws(() => configFromEnv({ ARTIFEX_URL: url }), (error) => !error.message.includes("secret"));
  }
  for (const options of [{ ARTIFEX_TIMEOUT_MS: "NaN" }, { ARTIFEX_TIMEOUT_MS: "0" },
    { ARTIFEX_LANGUAGE: "xx" }, { ARTIFEX_ALLOW_WRITES: "yes" }, { ARTIFEX_TOKEN: "x\ny" }]) {
    assert.throws(() => configFromEnv(options));
  }
});

test("writes are absent by default and unknown/path-injection arguments are rejected before HTTP", async () => {
  const client = new ARTIFEXClient(configFromEnv({}));
  const reads = createTools(client);
  assert.equal(reads.length, 6);
  assert.ok(reads.every((tool) => tool.annotations.readOnlyHint));
  const tool = reads.find((tool) => tool.name === "artifex_get_task");
  await assert.rejects(executeTool(tool, { task_id: "../../settings" }));
  await assert.rejects(executeTool(tool, { task_id: "1", command: "ignored" }));
  const writes = createTools(new ARTIFEXClient(configFromEnv({ ARTIFEX_ALLOW_WRITES: "true" })));
  assert.equal(writes.length, 8);
  const create = writes.find((tool) => tool.name === "artifex_create_task");
  assert.equal(create.schema.parse({ description: "lab", goal: "test" }).timeout_seconds, 600);
  await assert.rejects(executeTool(create, { description: " ", goal: "test" }));
  await assert.rejects(executeTool(create, { description: "lab", goal: "test", timeout_seconds: 0 }));
});

test("concurrent calls share login, send authorization and Korean locale, and paginate tasks", async (t) => {
  let logins = 0;
  const url = await fixture(t, async (req, res) => {
    res.setHeader("Content-Type", "application/json");
    if (req.url === "/api/auth/login") {
      logins++;
      let body = "";
      for await (const chunk of req) body += chunk;
      assert.deepEqual(JSON.parse(body), { username: "ARTIFEX", password: "fixture-password" });
      return res.end(JSON.stringify({ token: "fixture-token" }));
    }
    assert.equal(req.headers.authorization, "Bearer fixture-token");
    assert.equal(req.headers["accept-language"], "ko");
    res.end(JSON.stringify({ tasks: [{ id: "3" }, { id: "2" }, { id: "1" }], active: "3" }));
  });
  const client = new ARTIFEXClient(configFromEnv({ ARTIFEX_URL: url, ARTIFEX_PASSWORD: "fixture-password", ARTIFEX_LANGUAGE: "ko" }));
  const tool = createTools(client).find((tool) => tool.name === "artifex_list_tasks");
  const results = await Promise.all([executeTool(tool, { page: 2, limit: 1 }), executeTool(tool, {})]);
  assert.equal(logins, 1);
  assert.deepEqual(results[0].structuredContent.data.tasks, [{ id: "2" }]);
  assert.equal(results[0].structuredContent.data.total, 3);
});

test("backend errors redact credentials and writes are never retried", async (t) => {
  let requests = 0;
  const url = await fixture(t, (req, res) => {
    requests++;
    res.writeHead(401, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "fixture-token fixture-password" }));
  });
  const client = new ARTIFEXClient(configFromEnv({ ARTIFEX_URL: url, ARTIFEX_TOKEN: "fixture-token", ARTIFEX_PASSWORD: "fixture-password" }));
  await assert.rejects(client.request("/api/tasks", { method: "POST", body: {} }), (error) => {
    assert.match(error.message, /HTTP 401.*redacted.*Sign in again/);
    assert.ok(!error.message.includes("fixture-token"));
    assert.ok(!error.message.includes("fixture-password"));
    return true;
  });
  assert.equal(requests, 1);
});

test("redirects do not forward credentials to another server", async (t) => {
  let leaked = false;
  const destination = await fixture(t, (_, res) => { leaked = true; res.end("{}"); });
  const url = await fixture(t, (_, res) => { res.writeHead(302, { Location: destination }); res.end(); });
  const client = new ARTIFEXClient(configFromEnv({ ARTIFEX_URL: url, ARTIFEX_TOKEN: "fixture-token" }));
  await assert.rejects(client.request("/api/tasks"));
  assert.equal(leaked, false);
});

test("timeouts, cancellation, malformed JSON and excessive responses fail explicitly", async (t) => {
  const url = await fixture(t, (req, res) => {
    if (req.url === "/api/invalid") return res.end("html");
    if (req.url === "/api/large") return res.end("x".repeat(2 * 1024 * 1024 + 1));
    // /api/hang intentionally leaves the response open.
  });
  const client = new ARTIFEXClient(configFromEnv({ ARTIFEX_URL: url, ARTIFEX_TOKEN: "fixture-token", ARTIFEX_TIMEOUT_MS: "100" }));
  await assert.rejects(client.request("/api/invalid"), /non-JSON/);
  await assert.rejects(client.request("/api/large"), /exceeds 2 MiB/);
  await assert.rejects(client.request("/api/hang"), /timed out/);
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(client.request("/api/hang", { signal: controller.signal }), /cancelled/);
});

test("Pi extension shares schemas and reports backend failures by throwing", async () => {
  const tools = [];
  extension({ registerTool: (tool) => tools.push(tool) });
  assert.equal(tools.length, process.env.ARTIFEX_ALLOW_WRITES === "true" ? 8 : 6);
  const task = tools.find((tool) => tool.name === "artifex_get_task");
  assert.equal(task.parameters.type, "object");
  await assert.rejects(task.execute("id", { task_id: "../settings" }), /Invalid/);
});
