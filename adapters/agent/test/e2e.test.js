import assert from "node:assert/strict";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import extension from "../pi-extension.js";

const url = process.env.ARTIFEX_E2E_URL;
assert.ok(url, "Run through ARTIFEX_ADAPTER_E2E=1 go test ./server -run TestAgentAdapterE2E -count=1 -v with a disposable database.");
const password = process.env.ARTIFEX_E2E_PASSWORD;
const findingID = process.env.ARTIFEX_E2E_FINDING_ID;

async function connect(t, overrides = {}) {
  const transport = new StdioClientTransport({
    command: process.execPath, args: [fileURLToPath(new URL("../src/mcp.js", import.meta.url))],
    env: { ...process.env, ARTIFEX_URL: url, ARTIFEX_TOKEN: "", ARTIFEX_PASSWORD: password,
      ARTIFEX_ALLOW_WRITES: "true", ...overrides }, stderr: "pipe",
  });
  const client = new Client({ name: "artifex-e2e", version: "1.0.0" });
  await client.connect(transport);
  t.after(() => client.close());
  return client;
}

async function call(client, name, args = {}) {
  const result = await client.callTool({ name: `artifex_${name}`, arguments: args });
  assert.ok(!result.isError, JSON.stringify(result.content));
  assert.deepEqual(JSON.parse(result.content[0].text), result.structuredContent);
  return result.structuredContent.data;
}

test("MCP stdio → real authenticated backend → persisted task, controls, coverage and findings", async (t) => {
  const client = await connect(t);
  const { tools } = await client.listTools();
  assert.equal(tools.length, 8);
  assert.equal(tools.find((tool) => tool.name === "artifex_create_task").annotations.readOnlyHint, false);
  const health = await call(client, "health");
  assert.ok(health);
  const created = await call(client, "create_task", {
    name: "Agent adapter E2E", description: "Local isolated fixture; do not contact targets.", goal: "Record a task for adapter verification.",
  });
  assert.match(created.id, /^[1-9][0-9]*$/);
  assert.equal(created.goal, "Record a task for adapter verification.");
  const task = await call(client, "get_task", { task_id: created.id });
  assert.equal(task.id, created.id);
  const listed = await call(client, "list_tasks", { limit: 100 });
  assert.ok(listed.tasks.some((task) => task.id === created.id));
  await call(client, "control_task", { task_id: created.id, action: "pause" });
  assert.equal((await call(client, "get_task", { task_id: created.id })).paused, true);
  await call(client, "control_task", { task_id: created.id, action: "resume" });
  assert.equal((await call(client, "get_task", { task_id: created.id })).paused, false);
  assert.ok(await call(client, "get_coverage", { task_id: created.id }));
  const findings = await call(client, "list_findings", { severity: "high", q: "adapter fixture" });
  assert.ok(findings.items.some((finding) => finding.id === findingID));
  const finding = await call(client, "get_finding", { finding_id: findingID });
  assert.equal(finding.id, findingID);
  assert.match(finding.evidence, /SIMULATED/);
  const empty = await call(client, "list_findings", { task_id: created.id });
  assert.equal(empty.total, 0);
  const page = await call(client, "list_tasks", { page: 100, limit: 1 });
  assert.equal(page.tasks.length, 0);
});

test("MCP surfaces authentication, missing tasks and invalid input as errors; read-only mode hides writes", async (t) => {
  const readonly = await connect(t, { ARTIFEX_ALLOW_WRITES: "false" });
  assert.equal((await readonly.listTools()).tools.length, 6);
  const disabled = await readonly.callTool({ name: "artifex_create_task", arguments: { description: "x", goal: "y" } });
  assert.equal(disabled.isError, true);
  assert.match(disabled.content[0].text, /not found/);
  const missing = await readonly.callTool({ name: "artifex_get_task", arguments: { task_id: "999999999" } });
  assert.equal(missing.isError, true);
  assert.match(missing.content[0].text, /HTTP 404/);
  const invalid = await readonly.callTool({ name: "artifex_get_task", arguments: { task_id: "../settings" } });
  assert.equal(invalid.isError, true);
  const invalidAuth = await connect(t, { ARTIFEX_TOKEN: "invalid-fixture-token", ARTIFEX_PASSWORD: "" });
  const denied = await invalidAuth.callTool({ name: "artifex_list_tasks", arguments: {} });
  assert.equal(denied.isError, true);
  assert.match(denied.content[0].text, /HTTP 401/);
  assert.ok(!denied.content[0].text.includes("invalid-fixture-token"));
});

test("Pi tools → same real backend → create and read task, retrieve evidence, reject bad IDs", async () => {
  const saved = { ...process.env };
  Object.assign(process.env, { ARTIFEX_URL: url, ARTIFEX_TOKEN: "", ARTIFEX_PASSWORD: password, ARTIFEX_ALLOW_WRITES: "true" });
  const tools = new Map();
  try { extension({ registerTool: (tool) => tools.set(tool.name, tool) }); }
  finally {
    for (const key of Object.keys(process.env)) if (!(key in saved)) delete process.env[key];
    Object.assign(process.env, saved);
  }
  const run = async (name, args) => (await tools.get(`artifex_${name}`).execute("e2e", args)).details.data;
  const created = await run("create_task", { description: "Pi local fixture", goal: "Verify Pi tool adapter" });
  assert.equal((await run("get_task", { task_id: created.id })).goal, "Verify Pi tool adapter");
  assert.equal((await run("get_finding", { finding_id: findingID })).id, findingID);
  await assert.rejects(run("get_task", { task_id: "../settings" }));
  await assert.rejects(run("get_task", { task_id: "999999999" }), /HTTP 404/);
});
