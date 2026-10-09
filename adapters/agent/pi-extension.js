import { z } from "zod";
import { ARTEXClient } from "./src/client.js";
import { createTools, executeTool } from "./src/tools.js";

// Uses Pi's public registerTool contract without depending on a particular Pi package namespace.
export default function artexExtension(pi) {
  const client = new ARTEXClient();
  for (const tool of createTools(client)) {
    pi.registerTool({
      name: tool.name, label: tool.title, description: tool.description,
      parameters: z.toJSONSchema(tool.schema, { target: "draft-7" }),
      async execute(_toolCallId, params, signal) {
        const result = await executeTool(tool, params, signal);
        return { content: result.content, details: result.structuredContent };
      },
    });
  }
}
