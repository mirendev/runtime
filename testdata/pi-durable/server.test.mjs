import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import net from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { test } from "node:test";

test("independent conversations retain isolated, deduplicated work across a process crash", { timeout: 30000 }, async () => {
  const directory = await mkdtemp(join(tmpdir(), "pi-conversations-"));
  const listener = net.createServer();
  listener.listen(0, "127.0.0.1");
  await once(listener, "listening");
  const port = listener.address().port;
  await new Promise((resolve) => listener.close(resolve));
  let child;
  let logs = "";

  async function request(path, body, authenticated = true) {
    const response = await fetch(`http://127.0.0.1:${port}${path}`, {
      method: body === undefined ? "GET" : "POST",
      headers: { ...(authenticated ? { authorization: "Bearer test-token" } : {}), "content-type": "application/json" },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      signal: AbortSignal.timeout(1000),
    });
    return { status: response.status, body: await response.json() };
  }

  async function eventually(read) {
    for (let attempt = 0; attempt < 150; attempt++) {
      const value = await read();
      if (value) return value;
      await delay(50);
    }
    assert.fail(`Timed out waiting for agent state. Logs: ${logs}`);
  }

  async function start(provider = "faux") {
    child = spawn(process.execPath, ["server.mjs"], {
      cwd: new URL(".", import.meta.url),
      env: { ...process.env, PORT: String(port), SQLITE_PATH: join(directory, "data.db"), AGENT_TOKEN: "test-token", AGENT_PROVIDER: provider, ANTHROPIC_API_KEY: "test-key", TOOL_DELAY_MS: "2000" },
      stdio: ["ignore", "pipe", "pipe"],
    });
    child.stdout.on("data", (data) => { logs += data; });
    child.stderr.on("data", (data) => { logs += data; });
    await eventually(async () => {
      if (child.exitCode !== null) assert.fail(`Agent exited: ${logs}`);
      try { return (await request("/health")).status === 200; }
      catch { return false; }
    });
  }

  async function stop(signal) {
    if (!child || child.exitCode !== null || child.signalCode !== null) return;
    const exited = once(child, "exit");
    child.kill(signal);
    await exited;
  }

  async function completed(chat, submission) {
    return eventually(async () => {
      const response = await request(`/conversations/${chat}/submissions/${submission}`);
      assert.equal(response.status, 200);
      return response.body.submission.status === "done" && response.body;
    });
  }

  function answer(response) {
    return response.answer.model.flatMap((message) => message.content).filter((block) => block.type === "text").map((block) => block.text).join("");
  }

  try {
    await start();
    assert.equal((await request("/conversations", {}, false)).status, 401);
    const chats = await Promise.all([request("/conversations", {}), request("/conversations", {})]);
    assert.ok(chats.every((chat) => chat.status === 201));
    const [a, b] = chats.map((chat) => chat.body.id);
    assert.notEqual(a, b);
    const inputs = [{ requestId: "same-request", content: "alpha MiXeD 31" }, { requestId: "same-request", content: "beta DISTINCT 97" }];
    const submissions = await Promise.all([request(`/conversations/${a}/submissions`, inputs[0]), request(`/conversations/${b}/submissions`, inputs[1])]);
    assert.ok(submissions.every((submission) => submission.status === 202));
    const [sa, sb] = submissions.map((submission) => submission.body.id);
    assert.notEqual(sa, sb);
    assert.equal((await request(`/conversations/${a}/submissions/${sb}`)).status, 404);
    assert.equal((await request("/conversations/999999/view")).status, 404);
    await eventually(async () => {
      const views = await Promise.all([request(`/conversations/${a}/view`), request(`/conversations/${b}/view`)]);
      return views.every((view) => view.body.docs["pi.live"].tools?.some((tool) => tool.status === "running" && tool.details?.phase === "waiting"));
    });
    await stop("SIGKILL");
    await start();
    assert.equal(answer(await completed(a, sa)), "Result: ALPHA MIXED 31");
    assert.equal(answer(await completed(b, sb)), "Result: BETA DISTINCT 97");
    for (const [index, chat] of [a, b].entries()) {
      const retry = await request(`/conversations/${chat}/submissions`, inputs[index]);
      assert.deepEqual(retry.body, submissions[index].body);
      const view = (await request(`/conversations/${chat}/view`)).body;
      const users = view.entries.filter((entry) => entry.kind === "pi.user");
      assert.deepEqual(users.map((entry) => entry.model[0].content), [inputs[index].content]);
      assert.ok(view.entries.every((entry) => entry.conversationId === chat));
    }
    assert.equal((await request(`/conversations/${b}/submissions/${sa}`)).status, 404);
    const queued = await Promise.all(["first followup", "second followup"].map((content) => request(`/conversations/${a}/submissions`, { requestId: content, content })));
    assert.equal(answer(await completed(a, queued[0].body.id)), "Result: FIRST FOLLOWUP");
    assert.equal(answer(await completed(a, queued[1].body.id)), "Result: SECOND FOLLOWUP");
    const bView = (await request(`/conversations/${b}/view`)).body;
    assert.equal(bView.entries.filter((entry) => entry.kind === "pi.user").length, 1);
    await stop("SIGTERM");
    await start("anthropic");
    const newChat = (await request("/conversations", {})).body.id;
    const newView = (await request(`/conversations/${newChat}/view`)).body;
    assert.equal(newView.docs["pi.agent"].model.provider, "anthropic");
    const oldView = (await request(`/conversations/${b}/view`)).body;
    assert.equal(oldView.docs["pi.agent"].model.provider, "faux");
    const oldChatRequest = await request(`/conversations/${b}/submissions`, { requestId: "after-model-switch", content: "old chat still works" });
    assert.equal(answer(await completed(b, oldChatRequest.body.id)), "Result: OLD CHAT STILL WORKS");
  } finally {
    await stop("SIGTERM");
    await rm(directory, { recursive: true, force: true });
  }
});
