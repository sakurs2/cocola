"use client";

import { Plug as McpPageIcon } from "lucide-react";
import {
  useCallback,
  useEffect,
  useState,
  type CSSProperties,
  type ComponentType,
  type ReactNode,
} from "react";
import {
  Blocks,
  Boxes,
  ChevronDown,
  CircleAlert,
  CircleCheck,
  Cloud,
  Cpu,
  Eye,
  EyeOff,
  LoaderCircle,
  Network,
  Pencil,
  Plug2,
  Plus,
  Power,
  PowerOff,
  Radio,
  Save,
  Server,
  Terminal,
  Trash2,
  Waypoints,
} from "lucide-react";
import { SelectControl } from "@/components/ui/select-control";
import {
  AdminAlert,
  AdminConfirmDialog,
  AdminDrawer,
  AdminEmptyState,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
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

const GLYPH_ICONS: ComponentType<{ className?: string }>[] = [
  Server,
  Network,
  Cloud,
  Cpu,
  Boxes,
  Blocks,
  Radio,
  Waypoints,
  Plug2,
  Terminal,
];
const GLYPH_TONES: { ink: string; soft: string; ring: string }[] = [
  { ink: "#ea580c", soft: "#ffedd5", ring: "#fdba74" }, // orange
  { ink: "#d97706", soft: "#fef3c7", ring: "#fcd34d" }, // amber
  { ink: "#0d9488", soft: "#ccfbf1", ring: "#5eead4" }, // teal
  { ink: "#2563eb", soft: "#dbeafe", ring: "#93c5fd" }, // blue
  { ink: "#7c3aed", soft: "#ede9fe", ring: "#c4b5fd" }, // violet
  { ink: "#db2777", soft: "#fce7f3", ring: "#f9a8d4" }, // pink
  { ink: "#0891b2", soft: "#cffafe", ring: "#67e8f9" }, // cyan
  { ink: "#16a34a", soft: "#dcfce7", ring: "#86efac" }, // green
  { ink: "#c026d3", soft: "#fae8ff", ring: "#f0abfc" }, // fuchsia
  { ink: "#4f46e5", soft: "#e0e7ff", ring: "#a5b4fc" }, // indigo
];

function hashString(value: string): number {
  let hash = 0;
  for (let i = 0; i < value.length; i += 1) {
    hash = (hash * 31 + value.charCodeAt(i)) | 0;
  }
  return Math.abs(hash);
}

function glyphFor(id: string) {
  const h = hashString(id || "mcp");
  const Icon = GLYPH_ICONS[h % GLYPH_ICONS.length] ?? Server;
  const tone =
    GLYPH_TONES[h % GLYPH_TONES.length] ??
    ({ ink: "#ea580c", soft: "#ffedd5", ring: "#fdba74" } as const);
  const style = {
    "--glyph-ink": tone.ink,
    "--glyph-soft": tone.soft,
    "--glyph-ring": tone.ring,
  } as CSSProperties;
  return { Icon, style };
}

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
    <AdminPage className="admin-theme-orange">
      <AdminPageHeader
        icon={<McpPageIcon className="size-[18px]" />}
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

      {loading && !mcps.length ? (
        <div className="flex min-h-48 items-center justify-center text-sm text-muted-foreground">
          <LoaderCircle className="mr-2 size-4 animate-spin" />
          Loading MCP servers
        </div>
      ) : mcps.length ? (
        <div className="admin-entity-grid md:grid-cols-2 xl:grid-cols-3">
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
        <div className="admin-entity-card items-center py-12 text-center">
          <AdminEmptyState
            icon={<McpPageIcon className="size-6" />}
            title="No MCP servers configured"
            description="Add a server now; Cocola checks the connection when an agent first uses it."
            action={
              <Button className="gap-2" onClick={openCreate}>
                <Plus className="size-4" />
                Add server
              </Button>
            }
          />
        </div>
      )}

      <AdminDrawer
        open={drawerOpen}
        onOpenChange={(open) => {
          if (!saving) setDrawerOpen(open);
        }}
        className="admin-theme-orange"
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
              <SelectControl
                className={controlClass}
                value={form.transport}
                onValueChange={(value) =>
                  setForm({ ...form, transport: value as FormState["transport"] })
                }
                options={[
                  { value: "stdio", label: "stdio · Command" },
                  { value: "http", label: "HTTP · URL" },
                  { value: "sse", label: "SSE · URL" },
                ]}
                contentClassName="cocola-admin-ui"
              />
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
        className="admin-theme-orange"
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
  const { Icon, style } = glyphFor(mcp.id);
  return (
    <div className="admin-entity-card admin-entity-card--hover">
      <div className="flex items-start gap-3">
        <div className="admin-entity-glyph" style={style}>
          <Icon className="size-[18px]" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-sm font-semibold text-foreground">{mcp.name || mcp.id}</h2>
            <span className="admin-entity-tag">{transport === "http" ? "HTTP" : transport}</span>
          </div>
          <p className="mt-1 line-clamp-2 min-h-10 text-sm leading-5 text-muted-foreground">
            {mcp.description || "No description"}
          </p>
        </div>
        {mcp.enabled ? (
          <span className="admin-chip admin-chip--ok">
            <CircleCheck className="size-3.5" />
            Enabled
          </span>
        ) : (
          <span className="admin-chip admin-chip--off">
            <span className="admin-chip-dot" />
            Disabled
          </span>
        )}
      </div>

      <div className="admin-entity-mono mt-4">
        <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
          {remote ? "URL" : "Command"}
        </div>
        <code className="mt-1 block truncate font-mono text-xs tabular-nums text-foreground/80">
          {endpoint || (remote ? "Remote URL saved" : "Command saved")}
        </code>
      </div>

      {mcp.default_enabled ? (
        <div className="mt-3 text-xs text-muted-foreground">Default on</div>
      ) : null}
      <div className="mt-auto flex items-center gap-2 pt-4">
        <button className="admin-card-btn flex-1 justify-center" disabled={busy} onClick={onEdit}>
          <Pencil className="size-4" />
          Edit
        </button>
        <button className="admin-card-btn flex-1 justify-center" disabled={busy} onClick={onToggle}>
          {busy ? (
            <LoaderCircle className="size-4 animate-spin" />
          ) : mcp.enabled ? (
            <PowerOff className="size-4" />
          ) : (
            <Power className="size-4" />
          )}
          {mcp.enabled ? "Disable" : "Enable"}
        </button>
        <button
          className="admin-card-btn admin-card-btn--danger flex-1 justify-center"
          disabled={busy}
          onClick={onDelete}
        >
          <Trash2 className="size-4" />
          Remove
        </button>
      </div>
    </div>
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
