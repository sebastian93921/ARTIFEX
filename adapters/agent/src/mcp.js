#!/usr/bin/env node
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { ARTIFEXClient } from "./client.js";
import { createTools, executeTool } from "./tools.js";

try {
  const client = new ARTIFEXClient();
  const server = new McpServer({ name: "artifex-mcp-server", version: "0.1.0" });
  for (const tool of createTools(client)) {
    server.registerTool(tool.name, {
      title: tool.title, description: tool.description, inputSchema: tool.schema,
      outputSchema: tool.outputSchema, annotations: tool.annotations,
    }, async (args, extra) => {
      try { return await executeTool(tool, args, extra.signal); }
      catch (error) { return { isError: true, content: [{ type: "text", text: client.redact(error.message) }] }; }
    });
  }
  await server.connect(new StdioServerTransport());
} catch (error) {
  // stdout belongs exclusively to the MCP protocol. Configuration errors never echo secrets.
  console.error(error.message);
  process.exitCode = 1;
}
