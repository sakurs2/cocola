"use client";

import { PlugsConnected as McpPageIcon } from "@phosphor-icons/react";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  ChevronDown,
  LoaderCircle,
  PlugZap,
  Save,
  ToggleLeft,
  ToggleRight,
  Trash2,
} from "lucide-react";
import Link from "next/link";
import { AdminPageHeader } from "@/components/admin/admin-ui";

type MCPServer = {
  id: string;
  name: string;
  description: string;
  transport: "stdio" | "http" | "sse" | string;
  command?: string;
  args?: string[];
  url?: string;
  url_var_hints?: Record<string, string>;
  env_hints?: Record<string, string>;
  header_hints?: Record<string, string>;
  enabled: boolean;
  default_enabled: boolean;
  source: string;
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
  url_vars: string;
  env: string;
  headers: string;
  enabled: boolean;
  default_enabled: boolean;
};

const EMPTY_FORM: FormState = {
  id: "",
  name: "",
  description: "",
  transport: "stdio",
  command: "",
  args: "",
  url: "",
  url_vars: "",
  env: "",
  headers: "",
  enabled: true,
  default_enabled: false,
};

const input =
  "h-9 min-w-0 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-1 focus:ring-ring";
const textArea =
  "min-h-20 min-w-0 rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-1 focus:ring-ring";
const btn =
  "inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50";
const primaryBtn =
  "inline-flex h-9 items-center justify-center gap-2 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50";
const iconBtn =
  "inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-40";

export default function AdminMCPPage() {
  const [mcps, setMcps] = useState<MCPServer[]>([]);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [editing, setEditing] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [advancedOpen, setAdvancedOpen] = useState(false);

  const stats = useMemo(
    () => ({
      total: mcps.length,
      enabled: mcps.filter((mcp) => mcp.enabled).length,
      defaults: mcps.filter((mcp) => mcp.enabled && mcp.default_enabled).length,
    }),
    [mcps],
  );

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

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      const id = editing || slugify(form.id || form.name);
      const body: Record<string, unknown> = {
        id,
        name: form.name,
        description: form.description,
        transport: form.transport,
        command: form.transport === "stdio" ? form.command : "",
        args: form.transport === "stdio" ? splitArgs(form.args) : [],
        url: form.transport === "stdio" ? "" : form.url,
        enabled: form.enabled,
        default_enabled: form.default_enabled,
      };
      const env = parsePairs(form.env);
      const urlVars = parsePairs(form.url_vars);
      const headers = parsePairs(form.headers);
      if (Object.keys(env).length) body.env = env;
      if (Object.keys(urlVars).length) body.url_vars = urlVars;
      if (Object.keys(headers).length) body.headers = headers;
      const url = editing ? `/api/admin/mcps/${encodeURIComponent(editing)}` : "/api/admin/mcps";
      const res = await fetch(url, {
        method: editing ? "PATCH" : "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(await readError(res));
      setForm(EMPTY_FORM);
      setEditing(null);
      setAdvancedOpen(false);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const edit = (mcp: MCPServer) => {
    setEditing(mcp.id);
    setForm({
      id: mcp.id,
      name: mcp.name,
      description: mcp.description,
      transport: mcp.transport === "http" || mcp.transport === "sse" ? mcp.transport : "stdio",
      command: mcp.command || "",
      args: (mcp.args || []).join("\n"),
      url: mcp.url || "",
      url_vars: "",
      env: "",
      headers: "",
      enabled: mcp.enabled,
      default_enabled: mcp.default_enabled,
    });
    setAdvancedOpen(false);
  };

  const toggle = async (mcp: MCPServer) => {
    await mutate(
      `/api/admin/mcps/${encodeURIComponent(mcp.id)}/${mcp.enabled ? "disable" : "enable"}`,
      "POST",
    );
  };

  const remove = async (id: string) => {
    if (!confirm(`Delete MCP ${id}?`)) return;
    await mutate(`/api/admin/mcps/${encodeURIComponent(id)}`, "DELETE");
  };

  const mutate = async (url: string, method: string) => {
    setSaving(true);
    setError("");
    try {
      const res = await fetch(url, { method });
      if (!res.ok) throw new Error(await readError(res));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <main className="mx-auto max-w-6xl space-y-6 px-6 py-6">
      <AdminPageHeader
        icon={<McpPageIcon className="size-[18px]" weight="duotone" />}
        title="MCP"
        description="Publish Model Context Protocol servers that users can enable for their agent sessions."
        actions={
          <div className="grid grid-cols-3 overflow-hidden rounded-md border border-border text-center text-xs">
            <Stat label="Total" value={stats.total} />
            <Stat label="Enabled" value={stats.enabled} />
            <Stat label="Default" value={stats.defaults} />
          </div>
        }
      />

      {error ? (
        <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600">
          {error}
        </div>
      ) : null}

      <section className="rounded-lg border border-border bg-card p-4">
        <div className="mb-4 flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">
            {editing ? `Edit ${editing}` : "New MCP server"}
          </h2>
          {editing ? (
            <button
              type="button"
              className={btn}
              onClick={() => {
                setEditing(null);
                setForm(EMPTY_FORM);
                setAdvancedOpen(false);
              }}
            >
              Cancel
            </button>
          ) : null}
        </div>
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="Name">
            <input
              className={input}
              value={form.name}
              placeholder="Amap Maps"
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </Field>
          <Field label="Transport">
            <select
              className={input}
              value={form.transport}
              onChange={(e) =>
                setForm({ ...form, transport: e.target.value as FormState["transport"] })
              }
            >
              <option value="stdio">stdio</option>
              <option value="http">http</option>
              <option value="sse">sse</option>
            </select>
          </Field>
          <Field label="Description" optional>
            <input
              className={input}
              value={form.description}
              placeholder="Maps, geocoding, and route planning"
              onChange={(e) => setForm({ ...form, description: e.target.value })}
            />
          </Field>
          {form.transport === "stdio" ? (
            <>
              <Field label="Command">
                <input
                  className={input}
                  value={form.command}
                  placeholder="npx"
                  onChange={(e) => setForm({ ...form, command: e.target.value })}
                />
              </Field>
              <Field label="Args" optional>
                <textarea
                  className={textArea}
                  value={form.args}
                  placeholder="-y&#10;@modelcontextprotocol/server-github"
                  onChange={(e) => setForm({ ...form, args: e.target.value })}
                />
              </Field>
            </>
          ) : (
            <Field label="URL">
              <input
                className={input}
                value={form.url}
                placeholder="https://mcp.amap.com/mcp?key=${AMAP_KEY}"
                onChange={(e) => setForm({ ...form, url: e.target.value })}
              />
            </Field>
          )}
        </div>
        <div className="mt-4">
          <button
            type="button"
            className="inline-flex h-8 items-center gap-2 rounded-md px-2 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            onClick={() => setAdvancedOpen((open) => !open)}
          >
            <ChevronDown
              className={`size-4 transition-transform ${advancedOpen ? "rotate-180" : ""}`}
            />
            Advanced
          </button>
          {advancedOpen ? (
            <div className="mt-3 grid gap-3 md:grid-cols-2">
              {!editing ? (
                <Field label="ID" optional>
                  <input
                    className={input}
                    value={form.id}
                    placeholder={slugify(form.name) || "amap-maps"}
                    onChange={(e) => setForm({ ...form, id: e.target.value })}
                  />
                </Field>
              ) : null}
              {form.transport === "stdio" ? (
                <Field label="Env" optional>
                  <textarea
                    className={textArea}
                    value={form.env}
                    placeholder="GITHUB_TOKEN=..."
                    onChange={(e) => setForm({ ...form, env: e.target.value })}
                  />
                </Field>
              ) : (
                <>
                  <Field label="URL Variables" optional>
                    <textarea
                      className={textArea}
                      value={form.url_vars}
                      placeholder="AMAP_KEY=..."
                      onChange={(e) => setForm({ ...form, url_vars: e.target.value })}
                    />
                  </Field>
                  <Field label="Headers" optional>
                    <textarea
                      className={textArea}
                      value={form.headers}
                      placeholder="Authorization=Bearer ..."
                      onChange={(e) => setForm({ ...form, headers: e.target.value })}
                    />
                  </Field>
                </>
              )}
              {editing ? (
                <div className="self-end text-xs text-muted-foreground">
                  Leave secret fields empty to keep the saved values unchanged.
                </div>
              ) : null}
            </div>
          ) : null}
        </div>
        <div className="mt-4 flex flex-wrap items-center gap-4 text-sm">
          <label className="inline-flex items-center gap-2">
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
            />
            Enabled
          </label>
          <label className="inline-flex items-center gap-2">
            <input
              type="checkbox"
              checked={form.default_enabled}
              onChange={(e) => setForm({ ...form, default_enabled: e.target.checked })}
            />
            Default enabled for users
          </label>
          <button
            type="button"
            className={primaryBtn}
            disabled={saving}
            onClick={() => void save()}
          >
            {saving ? (
              <LoaderCircle className="size-4 animate-spin" />
            ) : (
              <Save className="size-4" />
            )}
            Save
          </button>
        </div>
        <p className="mt-3 text-xs text-muted-foreground">
          For URL keys, use placeholders such as ${"${AMAP_KEY}"} in the URL and save the real value
          in Advanced URL Variables. Env, headers, and URL variables are encrypted.
        </p>
      </section>

      <section className="grid gap-3 md:grid-cols-2">
        {loading ? (
          <div className="col-span-full flex h-32 items-center justify-center text-muted-foreground">
            <LoaderCircle className="mr-2 size-4 animate-spin" />
            Loading MCP servers
          </div>
        ) : mcps.length ? (
          mcps.map((mcp) => (
            <article key={mcp.id} className="rounded-lg border border-border bg-card p-4">
              <div className="flex items-start gap-3">
                <div className="grid size-10 shrink-0 place-items-center rounded-md bg-muted">
                  <PlugZap className="size-5 text-muted-foreground" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <Link
                      href={`/admin/mcps/${encodeURIComponent(mcp.id)}`}
                      className="truncate text-sm font-semibold hover:underline"
                    >
                      {mcp.name || mcp.id}
                    </Link>
                    <span className="rounded-md border border-border px-2 py-0.5 text-xs text-muted-foreground">
                      {mcp.transport}
                    </span>
                  </div>
                  <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                    {mcp.description || "No description"}
                  </p>
                  <div className="mt-3 space-y-1 text-xs text-muted-foreground">
                    <div className="truncate">
                      {mcp.transport === "stdio" ? mcp.command : mcp.url}
                    </div>
                    <SecretHints title="URL vars" hints={mcp.url_var_hints} />
                    <SecretHints title="Env" hints={mcp.env_hints} />
                    <SecretHints title="Headers" hints={mcp.header_hints} />
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <button className={iconBtn} title="Toggle" onClick={() => void toggle(mcp)}>
                    {mcp.enabled ? (
                      <ToggleRight className="size-4" />
                    ) : (
                      <ToggleLeft className="size-4" />
                    )}
                  </button>
                  <button className={iconBtn} title="Edit" onClick={() => edit(mcp)}>
                    <Save className="size-4" />
                  </button>
                  <button className={iconBtn} title="Delete" onClick={() => void remove(mcp.id)}>
                    <Trash2 className="size-4" />
                  </button>
                </div>
              </div>
              <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted-foreground">
                <span className="rounded-md border border-border px-2 py-0.5">
                  {mcp.enabled ? "enabled" : "disabled"}
                </span>
                <span className="rounded-md border border-border px-2 py-0.5">
                  {mcp.default_enabled ? "default on" : "default off"}
                </span>
              </div>
            </article>
          ))
        ) : (
          <div className="col-span-full rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
            No MCP servers configured.
          </div>
        )}
      </section>
    </main>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="min-w-20 border-r border-border px-3 py-2 last:border-r-0">
      <div className="text-base font-semibold">{value}</div>
      <div className="text-muted-foreground">{label}</div>
    </div>
  );
}

function Field({
  label,
  optional = false,
  children,
}: {
  label: string;
  optional?: boolean;
  children: ReactNode;
}) {
  return (
    <label className="grid gap-1.5 text-sm">
      <span className="text-xs font-medium text-muted-foreground">
        {label}
        {optional ? <span className="font-normal"> optional</span> : null}
      </span>
      {children}
    </label>
  );
}

function SecretHints({ title, hints }: { title: string; hints?: Record<string, string> }) {
  const entries = Object.entries(hints || {});
  if (!entries.length) return null;
  return (
    <div className="truncate">
      {title}: {entries.map(([k, v]) => `${k}=${v}`).join(", ")}
    </div>
  );
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

function parsePairs(raw: string) {
  const out: Record<string, string> = {};
  for (const line of raw.split(/\n+/)) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const idx = trimmed.indexOf("=");
    if (idx <= 0) continue;
    out[trimmed.slice(0, idx).trim()] = trimmed.slice(idx + 1).trim();
  }
  return out;
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
