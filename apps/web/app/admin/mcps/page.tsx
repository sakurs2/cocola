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
  Radio,
  Save,
  Server,
  Terminal,
  Trash2,
  Waypoints,
} from "lucide-react";
import {
  Button,
  Card,
  Checkbox,
  Chip,
  Input,
  Label,
  SearchField,
  Switch,
  TextArea,
  TextField,
} from "@heroui/react";
import { SelectControl } from "@/components/ui/select-control";
import {
  AdminAlert,
  AdminConfirmDialog,
  AdminDrawer,
  AdminEmptyState,
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
} from "@/components/admin/admin-ui";

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
  const [query, setQuery] = useState("");

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

  const visibleMcps = mcps.filter((mcp) =>
    `${mcp.name} ${mcp.description} ${mcp.transport}`
      .toLowerCase()
      .includes(query.trim().toLowerCase()),
  );

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
            <Button className="gap-2" onPress={openCreate}>
              <Plus className="size-4" />
              Add server
            </Button>
          </>
        }
      />

      <AdminErrorDialog
        error={drawerOpen ? null : error}
        title="MCP server operation failed"
        onDismiss={() => setError("")}
        onRetry={() => void load()}
      />
      {notice ? (
        <AdminAlert tone="success" icon={<CircleCheck className="size-4" />}>
          <span aria-live="polite">{notice}</span>
        </AdminAlert>
      ) : null}

      <SearchField
        aria-label="Search MCP servers"
        className="w-full sm:w-[320px]"
        value={query}
        onChange={setQuery}
      >
        <SearchField.Group>
          <SearchField.SearchIcon />
          <SearchField.Input placeholder="Search MCP servers" />
          <SearchField.ClearButton />
        </SearchField.Group>
      </SearchField>

      {loading && !mcps.length ? (
        <div className="flex min-h-48 items-center justify-center text-sm text-muted">
          <LoaderCircle className="mr-2 size-4 animate-spin" />
          Loading MCP servers
        </div>
      ) : visibleMcps.length ? (
        <div className="grid items-stretch gap-4 md:grid-cols-2 xl:grid-cols-3">
          {visibleMcps.map((mcp) => (
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
          icon={<McpPageIcon className="size-6" />}
          title={mcps.length ? "No matching MCP servers" : "No MCP servers configured"}
          description={
            mcps.length
              ? "Try another search or clear the active query."
              : "Add a server now; Cocola checks the connection when an agent first uses it."
          }
          action={
            !mcps.length ? (
              <Button className="gap-2" onPress={openCreate}>
                <Plus className="size-4" />
                Add server
              </Button>
            ) : undefined
          }
        />
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
            <Button variant="outline" isDisabled={saving} onPress={() => setDrawerOpen(false)}>
              Cancel
            </Button>
            <Button isDisabled={saving} className="min-w-32 gap-2" onPress={() => void save()}>
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
            <TextField
              value={form.name}
              variant="secondary"
              onChange={(name) => setForm({ ...form, name })}
            >
              <Label>Name</Label>
              <Input autoFocus placeholder="GitHub" />
            </TextField>
            <Field label="Transport">
              <SelectControl
                className="w-full"
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

          <p className="-mt-3 text-xs leading-5 text-muted">
            {form.transport === "stdio"
              ? "stdio starts a local process, so it uses Command instead of URL."
              : `${form.transport === "http" ? "HTTP" : "SSE"} connects to a remote URL.`}
          </p>

          <TextField
            value={form.description}
            variant="secondary"
            onChange={(description) => setForm({ ...form, description })}
          >
            <Label>
              Description <span className="text-muted font-normal">· optional</span>
            </Label>
            <Input placeholder="Repository tools for agent sessions" />
          </TextField>

          {form.transport === "stdio" ? (
            <>
              <TextField
                value={form.command}
                variant="secondary"
                onChange={(command) => setForm({ ...form, command })}
              >
                <Label>Command</Label>
                <Input className="font-mono" placeholder="npx" />
              </TextField>
              <TextField
                value={form.args}
                variant="secondary"
                onChange={(args) => setForm({ ...form, args })}
              >
                <Label>
                  Arguments{" "}
                  <span className="text-muted font-normal">· optional · one per line</span>
                </Label>
                <TextArea
                  className="min-h-24 font-mono"
                  placeholder={"-y\n@modelcontextprotocol/server-github"}
                />
              </TextField>
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
                <TextField
                  className="relative"
                  value={form.url}
                  variant="secondary"
                  onChange={(url) => setForm({ ...form, url })}
                >
                  <Label className="sr-only">URL</Label>
                  <Input
                    type={showURL ? "text" : "password"}
                    className="w-full pr-11 font-mono"
                    placeholder={
                      editing
                        ? editing.url_hint || "Saved URL"
                        : "https://mcp.example.com/api?token=..."
                    }
                    autoComplete="off"
                  />
                  <Button
                    isIconOnly
                    aria-label={showURL ? "Hide URL" : "Show URL"}
                    className="absolute bottom-1 right-1 size-9 min-w-9"
                    size="sm"
                    variant="ghost"
                    onPress={() => setShowURL((show) => !show)}
                  >
                    {showURL ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                  </Button>
                </TextField>
              </Field>
              <p className="-mt-3 text-xs leading-5 text-muted">
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

          <Card className="p-4">
            <Checkbox
              isSelected={form.defaultEnabled}
              onChange={(defaultEnabled) => setForm({ ...form, defaultEnabled })}
            >
              <span>
                <span className="block text-sm font-medium">Enabled for users by default</span>
                <span className="text-muted mt-0.5 block text-xs leading-5">
                  Users can still turn this server off for their own agent sessions.
                </span>
              </span>
            </Checkbox>
          </Card>

          {!editing ? (
            <div>
              <Button
                className="gap-2"
                size="sm"
                variant="ghost"
                onPress={() => setAdvancedOpen((open) => !open)}
              >
                <ChevronDown
                  className={`size-4 transition-transform ${advancedOpen ? "rotate-180" : ""}`}
                />
                Advanced
              </Button>
              {advancedOpen ? (
                <div className="mt-3">
                  <TextField
                    value={form.id}
                    variant="secondary"
                    onChange={(id) => setForm({ ...form, id })}
                  >
                    <Label>
                      ID <span className="text-muted font-normal">· optional</span>
                    </Label>
                    <Input className="font-mono" placeholder={slugify(form.name) || "github"} />
                    <span className="text-muted text-xs">
                      Generated from the name when left blank.
                    </span>
                  </TextField>
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
    <Card className="admin-mcp-card h-full p-5">
      <Card.Content className="flex h-full min-w-0 flex-col p-0">
        <div className="flex items-start gap-3">
          <span
            className="admin-mcp-card-icon flex size-10 shrink-0 items-center justify-center rounded-2xl"
            style={{ ...style, background: "var(--glyph-soft)", color: "var(--glyph-ink)" }}
          >
            <Icon className="size-[18px]" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="truncate text-sm font-semibold text-foreground">
                {mcp.name || mcp.id}
              </h2>
              <Chip size="sm" variant="soft">
                {transport === "http" ? "HTTP" : transport}
              </Chip>
            </div>
            <p className="text-muted mt-1 line-clamp-2 min-h-10 text-sm leading-5">
              {mcp.description || "No description"}
            </p>
          </div>
        </div>

        <div className="bg-surface-secondary mt-4 min-w-0 rounded-2xl p-3">
          <div className="text-muted text-[10px] font-semibold uppercase tracking-[0.12em]">
            {remote ? "URL" : "Command"}
          </div>
          <code className="mt-1 block truncate font-mono text-xs tabular-nums text-foreground/80">
            {endpoint || (remote ? "Remote URL saved" : "Command saved")}
          </code>
        </div>

        {mcp.default_enabled ? <div className="text-muted mt-3 text-xs">Default on</div> : null}
        <div className="mt-auto flex items-center justify-between gap-3 pt-4">
          <Button isDisabled={busy} size="sm" variant="outline" onPress={onEdit}>
            <Pencil className="size-4" />
            Edit
          </Button>
          <span className="flex items-center gap-2">
            <Switch
              aria-label={`${mcp.enabled ? "Disable" : "Enable"} ${mcp.name || mcp.id}`}
              isDisabled={busy}
              isSelected={mcp.enabled}
              onChange={onToggle}
            >
              <Switch.Content>
                <Switch.Control>
                  <Switch.Thumb className="admin-switch-thumb shadow-sm" />
                </Switch.Control>
              </Switch.Content>
            </Switch>
            <Button isDisabled={busy} size="sm" variant="danger-soft" onPress={onDelete}>
              <Trash2 className="size-4" />
              Remove
            </Button>
          </span>
        </div>
      </Card.Content>
    </Card>
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
      <span className="flex items-baseline justify-between gap-3 text-xs font-medium text-muted">
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
      <TextField value={value} variant="secondary" onChange={onChange}>
        <Label className="sr-only">{label}</Label>
        <TextArea className="min-h-24 font-mono" placeholder={placeholder} />
      </TextField>
      {savedKeys.length ? (
        <div className="text-muted flex flex-wrap items-center justify-between gap-2 text-xs">
          <span>
            {clearSaved
              ? "Saved values will be cleared."
              : `Saved: ${savedKeys.join(", ")}. Blank keeps them.`}
          </span>
          <Button
            className="h-8 min-h-8 px-2"
            size="sm"
            variant="ghost"
            onPress={() => onClearSaved(!clearSaved)}
          >
            {clearSaved ? "Keep saved values" : "Clear saved values"}
          </Button>
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
