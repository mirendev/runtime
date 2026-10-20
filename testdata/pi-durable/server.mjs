import http from "node:http";
import { setTimeout as delay } from "node:timers/promises";
import { BACKGROUND_CONTEXT } from "@earendil-works/chord/context";
import { Type } from "@earendil-works/pi-ai";
import { createModels } from "@earendil-works/pi-ai/models";
import { anthropicProvider } from "@earendil-works/pi-ai/providers/anthropic";
import { fauxAssistantMessage, fauxProvider, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";
import { AssistantEntry, createRegistry, defineExtension, defineTool, Harness } from "@earendil-works/pi-durable";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";

const context = BACKGROUND_CONTEXT;
const token = process.env.AGENT_TOKEN;
if (!token) throw new Error("AGENT_TOKEN is required");
const sqlitePath = process.env.SQLITE_PATH;
if (!sqlitePath) throw new Error("SQLITE_PATH is required (provided by the miren-sqlite addon)");
const toolDelay = Number(process.env.TOOL_DELAY_MS ?? 10000);
if (!Number.isFinite(toolDelay) || toolDelay < 0 || toolDelay > 60000) {
  throw new Error("TOOL_DELAY_MS must be between 0 and 60000");
}

const models = createModels();
const faux = fauxProvider({ tokensPerSecond: 100000 });
// Derive the next response from durable history, never a volatile call count.
const respond = (transcript) => {
  faux.appendResponses([respond]);
  const last = transcript.messages.at(-1);
  if (last?.role === "toolResult") {
    const text = last.content.filter((b) => b.type === "text").map((b) => b.text).join("");
    return fauxAssistantMessage(`Result: ${text}`);
  }
  const user = [...transcript.messages].reverse().find((m) => m.role === "user");
  const text = typeof user.content === "string" ? user.content : user.content.filter((b) => b.type === "text").map((b) => b.text).join("");
  return fauxAssistantMessage([fauxToolCall("uppercase", { text })], { stopReason: "toolUse" });
};
faux.setResponses([respond]);
models.setProvider(faux.provider);
if (process.env.ANTHROPIC_API_KEY) models.setProvider(anthropicProvider());

const provider = process.env.AGENT_PROVIDER ?? "faux";
let model;
if (provider === "faux") {
  model = { provider: "faux", modelId: "faux-1" };
} else if (provider === "anthropic") {
  if (!process.env.ANTHROPIC_API_KEY) throw new Error("ANTHROPIC_API_KEY is required");
  model = { provider: "anthropic", modelId: process.env.MODEL_ID ?? "claude-haiku-4-5" };
} else {
  throw new Error(`Unknown AGENT_PROVIDER: ${provider}`);
}

const registry = createRegistry();
registry.install(defineExtension({
  name: "text-tools",
  tools: [defineTool({
    name: "uppercase",
    description: "Convert text to uppercase. Use this tool whenever asked to uppercase text.",
    parameters: Type.Object({ text: Type.String() }),
    replay: "safe",
    async execute({ text }, api, callContext) {
      await api.details({ phase: "waiting", taskId: api.taskId }, callContext);
      console.log(JSON.stringify({ event: "tool_started", taskId: api.taskId }));
      await delay(toolDelay, undefined, { signal: callContext.abortSignal });
      return { output: [{ type: "text", text: text.toUpperCase() }] };
    },
  })],
}));

const harness = await Harness.open(
  await openNodeSqliteStorage(sqlitePath),
  { models, registry, onReport: (error, info) => console.error("harness_report", info, error) },
  context,
);
harness.resume();

let stopping = false;
function send(res, status, body) {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify(body));
}

const server = http.createServer(async (req, res) => {
  try {
    if (stopping) return send(res, 503, { error: "shutting_down" });
    const path = new URL(req.url, "http://agent").pathname;
    if (req.method === "GET" && path === "/health") return send(res, 200, { ok: true });
    if (req.headers.authorization !== `Bearer ${token}`) return send(res, 401, { error: "unauthorized" });
    if (req.method === "POST" && path === "/conversations") {
      const conversation = await harness.createConversation({
        ownership: { kind: "ownerless" },
        agent: { model, instructions: "You are a text assistant. Use uppercase for uppercase requests; return its result." },
      }, context);
      return send(res, 201, { id: conversation.id });
    }
    const route = path.match(/^\/conversations\/([1-9]\d*)\/(submissions|view)(?:\/([1-9]\d*))?$/);
    if (!route || !Number.isSafeInteger(Number(route[1]))) return send(res, 404, { error: "not_found" });
    const conversation = await harness.conversation(Number(route[1]), context);
    if (!conversation) return send(res, 404, { error: "not_found" });
    if (req.method === "POST" && route[2] === "submissions" && !route[3]) {
      let body = "";
      for await (const chunk of req) {
        body += chunk;
        if (Buffer.byteLength(body) > 16384) return send(res, 413, { error: "body_too_large" });
      }
      let input;
      try { input = JSON.parse(body); }
      catch { return send(res, 400, { error: "invalid_json" }); }
      if (typeof input?.requestId !== "string" || !input.requestId || typeof input.content !== "string" || !input.content.trim()) {
        return send(res, 400, { error: "Expected nonempty requestId and content strings" });
      }
      const submission = await conversation.submit({ type: "input", requestId: input.requestId, content: input.content, whenBusy: "followUp" }, context);
      return send(res, 202, { id: submission.id, conversationId: conversation.id });
    }
    if (req.method === "GET" && route[2] === "submissions" && Number.isSafeInteger(Number(route[3]))) {
      const submission = await harness.submission(Number(route[3]), context);
      if (!submission) return send(res, 404, { error: "not_found" });
      const record = await submission.status(context);
      if (record.conversationId !== conversation.id) return send(res, 404, { error: "not_found" });
      let answer;
      if (record.status === "done" && record.type === "input") {
        answer = await conversation.commit((tx) => tx.entry(AssistantEntry, record.answer), context);
      }
      return send(res, 200, { submission: record, answer });
    }
    if (req.method === "GET" && route[2] === "view" && !route[3]) {
      const watch = await conversation.watch(context);
      try { return send(res, 200, watch.value); }
      finally { await watch.stop(); }
    }
    return send(res, 404, { error: "not_found" });
  } catch (error) {
    console.error(error);
    send(res, 500, { error: "internal_error" });
  }
});

async function shutdown(signal) {
  if (stopping) return;
  stopping = true;
  console.log(JSON.stringify({ event: "shutdown", signal }));
  const deadline = setTimeout(() => process.exit(1), 20000);
  deadline.unref();
  const closed = new Promise((resolve) => server.close(resolve));
  try {
    await harness.close(context);
    server.closeAllConnections();
    await closed;
    clearTimeout(deadline);
  } catch (error) {
    console.error(error);
    process.exit(1);
  }
}
process.once("SIGTERM", () => void shutdown("SIGTERM"));
process.once("SIGINT", () => void shutdown("SIGINT"));
server.listen(Number(process.env.PORT ?? 8080), "0.0.0.0", () => console.log("agent_ready"));
