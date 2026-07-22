"use client";

import { useThread } from "@assistant-ui/react";
import type { FitAddon } from "@xterm/addon-fit";
import type { Terminal } from "@xterm/xterm";
import { AlertTriangle, LoaderCircle, RefreshCw, SquareTerminal } from "lucide-react";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";

import {
  decodeTerminalOutput,
  encodeTerminalInput,
  terminalReconnectDelay,
  terminalWebSocketURL,
} from "@/lib/terminal-protocol.mjs";

const TERMINAL_PREPARE_LIMIT_MS = 4 * 60 * 1000;
const TERMINAL_RECONNECT_LIMIT = 5;
const TERMINAL_STABLE_CONNECTION_MS = 10 * 1000;

type ShellStatus =
  | "not-started"
  | "connecting"
  | "waiting"
  | "ready"
  | "reclaimed"
  | "error"
  | "exited";

type TerminalServerFrame = {
  type?: string;
  code?: string;
  error?: string;
  exit_code?: number;
};

export function ShellPage({
  sessionID,
  active,
  setHeaderActions,
}: {
  sessionID: string;
  active: boolean;
  setHeaderActions: (node: ReactNode) => void;
}) {
  const hasMessages = useThread((thread) => thread.messages.length > 0);
  const environmentPreparing = useThread((thread) => thread.isRunning);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const terminalIDRef = useRef<string | null>(null);
  const createInFlightRef = useRef(false);
  const mountedRef = useRef(true);
  const connectionGenerationRef = useRef(0);
  const outputOffsetRef = useRef(0);
  const prepareStartedAtRef = useRef(0);
  const prepareAttemptRef = useRef(0);
  const reconnectAttemptRef = useRef(0);
  const retryTimerRef = useRef<number | null>(null);
  const stableConnectionTimerRef = useRef<number | null>(null);
  const [xtermReady, setXtermReady] = useState(false);
  const [status, setStatus] = useState<ShellStatus>(hasMessages ? "connecting" : "not-started");
  const [error, setError] = useState<string | null>(null);
  const [exitCode, setExitCode] = useState<number | null>(null);

  const clearRetryTimer = useCallback(() => {
    if (retryTimerRef.current != null) {
      window.clearTimeout(retryTimerRef.current);
      retryTimerRef.current = null;
    }
  }, []);

  const clearStableConnectionTimer = useCallback(() => {
    if (stableConnectionTimerRef.current != null) {
      window.clearTimeout(stableConnectionTimerRef.current);
      stableConnectionTimerRef.current = null;
    }
  }, []);

  const closeConnection = useCallback(() => {
    clearStableConnectionTimer();
    connectionGenerationRef.current++;
    const socket = socketRef.current;
    socketRef.current = null;
    socket?.close();
  }, [clearStableConnectionTimer]);

  const deleteTerminal = useCallback(
    async (terminalID: string) => {
      try {
        await fetch(
          `/api/conversations/${encodeURIComponent(sessionID)}/terminal/${encodeURIComponent(terminalID)}`,
          { method: "DELETE", keepalive: true },
        );
      } catch {
        // Gateway's disconnect lease remains the fallback when explicit deletion fails.
      }
    },
    [sessionID],
  );

  const sendResize = useCallback(() => {
    const socket = socketRef.current;
    const terminal = terminalRef.current;
    if (!terminal || !socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(JSON.stringify({ type: "resize", cols: terminal.cols, rows: terminal.rows }));
  }, []);

  const connectWebSocketRef = useRef<(terminalID: string, takeover: boolean) => void>(() => {});

  const connectWebSocket = useCallback(
    (terminalID: string, takeover: boolean) => {
      clearRetryTimer();
      setStatus("connecting");
      setError(null);

      const connectionGeneration = ++connectionGenerationRef.current;

      const socket = new WebSocket(
        terminalWebSocketURL(
          sessionID,
          terminalID,
          outputOffsetRef.current,
          takeover,
          window.location,
        ),
      );
      socket.binaryType = "arraybuffer";
      socketRef.current = socket;

      socket.onmessage = (event) => {
        if (
          connectionGeneration !== connectionGenerationRef.current ||
          socketRef.current !== socket
        )
          return;
        if (event.data instanceof ArrayBuffer) {
          const frame = decodeTerminalOutput(event.data);
          if (!frame) return;
          terminalRef.current?.write(frame.data);
          outputOffsetRef.current =
            frame.kind === "replay" && frame.offset != null
              ? frame.offset + frame.data.length
              : outputOffsetRef.current + frame.data.length;
          return;
        }
        if (typeof event.data !== "string") return;
        let frame: TerminalServerFrame;
        try {
          frame = JSON.parse(event.data) as TerminalServerFrame;
        } catch {
          return;
        }
        if (frame.type === "connected") {
          setStatus("ready");
          sendResize();
          terminalRef.current?.focus();
          clearStableConnectionTimer();
          stableConnectionTimerRef.current = window.setTimeout(() => {
            if (
              mountedRef.current &&
              connectionGeneration === connectionGenerationRef.current &&
              socketRef.current === socket
            ) {
              reconnectAttemptRef.current = 0;
            }
          }, TERMINAL_STABLE_CONNECTION_MS);
          return;
        }
        if (frame.type === "exit") {
          clearStableConnectionTimer();
          connectionGenerationRef.current++;
          setExitCode(typeof frame.exit_code === "number" ? frame.exit_code : null);
          setStatus("exited");
        } else if (frame.type === "error") {
          clearStableConnectionTimer();
          connectionGenerationRef.current++;
          if (frame.code === "SESSION_GONE") {
            terminalIDRef.current = null;
            prepareStartedAtRef.current = 0;
            prepareAttemptRef.current = 0;
          }
          setError(frame.error || "The terminal connection failed");
          setStatus(frame.code === "SESSION_GONE" ? "reclaimed" : "error");
        }
      };

      socket.onclose = () => {
        if (socketRef.current === socket) socketRef.current = null;
        if (!mountedRef.current || connectionGeneration !== connectionGenerationRef.current) return;
        clearStableConnectionTimer();
        const attempt = reconnectAttemptRef.current;
        if (attempt >= TERMINAL_RECONNECT_LIMIT) {
          setError("The terminal connection was interrupted");
          setStatus("error");
          return;
        }
        reconnectAttemptRef.current = attempt + 1;
        setStatus("connecting");
        retryTimerRef.current = window.setTimeout(
          () => connectWebSocketRef.current(terminalID, true),
          terminalReconnectDelay(attempt),
        );
      };
    },
    [clearRetryTimer, clearStableConnectionTimer, sendResize, sessionID],
  );

  useEffect(() => {
    connectWebSocketRef.current = connectWebSocket;
  }, [connectWebSocket]);

  const createTerminalRef = useRef<() => void>(() => {});
  const createTerminal = useCallback(async () => {
    if (!mountedRef.current || createInFlightRef.current || terminalIDRef.current) return;
    createInFlightRef.current = true;
    clearRetryTimer();
    setStatus("connecting");
    setError(null);
    if (prepareStartedAtRef.current === 0) prepareStartedAtRef.current = Date.now();

    try {
      const response = await fetch(`/api/conversations/${encodeURIComponent(sessionID)}/terminal`, {
        method: "POST",
      });
      if (!response.ok) {
        if ((response.status === 425 || response.status === 502) && environmentPreparing) {
          if (Date.now() - prepareStartedAtRef.current >= TERMINAL_PREPARE_LIMIT_MS) {
            prepareStartedAtRef.current = 0;
            prepareAttemptRef.current = 0;
            setError("The sandbox did not become ready in time");
            setStatus("error");
            return;
          }
          const attempt = prepareAttemptRef.current++;
          setStatus("waiting");
          retryTimerRef.current = window.setTimeout(
            () => createTerminalRef.current(),
            terminalReconnectDelay(attempt),
          );
          return;
        }
        if (response.status === 502) {
          prepareStartedAtRef.current = 0;
          prepareAttemptRef.current = 0;
          setStatus("reclaimed");
          return;
        }
        prepareStartedAtRef.current = 0;
        prepareAttemptRef.current = 0;
        const payload = (await response.json().catch(() => null)) as {
          error?: { message?: string };
        } | null;
        setError(payload?.error?.message || "Could not create a terminal");
        setStatus("error");
        return;
      }

      const body = (await response.json()) as { session_id?: string };
      if (!body.session_id) throw new Error("Terminal session id is missing");
      if (!mountedRef.current) {
        void deleteTerminal(body.session_id);
        return;
      }
      terminalIDRef.current = body.session_id;
      prepareStartedAtRef.current = 0;
      prepareAttemptRef.current = 0;
      reconnectAttemptRef.current = 0;
      connectWebSocketRef.current(body.session_id, false);
    } catch (createError) {
      if (!mountedRef.current) return;
      if (environmentPreparing) {
        if (Date.now() - prepareStartedAtRef.current >= TERMINAL_PREPARE_LIMIT_MS) {
          prepareStartedAtRef.current = 0;
          prepareAttemptRef.current = 0;
          setError("The sandbox did not become ready in time");
          setStatus("error");
          return;
        }
        const attempt = prepareAttemptRef.current++;
        setStatus("waiting");
        retryTimerRef.current = window.setTimeout(
          () => createTerminalRef.current(),
          terminalReconnectDelay(attempt),
        );
      } else {
        prepareStartedAtRef.current = 0;
        prepareAttemptRef.current = 0;
        setError(
          createError instanceof Error ? createError.message : "Could not create a terminal",
        );
        setStatus("error");
      }
    } finally {
      createInFlightRef.current = false;
    }
  }, [clearRetryTimer, deleteTerminal, environmentPreparing, sessionID]);

  useEffect(() => {
    createTerminalRef.current = () => void createTerminal();
  }, [createTerminal]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      clearRetryTimer();
      clearStableConnectionTimer();
      closeConnection();
      const terminalID = terminalIDRef.current;
      terminalIDRef.current = null;
      if (terminalID) void deleteTerminal(terminalID);
    };
  }, [clearRetryTimer, clearStableConnectionTimer, closeConnection, deleteTerminal]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    let disposed = false;
    let resizeObserver: ResizeObserver | null = null;
    let terminal: Terminal | null = null;

    void Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")])
      .then(([{ Terminal: XTerm }, { FitAddon: XTermFitAddon }]) => {
        if (disposed) return;
        terminal = new XTerm({
          cursorBlink: true,
          convertEol: true,
          fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
          fontSize: 13,
          scrollback: 5000,
          theme: {
            background: "#111827",
            foreground: "#e5e7eb",
            cursor: "#60a5fa",
            selectionBackground: "#374151",
          },
        });
        const fitAddon = new XTermFitAddon();
        terminal.loadAddon(fitAddon);
        terminal.open(container);
        terminal.onData((data) => {
          const socket = socketRef.current;
          if (socket?.readyState === WebSocket.OPEN) socket.send(encodeTerminalInput(data));
        });
        terminal.onResize(() => sendResize());
        terminalRef.current = terminal;
        fitAddonRef.current = fitAddon;
        fitAddon.fit();
        resizeObserver = new ResizeObserver(() => fitAddon.fit());
        resizeObserver.observe(container);
        setXtermReady(true);
      })
      .catch((loadError: unknown) => {
        if (disposed) return;
        setError(loadError instanceof Error ? loadError.message : "Could not load the terminal");
        setStatus("error");
      });

    return () => {
      disposed = true;
      resizeObserver?.disconnect();
      terminal?.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
    };
  }, [sendResize]);

  useEffect(() => {
    if (!active) return;
    requestAnimationFrame(() => {
      fitAddonRef.current?.fit();
      terminalRef.current?.focus();
    });
  }, [active]);

  useEffect(() => {
    if (!active || !xtermReady || !hasMessages || terminalIDRef.current) return;
    void createTerminal();
  }, [active, createTerminal, hasMessages, xtermReady]);

  useEffect(() => {
    if (!active) return;
    setHeaderActions(null);
    return () => setHeaderActions(null);
  }, [active, setHeaderActions]);

  const retry = useCallback(async () => {
    setError(null);
    const terminalID = terminalIDRef.current;
    if (terminalID) {
      try {
        const response = await fetch(
          `/api/conversations/${encodeURIComponent(sessionID)}/terminal/${encodeURIComponent(terminalID)}`,
          { cache: "no-store" },
        );
        if (!response.ok) {
          if (response.status === 404 || response.status === 502) {
            terminalIDRef.current = null;
            prepareStartedAtRef.current = 0;
            prepareAttemptRef.current = 0;
            setStatus(response.status === 502 ? "reclaimed" : "connecting");
            if (response.status === 404) void createTerminal();
            return;
          }
          throw new Error("Could not inspect the terminal session");
        }
      } catch (retryError) {
        setError(retryError instanceof Error ? retryError.message : "Could not reconnect");
        setStatus("error");
        return;
      }
      reconnectAttemptRef.current = 0;
      connectWebSocketRef.current(terminalID, true);
    } else {
      prepareStartedAtRef.current = Date.now();
      prepareAttemptRef.current = 0;
      void createTerminal();
    }
  }, [createTerminal, sessionID]);

  const restart = useCallback(async () => {
    clearRetryTimer();
    closeConnection();
    const terminalID = terminalIDRef.current;
    terminalIDRef.current = null;
    if (terminalID) await deleteTerminal(terminalID);
    terminalRef.current?.clear();
    outputOffsetRef.current = 0;
    prepareStartedAtRef.current = Date.now();
    prepareAttemptRef.current = 0;
    reconnectAttemptRef.current = 0;
    setExitCode(null);
    void createTerminal();
  }, [clearRetryTimer, closeConnection, createTerminal, deleteTerminal]);

  return (
    <div className="relative h-full min-h-0 bg-[#111827]">
      <div ref={containerRef} className="h-full min-h-0 p-2" aria-label="Sandbox shell" />
      {status !== "ready" ? (
        <ShellOverlay
          status={status}
          hasMessages={hasMessages}
          error={error}
          exitCode={exitCode}
          onRetry={() => void retry()}
          onRestart={() => void restart()}
        />
      ) : null}
    </div>
  );
}

function ShellOverlay({
  status,
  hasMessages,
  error,
  exitCode,
  onRetry,
  onRestart,
}: {
  status: ShellStatus;
  hasMessages: boolean;
  error: string | null;
  exitCode: number | null;
  onRetry: () => void;
  onRestart: () => void;
}) {
  if (status === "connecting" || status === "waiting") {
    return (
      <div className="absolute inset-0 flex flex-col items-center justify-center bg-[#111827] px-6 text-center">
        <LoaderCircle className="mb-3 size-7 animate-spin text-blue-400" />
        <p className="text-sm font-medium text-slate-100">
          {status === "waiting" ? "Waiting for the environment" : "Opening shell"}
        </p>
        <p className="mt-1 text-xs text-slate-400">Connecting to the sandbox terminal…</p>
      </div>
    );
  }

  if (status === "not-started" || status === "reclaimed") {
    return (
      <div className="absolute inset-0 flex flex-col items-center justify-center bg-[#111827] px-6 text-center">
        <SquareTerminal className="mb-3 size-8 text-slate-500" />
        <p className="text-sm font-medium text-slate-100">
          {status === "reclaimed" ? "Sandbox reclaimed" : "Shell not ready"}
        </p>
        <p className="mt-1 max-w-sm text-xs leading-5 text-slate-400">
          {hasMessages
            ? "Continue the conversation to restore the sandbox, then reopen the shell."
            : "Send the first message to prepare the sandbox environment."}
        </p>
      </div>
    );
  }

  if (status === "exited") {
    return (
      <div className="absolute inset-0 flex flex-col items-center justify-center bg-[#111827]/95 px-6 text-center">
        <SquareTerminal className="mb-3 size-8 text-slate-500" />
        <p className="text-sm font-medium text-slate-100">Shell exited</p>
        <p className="mt-1 text-xs text-slate-400">
          {exitCode == null ? "The shell process ended." : `Exit code ${exitCode}`}
        </p>
        <button
          type="button"
          onClick={onRestart}
          className="mt-4 inline-flex h-8 items-center gap-1.5 rounded-md border border-slate-600 bg-slate-800 px-3 text-xs font-medium text-slate-100 hover:bg-slate-700 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-blue-400"
        >
          <RefreshCw className="size-3.5" />
          Start new shell
        </button>
      </div>
    );
  }

  return (
    <div className="absolute inset-0 flex flex-col items-center justify-center bg-[#111827]/95 px-6 text-center">
      <AlertTriangle className="mb-3 size-8 text-amber-400" />
      <p className="text-sm font-medium text-slate-100">Shell unavailable</p>
      <p className="mt-1 max-w-sm text-xs leading-5 text-slate-400">
        {error || "The sandbox terminal could not be reached."}
      </p>
      <button
        type="button"
        onClick={onRetry}
        className="mt-4 inline-flex h-8 items-center gap-1.5 rounded-md border border-slate-600 bg-slate-800 px-3 text-xs font-medium text-slate-100 hover:bg-slate-700 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-blue-400"
      >
        <RefreshCw className="size-3.5" />
        Retry
      </button>
    </div>
  );
}
