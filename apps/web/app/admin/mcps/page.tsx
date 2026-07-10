"use client";

import { PlugsConnected as McpPageIcon } from "@phosphor-icons/react";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  ChevronDown,
  CircleAlert,
  CircleCheck,
  Eye,
  EyeOff,
  FileTerminal,
  Globe2,
  LoaderCircle,
  Pencil,
  Plus,
  Power,
  PowerOff,
  Save,
  Trash2,
} from "lucide-react";
import {
  AdminAlert,
  AdminConfirmDialog,
  AdminDrawer,
  AdminEmptyState,
  AdminIconButton,
  AdminPage,
  AdminPageHeader,
  AdminPanel,
  AdminRefreshButton,
  AdminStatusBadge,
} from "@/components/admin/admin-ui";
import { Button } from "@/components/ui/button";

type MCPServer = {
  id: string;
  name: string;
  description: string;
  transport: "stdio" | "http" | "sse" | string;
  command?: string;
  args?: string[];
  url_hint?: string;
  env_hints?: Record<string, string>;
  header_hints?: Record<string, string>;
  enabled: boolean;
  default_enabled: boolean;
  status: string;
};

type FormState = {
  id: string;
  name: string;
  description: string;
  transport: "stdio" | "http" | "sse";
  command: string;
  args: string;
  url: string;
  env: string;
  headers: string;
  defaultEnabled: boolean;
  clearEnv: boolean;
  clearHeaders: boolean;
};

const EMPTY_FORM: FormState = {
  id: "",
  name: "",
  description: "",
  transport: "stdio",
  command: "",
  args: "",
  url: "",
  env: "",
  headers: "",
  defaultEnabled: false,
  clearEnv: false,
  clearHeaders: false,
};

const controlClass =
  "h-10 min-w-0 rounded-xl border border-input bg-background/85 px-3 text-sm text-foreground outline-none transition-[border-color,box-shadow] placeholder:text-muted-foreground focus:border-ring focus:ring-2 focus:ring-ring/20";
const textAreaClass =
  "min-h-24 min-w-0 resize-y rounded-xl border border-input bg-background/85 px-3 py-2.5 font-mono text-sm text-foreground outline-none transition-[border-color,box-shadow] placeholder:text-muted-foreground focus:border-ring focus:ring-2 focus:ring-ring/20";

export default function AdminMCPPage() {
  const [mcps, setMcps] = useState<MCPServer[]>([]);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [editing, setEditing] = useState<MCPServer | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [showURL, setShowURL] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [busyID, setBusyID] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<MCPServer | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/admin/mcps", { cache: "no-store" });
      if (!res.ok) throw new Error(await readError(res));
      const data = (await res.json()) as { mcps?: MCPServer[] };
      setMcps(data.mcps ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = () => {
    setEditing(null);
    setForm(EMPTY_FORM);
    setAdvancedOpen(false);
    setShowURL(false);
    setError("");
    setDrawerOpen(true);
  };

  const openEdit = (mcp: MCPServer) => {
    const transport = normalizeTransport(mcp.transport);
    setEditing(mcp);
    setForm({
      ...EMPTY_FORM,
      id: mcp.id,
      name: mcp.name,
      description: mcp.description,
      transport,
      command: mcp.command ?? "",
      args: (mcp.args ?? []).join("\n"),
      defaultEnabled: mcp.default_enabled,
    });
    setAdvancedOpen(false);
    setShowURL(false);
    setError("");
    setDrawerOpen(true);
  };

  const save = async () => {
    setError("");
    setNotice("");
    const name = form.name.trim();
    const id = editing?.id || slugify(form.id || name);
    if (!name || !id) {
      setError("Name is required.");
      return;
    }
    if (form.transport === "stdio" && !form.command.trim()) {
      setError("Command is required for a stdio server.");
      return;
    }
    const keepsRemoteURL =
      editing && normalizeTransport(editing.transport) !== "stdio" && !form.url.trim();
    if (form.transport !== "stdio" && !form.url.trim() && !keepsRemoteURL) {
      setError("URL is required for an HTTP or SSE server.");
      return;
    }

    let env: Record<string, string> | undefined;
    let headers: Record<string, string> | undefined;
    try {
      env = parsePairs(form.env, "Env");
      headers = parsePairs(form.headers, "Headers");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      return;
    }

    setSaving(true);
    try {
      const body: Record<string, unknown> = {
        id,
        name,
        description: form.description.trim(),
        transport: form.transport,
        default_enabled: form.defaultEnabled,
      };
      if (form.transport === "stdio") {
        body.command = form.command.trim();
        body.args = splitArgs(form.args);
        if (env) body.env = env;
        if (form.clearEnv) body.clear_env = true;
      } else {
        if (form.url.trim()) body.url = form.url.trim();
        if (headers) body.headers = headers;
        if (form.clearHeaders) body.clear_headers = true;
      }
      const endpoint = editing
        ? `/api/admin/mcps/${encodeURIComponent(editing.id)}`
        : "/api/admin/mcps";
      const res = await fetch(endpoint, {
        method: editing ? "PATCH" : "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(await readError(res));
      const result = (await res.json()) as MCPServer;
      setNotice(`${result.name} saved. The connection will be checked when an agent uses it.`);
      setDrawerOpen(false);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const mutate = async (mcp: MCPServer, action: "enable" | "disable" | "delete") => {
    setBusyID(mcp.id);
    setError("");
    setNotice("");
    try {
      const endpoint =
        action === "delete"
          ? `/api/admin/mcps/${encodeURIComponent(mcp.id)}`
          : `/api/admin/mcps/${encodeURIComponent(mcp.id)}/${action}`;
      const res = await fetch(endpoint, { method: action === "delete" ? "DELETE" : "POST" });
      if (!res.ok) throw new Error(await readError(res));
      if (action === "delete") setDeleteTarget(null);
      await load();
    } catch (err) {
      if (action === "delete") setDeleteTarget(null);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyID(null);
    }
  };

  return (
    <AdminPage width="standard">
      <AdminPageHeader
        icon={<McpPageIcon className="size-[18px]" weight="duotone" />}
        title="MCP Servers"
        actions={
          <>
            <AdminRefreshButton refreshing={loading} onClick={() => void load()} variant="outline">
              Refresh
            </AdminRefreshButton>
            <Button className="gap-2" onClick={openCreate}>
              <Plus className="size-4" />
              Add server
            </Button>
          </>
        }
      />

      {error && !drawerOpen ? (
        <AdminAlert tone="error" icon={<CircleAlert className="size-4" />}>
          <span aria-live="polite">{error}</span>
        </AdminAlert>
      ) : null}
      {notice ? (
        <AdminAlert tone="success" icon={<CircleCheck className="size-4" />}>
          <span aria-live="polite">{notice}</span>
        </AdminAlert>
      ) : null}

      <AdminPanel contentClassName="p-4 sm:p-5">
        {loading && !mcps.length ? (
          <div className="flex min-h-48 items-center justify-center text-sm text-muted-foreground">
            <LoaderCircle className="mr-2 size-4 animate-spin" />
            Loading MCP servers
          </div>
        ) : mcps.length ? (
          <div className="grid gap-4 md:grid-cols-2">
            {mcps.map((mcp) => (
              <MCPCard
                key={mcp.id}
                mcp={mcp}
                busy={busyID === mcp.id}
                onEdit={() => openEdit(mcp)}
                onToggle={() => void mutate(mcp, mcp.enabled ? "disable" : "enable")}
                onDelete={() => setDeleteTarget(mcp)}
              />
            ))}
          </div>
        ) : (
          <AdminEmptyState
            icon={<McpPageIcon className="size-6" weight="duotone" />}
            title="No MCP servers configured"
            description="Add a server now; Cocola checks the connection when an agent first uses it."
            action={
              <Button className="gap-2" onClick={openCreate}>
                <Plus className="size-4" />
                Add server
              </Button>
            }
          />
        )}
      </AdminPanel>

      <AdminDrawer
        open={drawerOpen}
        onOpenChange={(open) => {
          if (!saving) setDrawerOpen(open);
        }}
        title={editing ? `Edit ${editing.name}` : "Add MCP server"}
        description="Save the configuration now. Its connection is checked in the first agent session that uses it."
        size="lg"
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button variant="outline" disabled={saving} onClick={() => setDrawerOpen(false)}>
              Cancel
            </Button>
            <Button disabled={saving} className="min-w-32 gap-2" onClick={() => void save()}>
              {saving ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <Save className="size-4" />
              )}
              {saving ? "Saving…" : editing ? "Save changes" : "Add server"}
            </Button>
          </div>
        }
      >
        <div className="space-y-5">
          {error ? (
            <AdminAlert tone="error" icon={<CircleAlert className="size-4" />}>
              <span aria-live="polite">{error}</span>
            </AdminAlert>
          ) : null}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name">
              <input
                className={controlClass}
                value={form.name}
                placeholder="GitHub"
                autoFocus
                onChange={(event) => setForm({ ...form, name: event.target.value })}
              />
            </Field>
            <Field label="Transport">
              <select
                className={controlClass}
                value={form.transport}
                onChange={(event) =>
                  setForm({ ...form, transport: event.target.value as FormState["transport"] })
                }
              >
                <option value="stdio">stdio · Command</option>
                <option value="http">HTTP · URL</option>
                <option value="sse">SSE · URL</option>
              </select>
            </Field>
          </div>

          <p className="-mt-3 text-xs leading-5 text-muted-foreground">
            {form.transport === "stdio"
              ? "stdio starts a local process, so it uses Command instead of URL."
              : `${form.transport === "http" ? "HTTP" : "SSE"} connects to a remote URL.`}
          </p>

          <Field label="Description" optional>
            <input
              className={controlClass}
              value={form.description}
              placeholder="Repository tools for agent sessions"
              onChange={(event) => setForm({ ...form, description: event.target.value })}
            />
          </Field>

          {form.transport === "stdio" ? (
            <>
              <Field label="Command">
                <input
                  className={`${controlClass} font-mono`}
                  value={form.command}
                  placeholder="npx"
                  onChange={(event) => setForm({ ...form, command: event.target.value })}
                />
              </Field>
              <Field label="Arguments" optional hint="One argument per line.">
                <textarea
                  className={textAreaClass}
                  value={form.args}
                  placeholder={"-y\n@modelcontextprotocol/server-github"}
                  onChange={(event) => setForm({ ...form, args: event.target.value })}
                />
              </Field>
              <SecretPairsField
                label="Env"
                value={form.env}
                placeholder="GITHUB_TOKEN=..."
                savedHints={editing?.env_hints}
                clearSaved={form.clearEnv}
                onClearSaved={(clearEnv) =>
                  setForm({ ...form, clearEnv, env: clearEnv ? "" : form.env })
                }
                onChange={(env) => setForm({ ...form, env, clearEnv: false })}
              />
            </>
          ) : (
            <>
              <Field
                label="URL"
                hint={
                  editing
                    ? "Leave blank to keep the saved URL."
                    : "Paste the complete provider URL."
                }
              >
                <div className="relative">
                  <input
                    type={showURL ? "text" : "password"}
                    className={`${controlClass} w-full pr-11 font-mono`}
                    value={form.url}
                    placeholder={
                      editing
                        ? editing.url_hint || "Saved URL"
                        : "https://mcp.example.com/api?token=..."
                    }
                    autoComplete="off"
                    onChange={(event) => setForm({ ...form, url: event.target.value })}
                  />
                  <button
                    type="button"
                    aria-label={showURL ? "Hide URL" : "Show URL"}
                    className="absolute inset-y-0 right-1 grid w-9 place-items-center rounded-lg text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30"
                    onClick={() => setShowURL((show) => !show)}
                  >
                    {showURL ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                  </button>
                </div>
              </Field>
              <p className="-mt-3 text-xs leading-5 text-muted-foreground">
                The complete URL is encrypted. Lists only show its scheme, host, and path.
              </p>
              <SecretPairsField
                label="Headers"
                value={form.headers}
                placeholder="Authorization=Bearer ..."
                savedHints={editing?.header_hints}
                clearSaved={form.clearHeaders}
                onClearSaved={(clearHeaders) =>
                  setForm({ ...form, clearHeaders, headers: clearHeaders ? "" : form.headers })
                }
                onChange={(headers) => setForm({ ...form, headers, clearHeaders: false })}
              />
            </>
          )}

          <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-border/70 bg-muted/30 p-3.5">
            <input
              type="checkbox"
              className="mt-0.5 size-4 rounded border-input accent-primary"
              checked={form.defaultEnabled}
              onChange={(event) => setForm({ ...form, defaultEnabled: event.target.checked })}
            />
            <span>
              <span className="block text-sm font-medium">Enabled for users by default</span>
              <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">
                Users can still turn this server off for their own agent sessions.
              </span>
            </span>
          </label>

          {!editing ? (
            <div>
              <button
                type="button"
                className="inline-flex h-9 items-center gap-2 rounded-xl px-2 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
                onClick={() => setAdvancedOpen((open) => !open)}
              >
                <ChevronDown
                  className={`size-4 transition-transform ${advancedOpen ? "rotate-180" : ""}`}
                />
                Advanced
              </button>
              {advancedOpen ? (
                <div className="mt-3">
                  <Field label="ID" optional hint="Generated from the name when left blank.">
                    <input
                      className={`${controlClass} w-full font-mono`}
                      value={form.id}
                      placeholder={slugify(form.name) || "github"}
                      onChange={(event) => setForm({ ...form, id: event.target.value })}
                    />
                  </Field>
                </div>
              ) : null}
            </div>
          ) : null}
        </div>
      </AdminDrawer>

      <AdminConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
        title="Delete MCP server?"
        description={`This permanently removes ${deleteTarget?.name || deleteTarget?.id || "this server"} from future agent sessions.`}
        confirmLabel="Delete server"
        destructive
        busy={deleteTarget !== null && busyID === deleteTarget.id}
        onConfirm={() => {
          if (deleteTarget) void mutate(deleteTarget, "delete");
        }}
      />
    </AdminPage>
  );
}

function MCPCard({
  mcp,
  busy,
  onEdit,
  onToggle,
  onDelete,
}: {
  mcp: MCPServer;
  busy: boolean;
  onEdit: () => void;
  onToggle: () => void;
  onDelete: () => void;
}) {
  const transport = normalizeTransport(mcp.transport);
  const remote = transport !== "stdio";
  const endpoint = !remote
    ? [mcp.command, ...(mcp.args ?? [])].filter(Boolean).join(" ")
    : mcp.url_hint;
  const TransportIcon = remote ? Globe2 : FileTerminal;
  return (
    <article className="group flex min-h-56 flex-col rounded-2xl border border-border/75 bg-white/45 p-4 shadow-[0_12px_35px_-28px_rgba(30,64,175,0.65)] transition-[border-color,background-color,transform,box-shadow] hover:-translate-y-0.5 hover:border-blue-400/35 hover:bg-white/60 hover:shadow-[0_18px_42px_-28px_rgba(30,64,175,0.7)] motion-reduce:transform-none">
      <div className="flex items-start gap-3">
        <div className="grid size-10 shrink-0 place-items-center rounded-xl border border-border/70 bg-white/70 text-muted-foreground shadow-sm">
          <TransportIcon className="size-[18px]" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-sm font-semibold text-foreground">{mcp.name || mcp.id}</h2>
            <AdminStatusBadge>{transport === "http" ? "HTTP" : transport}</AdminStatusBadge>
          </div>
          <p className="mt-1 line-clamp-2 text-sm leading-5 text-muted-foreground">
            {mcp.description || "No description"}
          </p>
        </div>
        <AdminStatusBadge tone={mcp.enabled ? "green" : "neutral"} dot>
          {mcp.enabled ? "Enabled" : "Disabled"}
        </AdminStatusBadge>
      </div>

      <div className="mt-4 rounded-xl border border-border/60 bg-slate-50/65 px-3 py-2.5">
        <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
          {remote ? "URL" : "Command"}
        </div>
        <code className="mt-1 block truncate font-mono text-xs tabular-nums text-foreground/80">
          {endpoint || (remote ? "Remote URL saved" : "Command saved")}
        </code>
      </div>

      <div className="mt-auto flex flex-wrap items-center gap-2 pt-4">
        <span className="inline-flex items-center gap-1.5 text-xs text-blue-700">
          <CircleCheck className="size-3.5" />
          Configured
        </span>
        {mcp.default_enabled ? (
          <span className="text-xs text-muted-foreground">· Default on</span>
        ) : null}
        <div className="ml-auto flex items-center gap-1">
          <Button
            variant="outline"
            size="sm"
            className="h-8 gap-1.5"
            disabled={busy}
            onClick={onEdit}
          >
            <Pencil className="size-4" />
            Edit
          </Button>
          <AdminIconButton
            disabled={busy}
            aria-label={mcp.enabled ? `Disable ${mcp.name}` : `Enable ${mcp.name}`}
            title={mcp.enabled ? "Disable" : "Enable"}
            onClick={onToggle}
          >
            {busy ? (
              <LoaderCircle className="size-4 animate-spin" />
            ) : mcp.enabled ? (
              <PowerOff className="size-4" />
            ) : (
              <Power className="size-4" />
            )}
          </AdminIconButton>
          <AdminIconButton
            disabled={busy}
            aria-label={`Delete ${mcp.name}`}
            title="Delete"
            className="hover:bg-destructive/10 hover:text-destructive"
            onClick={onDelete}
          >
            <Trash2 className="size-4" />
          </AdminIconButton>
        </div>
      </div>
    </article>
  );
}

function Field({
  label,
  optional = false,
  hint,
  children,
}: {
  label: string;
  optional?: boolean;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="grid gap-1.5 text-sm">
      <span className="flex items-baseline justify-between gap-3 text-xs font-medium text-muted-foreground">
        <span>
          {label}
          {optional ? <span className="font-normal"> · optional</span> : null}
        </span>
        {hint ? <span className="text-right font-normal">{hint}</span> : null}
      </span>
      {children}
    </label>
  );
}

function SecretPairsField({
  label,
  value,
  placeholder,
  savedHints,
  clearSaved,
  onChange,
  onClearSaved,
}: {
  label: string;
  value: string;
  placeholder: string;
  savedHints?: Record<string, string>;
  clearSaved: boolean;
  onChange: (value: string) => void;
  onClearSaved: (clear: boolean) => void;
}) {
  const savedKeys = Object.keys(savedHints ?? {});
  return (
    <Field label={label} optional hint="One KEY=value pair per line.">
      <textarea
        className={textAreaClass}
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
      />
      {savedKeys.length ? (
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>
            {clearSaved
              ? "Saved values will be cleared."
              : `Saved: ${savedKeys.join(", ")}. Blank keeps them.`}
          </span>
          <button
            type="button"
            className="font-medium text-foreground underline-offset-4 hover:underline"
            onClick={() => onClearSaved(!clearSaved)}
          >
            {clearSaved ? "Keep saved values" : "Clear saved values"}
          </button>
        </div>
      ) : null}
    </Field>
  );
}

function normalizeTransport(value: string): FormState["transport"] {
  const normalized = value.trim().toLowerCase().replace(/[_-]/g, "");
  if (normalized === "http" || normalized === "streamablehttp") return "http";
  if (normalized === "sse") return "sse";
  return "stdio";
}

function slugify(raw: string) {
  return raw
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function splitArgs(raw: string) {
  return raw
    .split(/\n+/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function parsePairs(raw: string, field: string): Record<string, string> | undefined {
  const result: Record<string, string> = {};
  const lines = raw.split("\n");
  for (const [index, rawLine] of lines.entries()) {
    const line = rawLine.trim();
    if (!line) continue;
    const separator = line.indexOf("=");
    if (separator <= 0) throw new Error(`${field} line ${index + 1} must use KEY=value.`);
    const key = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_.-]*$/.test(key)) {
      throw new Error(`${field} line ${index + 1} has an invalid key.`);
    }
    if (!value) throw new Error(`${field} line ${index + 1} has an empty value.`);
    if (Object.hasOwn(result, key)) throw new Error(`${field} contains duplicate key ${key}.`);
    result[key] = value;
  }
  return Object.keys(result).length ? result : undefined;
}

async function readError(res: Response) {
  const text = await res.text();
  try {
    const json = JSON.parse(text);
    if (typeof json.error === "string") return json.error;
    if (json.error && typeof json.error === "object") {
      return json.error.message || json.error.code || text;
    }
    return json.message || text;
  } catch {
    return text || res.statusText;
  }
}
