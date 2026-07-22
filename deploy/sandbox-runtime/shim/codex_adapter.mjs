#!/usr/bin/env node

import { Codex } from "@openai/codex-sdk";

const emit = (event) => process.stdout.write(`${JSON.stringify(event)}\n`);

const readRequest = async () => {
  let raw = "";
  for await (const chunk of process.stdin) raw += chunk;
  const request = JSON.parse(raw);
  if (!request.prompt) throw new Error("request.prompt is required");
  return request;
};

const sessionMissing = (error) => {
  const message = String(error?.message || error || "").toLowerCase();
  return ["thread not found", "session not found", "no rollout found"].some((marker) =>
    message.includes(marker),
  );
};

const safeMessage = (error) =>
  String(error?.message || error || "Codex execution failed").slice(0, 500);

const compactConfig = (value) => {
  if (Array.isArray(value)) return value.filter((item) => item != null).map(compactConfig);
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.entries(value)
      .filter(([, child]) => child != null)
      .map(([key, child]) => [key, compactConfig(child)]),
  );
};

const mcpConfig = (servers) => {
  if (!servers || typeof servers !== "object") return {};
  return Object.fromEntries(
    Object.entries(servers).map(([name, source]) => {
      const config = { ...source };
      delete config.type;
      if (config.headers) {
        config.http_headers = config.headers;
        delete config.headers;
      }
      return [name, compactConfig(config)];
    }),
  );
};

const toolEvent = (item) => {
  if (item.type === "command_execution") {
    return {
      type: "tool_use",
      id: item.id,
      name: "command_execution",
      input: { command: item.command },
    };
  }
  if (item.type === "file_change") {
    return { type: "tool_use", id: item.id, name: "file_change", input: item.changes || [] };
  }
  if (item.type === "mcp_tool_call") {
    return {
      type: "tool_use",
      id: item.id,
      name: `${item.server || "mcp"}.${item.tool || "tool"}`,
      input: item.arguments || {},
      _cocola_mcp_server: String(item.server || ""),
    };
  }
  return null;
};

const toolResult = (item) => {
  if (item.type === "command_execution") {
    return {
      type: "tool_result",
      tool_use_id: item.id,
      is_error: item.status === "failed" || Number(item.exit_code || 0) !== 0,
      content: String(item.aggregated_output || "").slice(0, 4000),
    };
  }
  if (item.type === "file_change") {
    return {
      type: "tool_result",
      tool_use_id: item.id,
      is_error: item.status === "failed",
      content: JSON.stringify(item.changes || []).slice(0, 4000),
    };
  }
  if (item.type === "mcp_tool_call") {
    return {
      type: "tool_result",
      tool_use_id: item.id,
      is_error: Boolean(item.error),
      content: JSON.stringify(item.error || item.result || {}).slice(0, 4000),
    };
  }
  return null;
};

const run = async (request) => {
  const gatewayRoot = String(process.env.COCOLA_LLM_BASE_URL || "").replace(/\/$/, "");
  if (!gatewayRoot) throw new Error("COCOLA_LLM_BASE_URL is required");
  const headers = { "x-cocola-conversation-id": request.conversation_id || "" };
  if (request.traceparent) headers.traceparent = request.traceparent;
  const config = {
    model_provider: "cocola",
    model_providers: {
      cocola: {
        name: "Cocola",
        base_url: `${gatewayRoot}/v1`,
        env_key: "CODEX_API_KEY",
        wire_api: "responses",
        http_headers: headers,
      },
    },
    mcp_servers: mcpConfig(request.mcp_servers),
  };
  if (request.system_prompt) config.developer_instructions = request.system_prompt;

  const codex = new Codex({
    apiKey: process.env.CODEX_API_KEY,
    baseUrl: `${gatewayRoot}/v1`,
    config,
  });
  const threadOptions = {
    model: request.model || process.env.CODEX_MODEL,
    workingDirectory: request.cwd || process.env.COCOLA_AGENT_CWD || "/workspace",
    skipGitRepoCheck: true,
    sandboxMode: "danger-full-access",
    approvalPolicy: "never",
  };
  const thread = request.resume
    ? codex.resumeThread(request.resume, threadOptions)
    : codex.startThread(threadOptions);
  const skillId = String(request.skill_id || "").trim();
  const prompt = skillId ? `$${skillId}\n\n${request.prompt}` : request.prompt;
  const { events } = await thread.runStreamed(prompt);
  const textByItem = new Map();
  const startedTools = new Set();
  let threadId = request.resume || "";
  let terminalError = "";

  try {
    for await (const event of events) {
      if (event.type === "thread.started") {
        threadId = event.thread_id;
        emit({ type: "start", session_id: threadId });
        continue;
      }
      if (event.type === "item.started" || event.type === "item.updated") {
        const item = event.item;
        if (item?.type === "agent_message" || item?.type === "reasoning") {
          const previous = textByItem.get(item.id) || "";
          const current = String(item.text || "");
          const delta = current.startsWith(previous) ? current.slice(previous.length) : current;
          textByItem.set(item.id, current);
          if (delta) emit({ type: item.type === "reasoning" ? "thinking" : "text", text: delta });
        } else if (item?.type === "todo_list") {
          emit({ type: "progress", id: "todo-list", items: item.items || [] });
        } else if (item && !startedTools.has(item.id)) {
          const mapped = toolEvent(item);
          if (mapped) {
            startedTools.add(item.id);
            emit(mapped);
          }
        }
        continue;
      }
      if (event.type === "item.completed") {
        const item = event.item;
        if (item?.type === "agent_message" || item?.type === "reasoning") {
          const previous = textByItem.get(item.id) || "";
          const current = String(item.text || "");
          const delta = current.startsWith(previous) ? current.slice(previous.length) : current;
          if (delta) emit({ type: item.type === "reasoning" ? "thinking" : "text", text: delta });
        } else if (item?.type === "todo_list") {
          emit({ type: "progress", id: "todo-list", items: item.items || [] });
        } else if (item?.type === "error") {
          process.stderr.write("non-fatal Codex item error\n");
        } else if (item) {
          if (!startedTools.has(item.id)) {
            const started = toolEvent(item);
            if (started) emit(started);
          }
          const result = toolResult(item);
          if (result) emit(result);
        }
        continue;
      }
      if (event.type === "turn.completed") {
        emit({ type: "result", is_error: false, session_id: threadId, usage: event.usage || {} });
        emit({ type: "done", session_id: threadId });
        return;
      }
      if (event.type === "turn.failed") {
        terminalError = safeMessage(event.error || event.message);
      } else if (event.type === "error" && !terminalError) {
        terminalError = safeMessage(event.error || event.message);
      }
    }
  } catch (error) {
    if (terminalError) throw new Error(terminalError, { cause: error });
    throw error;
  }
  if (terminalError) throw new Error(terminalError);
  throw new Error("Codex stream ended without a terminal event");
};

let request;
try {
  request = await readRequest();
  await run(request);
} catch (error) {
  const event = { type: "error", stage: "run", error: safeMessage(error) };
  if (request?.resume && sessionMissing(error)) event.code = "SESSION_NOT_FOUND";
  emit(event);
  process.exitCode = 1;
}
