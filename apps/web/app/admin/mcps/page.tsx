"use client";

import { Plug as McpPageIcon } from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  ChevronDown,
  CircleAlert,
  Eye,
  EyeOff,
  KeyRound,
  LoaderCircle,
  Pencil,
  Plus,
  Save,
  Search as SearchIcon,
  Trash2,
} from "lucide-react";
import {
  Button,
  Card,
  Checkbox,
  Chip,
  Input,
  Label,
  Modal,
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
  AdminToast,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";
import { MCPServerIcon } from "@/components/ui/mcp-server-icon";
import { useTranslations } from "next-intl";

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

const TAVILY_MCP_URL = "https://mcp.tavily.com/mcp/";
const TAVILY_DEFAULT_PARAMETERS = JSON.stringify({
  search_depth: "basic",
  max_results: 5,
  include_images: false,
  include_raw_content: false,
});

export default function AdminMCPPage() {
  const t = useTranslations("admin.mcpsPage");
  const [mcps, setMcps] = useState<MCPServer[]>([]);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [editing, setEditing] = useState<MCPServer | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [tavilyModalOpen, setTavilyModalOpen] = useState(false);
  const [tavilyEditing, setTavilyEditing] = useState<MCPServer | null>(null);
  const [tavilyAPIKey, setTavilyAPIKey] = useState("");
  const [tavilyDefaultEnabled, setTavilyDefaultEnabled] = useState(true);
  const [tavilyError, setTavilyError] = useState("");
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
      url: transport === "stdio" ? "" : (mcp.url_hint ?? ""),
      defaultEnabled: mcp.default_enabled,
    });
    setAdvancedOpen(false);
    setShowURL(transport !== "stdio");
    setError("");
    setDrawerOpen(true);
  };

  const openTavily = () => {
    const existing = mcps.find((mcp) => mcp.id === "tavily") ?? null;
    setTavilyEditing(existing);
    setTavilyAPIKey("");
    setTavilyDefaultEnabled(existing?.default_enabled ?? true);
    setTavilyError("");
    setTavilyModalOpen(true);
  };

  const saveTavily = async () => {
    const apiKey = tavilyAPIKey.trim();
    if (!apiKey && !tavilyEditing) {
      setTavilyError(t("tavily.keyRequired"));
      return;
    }
    setSaving(true);
    setTavilyError("");
    setNotice("");
    try {
      const body: Record<string, unknown> = {
        id: "tavily",
        name: "Tavily",
        description: t("tavily.serverDescription"),
        transport: "http",
        default_enabled: tavilyDefaultEnabled,
      };
      if (!tavilyEditing) body.url = TAVILY_MCP_URL;
      if (apiKey) {
        body.headers = {
          Authorization: `Bearer ${apiKey}`,
          DEFAULT_PARAMETERS: TAVILY_DEFAULT_PARAMETERS,
        };
      }
      const response = await fetch(tavilyEditing ? "/api/admin/mcps/tavily" : "/api/admin/mcps", {
        method: tavilyEditing ? "PATCH" : "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!response.ok) throw new Error(await readError(response));
      setTavilyAPIKey("");
      setTavilyEditing(null);
      setTavilyModalOpen(false);
      setNotice(t("savedNotice", { name: "Tavily" }));
      await load();
    } catch (err) {
      setTavilyError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const save = async () => {
    setError("");
    setNotice("");
    const name = form.name.trim();
    const id = editing?.id || slugify(form.id || name);
    if (!name || !id) {
      setError(t("validation.name"));
      return;
    }
    if (form.transport === "stdio" && !form.command.trim()) {
      setError(t("validation.command"));
      return;
    }
    const remoteURL = form.url.trim();
    const savedURLHint = editing?.url_hint?.trim() ?? "";
    const remoteURLUnchanged = Boolean(editing && savedURLHint && remoteURL === savedURLHint);
    const keepsRemoteURL =
      editing &&
      normalizeTransport(editing.transport) !== "stdio" &&
      (!remoteURL || remoteURLUnchanged);
    if (form.transport !== "stdio" && !remoteURL && !keepsRemoteURL) {
      setError(t("validation.url"));
      return;
    }

    let env: Record<string, string> | undefined;
    let headers: Record<string, string> | undefined;
    try {
      env = parsePairs(form.env, "Env", (kind, values) => t(`validation.${kind}`, values));
      headers = parsePairs(form.headers, "Headers", (kind, values) =>
        t(`validation.${kind}`, values),
      );
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
        if (remoteURL && !remoteURLUnchanged) body.url = remoteURL;
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
      setNotice(t("savedNotice", { name: result.name }));
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

  const normalizedQuery = query.trim().toLowerCase();
  const visibleMcps = mcps
    .filter((mcp) =>
      `${mcp.name} ${mcp.description} ${mcp.transport}`.toLowerCase().includes(normalizedQuery),
    )
    .sort((a, b) => Number(b.id === "tavily") - Number(a.id === "tavily"));
  const tavilyConfigured = mcps.some((mcp) => mcp.id === "tavily");
  const tavilyPresetVisible =
    !tavilyConfigured &&
    `tavily ${t("tavily.serverDescription")} http`.toLowerCase().includes(normalizedQuery);
  const addControl = (
    <Button className="gap-2" onPress={openCreate}>
      <Plus className="size-4" />
      {t("add")}
    </Button>
  );

  return (
    <AdminPage className="admin-theme-orange">
      <AdminPageHeader
        icon={<McpPageIcon className="size-[18px]" />}
        title={t("title")}
        actions={
          <>
            <AdminRefreshButton refreshing={loading} onClick={() => void load()} variant="outline">
              {t("refresh")}
            </AdminRefreshButton>
            {addControl}
          </>
        }
      />

      <AdminErrorDialog
        error={drawerOpen || tavilyModalOpen ? null : error}
        title={t("operationFailed")}
        onDismiss={() => setError("")}
        onRetry={() => void load()}
      />
      <AdminToast message={notice} tone="success" onDismiss={() => setNotice("")} />

      <SearchField
        aria-label={t("search")}
        className="w-full sm:w-[320px]"
        value={query}
        onChange={setQuery}
      >
        <SearchField.Group>
          <SearchField.SearchIcon />
          <SearchField.Input placeholder={t("search")} />
          <SearchField.ClearButton />
        </SearchField.Group>
      </SearchField>

      {loading && !mcps.length ? (
        <div className="flex min-h-48 items-center justify-center text-sm text-muted">
          <LoaderCircle className="mr-2 size-4 animate-spin" />
          {t("loading")}
        </div>
      ) : tavilyPresetVisible || visibleMcps.length ? (
        <div className="grid items-stretch gap-4 md:grid-cols-2 xl:grid-cols-3">
          {tavilyPresetVisible ? <TavilyPresetCard onConfigure={openTavily} /> : null}
          {visibleMcps.map((mcp) => (
            <MCPCard
              key={mcp.id}
              mcp={mcp}
              busy={busyID === mcp.id}
              onEdit={() => (mcp.id === "tavily" ? openTavily() : openEdit(mcp))}
              onToggle={() => void mutate(mcp, mcp.enabled ? "disable" : "enable")}
              onDelete={() => setDeleteTarget(mcp)}
            />
          ))}
        </div>
      ) : (
        <AdminEmptyState
          icon={<McpPageIcon className="size-6" />}
          title={t("noMatch")}
          description={t("noMatchDescription")}
        />
      )}

      <Modal
        isOpen={tavilyModalOpen}
        onOpenChange={(open) => {
          if (saving) return;
          setTavilyModalOpen(open);
          if (!open) {
            setTavilyEditing(null);
            setTavilyAPIKey("");
            setTavilyError("");
          }
        }}
      >
        <Modal.Backdrop isDismissable={!saving}>
          <Modal.Container placement="center" size="sm">
            <Modal.Dialog className="mx-auto w-full max-w-[440px]">
              <Modal.CloseTrigger aria-label={t("tavily.close")} />
              <Modal.Header className="items-start">
                <Modal.Icon className="bg-[#3370FF]/10 text-[#3370FF]">
                  <SearchIcon className="size-5" />
                </Modal.Icon>
                <div className="min-w-0">
                  <Modal.Heading>
                    {tavilyEditing ? t("tavily.manageTitle") : t("tavily.title")}
                  </Modal.Heading>
                  <p className="mt-1 text-sm leading-5 text-muted">
                    {tavilyEditing ? t("tavily.manageDescription") : t("tavily.description")}
                  </p>
                </div>
              </Modal.Header>
              <Modal.Body className="grid content-start gap-4">
                {tavilyError ? (
                  <AdminAlert tone="error" icon={<CircleAlert className="size-4" />}>
                    <span aria-live="polite">{tavilyError}</span>
                  </AdminAlert>
                ) : null}
                {tavilyEditing?.header_hints?.Authorization ? (
                  <AdminAlert tone="success" icon={<KeyRound className="size-4" />}>
                    {t("tavily.keySaved")}
                  </AdminAlert>
                ) : null}
                <TextField value={tavilyAPIKey} variant="secondary" onChange={setTavilyAPIKey}>
                  <Label>{tavilyEditing ? t("tavily.newApiKey") : t("tavily.apiKey")}</Label>
                  <div className="relative">
                    <KeyRound
                      aria-hidden="true"
                      className="pointer-events-none absolute left-3 top-1/2 z-10 size-4 -translate-y-1/2 text-muted"
                    />
                    <Input
                      autoFocus
                      autoComplete="new-password"
                      className="pl-10"
                      placeholder={
                        tavilyEditing
                          ? t("tavily.apiKeyRotatePlaceholder")
                          : t("tavily.apiKeyPlaceholder")
                      }
                      type="password"
                    />
                  </div>
                </TextField>
                <Card className="p-4">
                  <Switch isSelected={tavilyDefaultEnabled} onChange={setTavilyDefaultEnabled}>
                    <Switch.Content>
                      <Switch.Control>
                        <Switch.Thumb />
                      </Switch.Control>
                      <span>
                        <span className="block text-sm font-medium">{t("fields.defaultOn")}</span>
                        <span className="mt-0.5 block text-xs leading-5 text-muted">
                          {t("tavily.defaultDescription")}
                        </span>
                      </span>
                    </Switch.Content>
                  </Switch>
                </Card>
              </Modal.Body>
              <Modal.Footer className="gap-2">
                <Button
                  variant="outline"
                  isDisabled={saving}
                  onPress={() => {
                    setTavilyEditing(null);
                    setTavilyAPIKey("");
                    setTavilyError("");
                    setTavilyModalOpen(false);
                  }}
                >
                  {t("cancel")}
                </Button>
                <Button
                  className="min-w-28 gap-2 bg-[#3370FF] text-white hover:bg-[#2B60E8]"
                  isDisabled={saving}
                  onPress={() => void saveTavily()}
                >
                  {saving ? <LoaderCircle className="size-4 animate-spin" /> : null}
                  {saving
                    ? t("saving")
                    : tavilyEditing
                      ? t("tavily.saveChanges")
                      : t("tavily.save")}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      <AdminDrawer
        open={drawerOpen}
        onOpenChange={(open) => {
          if (!saving) setDrawerOpen(open);
        }}
        className="admin-theme-orange"
        title={editing ? t("editTitle", { name: editing.name }) : t("add")}
        description={t("drawerDescription")}
        size="lg"
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button variant="outline" isDisabled={saving} onPress={() => setDrawerOpen(false)}>
              {t("cancel")}
            </Button>
            <Button isDisabled={saving} className="min-w-32 gap-2" onPress={() => void save()}>
              {saving ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <Save className="size-4" />
              )}
              {saving ? t("saving") : editing ? t("save") : t("add")}
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
              <Label>{t("fields.name")}</Label>
              <Input autoFocus placeholder="GitHub" />
            </TextField>
            <Field label={t("fields.transport")}>
              <SelectControl
                className="h-10 min-h-10 w-full rounded-xl"
                value={form.transport}
                onValueChange={(value) =>
                  setForm({ ...form, transport: value as FormState["transport"] })
                }
                options={[
                  { value: "stdio", label: `stdio · ${t("fields.command")}` },
                  { value: "http", label: "HTTP · URL" },
                  { value: "sse", label: "SSE · URL" },
                ]}
                contentClassName="cocola-admin-ui"
              />
            </Field>
          </div>

          <p className="-mt-3 text-xs leading-5 text-muted">
            {form.transport === "stdio"
              ? t("fields.stdioHint")
              : t("fields.remoteHint", { transport: form.transport === "http" ? "HTTP" : "SSE" })}
          </p>

          <TextField
            value={form.description}
            variant="secondary"
            onChange={(description) => setForm({ ...form, description })}
          >
            <Label>
              {t("fields.description")}{" "}
              <span className="text-muted font-normal">· {t("fields.optional")}</span>
            </Label>
            <Input placeholder={t("fields.descriptionPlaceholder")} />
          </TextField>

          {form.transport === "stdio" ? (
            <>
              <TextField
                value={form.command}
                variant="secondary"
                onChange={(command) => setForm({ ...form, command })}
              >
                <Label>{t("fields.command")}</Label>
                <Input className="font-mono" placeholder="npx" />
              </TextField>
              <TextField
                value={form.args}
                variant="secondary"
                onChange={(args) => setForm({ ...form, args })}
              >
                <Label>
                  {t("fields.arguments")}{" "}
                  <span className="text-muted font-normal">
                    · {t("fields.optional")} · {t("fields.onePerLine")}
                  </span>
                </Label>
                <TextArea
                  className="min-h-24 font-mono"
                  placeholder={"-y\n@modelcontextprotocol/server-github"}
                />
              </TextField>
              <SecretPairsField
                label={t("fields.environment")}
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
                label={t("fields.url")}
                hint={editing ? t("fields.keepUrl") : t("fields.pasteUrl")}
              >
                <TextField
                  className="relative"
                  value={form.url}
                  variant="secondary"
                  onChange={(url) => setForm({ ...form, url })}
                >
                  <Label className="sr-only">{t("fields.url")}</Label>
                  <Input
                    type={showURL ? "text" : "password"}
                    className="w-full pr-11 font-mono"
                    placeholder={
                      editing
                        ? editing.url_hint || t("fields.savedUrl")
                        : "https://mcp.example.com/api?token=..."
                    }
                    autoComplete="off"
                  />
                  <Button
                    isIconOnly
                    aria-label={showURL ? t("fields.hideUrl") : t("fields.showUrl")}
                    className="absolute right-1 top-1/2 size-9 min-w-9 -translate-y-1/2"
                    size="sm"
                    variant="ghost"
                    onPress={() => setShowURL((show) => !show)}
                  >
                    {showURL ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                  </Button>
                </TextField>
              </Field>
              <p className="-mt-3 text-xs leading-5 text-muted">{t("fields.urlSecure")}</p>
              <SecretPairsField
                label={t("fields.headers")}
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
                <span className="block text-sm font-medium">{t("fields.defaultOn")}</span>
                <span className="text-muted mt-0.5 block text-xs leading-5">
                  {t("fields.defaultDescription")}
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
                {t("fields.advanced")}
              </Button>
              {advancedOpen ? (
                <div className="mt-3">
                  <TextField
                    value={form.id}
                    variant="secondary"
                    onChange={(id) => setForm({ ...form, id })}
                  >
                    <Label>
                      ID <span className="text-muted font-normal">· {t("fields.optional")}</span>
                    </Label>
                    <Input className="font-mono" placeholder={slugify(form.name) || "github"} />
                    <span className="text-muted text-xs">{t("fields.idHint")}</span>
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
        title={t("delete.title")}
        description={t("delete.description", {
          name: deleteTarget?.name || deleteTarget?.id || t("delete.thisServer"),
        })}
        confirmLabel={t("delete.action")}
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
  const t = useTranslations("admin.mcpsPage.card");
  const transport = normalizeTransport(mcp.transport);
  const remote = transport !== "stdio";
  const endpoint = !remote
    ? [mcp.command, ...(mcp.args ?? [])].filter(Boolean).join(" ")
    : mcp.url_hint;
  return (
    <Card className="admin-mcp-card p-5">
      <Card.Content className="flex h-full min-w-0 flex-col p-0">
        <div className="flex items-start gap-3">
          <MCPServerIcon className="admin-mcp-card-icon" id={mcp.id} />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="min-w-0 flex-1 text-sm font-semibold text-foreground">
                <AdminTruncatedValue copyLabel={t("copyName")} value={mcp.name || mcp.id} />
              </h2>
              <Chip size="sm" variant="soft">
                {transport === "http" ? "HTTP" : transport}
              </Chip>
            </div>
            <p className="text-muted mt-1 line-clamp-2 min-h-10 text-sm leading-5">
              {mcp.description || t("noDescription")}
            </p>
          </div>
        </div>

        <div className="bg-surface-secondary mt-4 min-w-0 rounded-2xl p-3">
          <div className="text-muted text-[10px] font-semibold uppercase tracking-[0.12em]">
            {remote ? "URL" : t("command")}
          </div>
          <AdminTruncatedValue
            className="mt-1 font-mono text-xs tabular-nums text-foreground/80"
            copyLabel={remote ? t("copyUrl") : t("copyCommand")}
            value={endpoint || (remote ? t("remoteSaved") : t("commandSaved"))}
          />
        </div>

        {mcp.default_enabled ? (
          <div className="text-muted mt-3 text-xs">{t("defaultOn")}</div>
        ) : null}
        <div className="mt-auto flex items-center justify-between gap-3 pt-4">
          <Button isDisabled={busy} size="sm" variant="outline" onPress={onEdit}>
            <Pencil className="size-4" />
            {t("edit")}
          </Button>
          <span className="flex items-center gap-2">
            <Switch
              aria-label={
                mcp.enabled
                  ? t("disable", { name: mcp.name || mcp.id })
                  : t("enable", { name: mcp.name || mcp.id })
              }
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
              {t("remove")}
            </Button>
          </span>
        </div>
      </Card.Content>
    </Card>
  );
}

function TavilyPresetCard({ onConfigure }: { onConfigure: () => void }) {
  const t = useTranslations("admin.mcpsPage.tavily");
  const cardT = useTranslations("admin.mcpsPage.card");
  return (
    <Card className="admin-mcp-card p-5">
      <Card.Content className="flex h-full min-w-0 flex-col p-0">
        <div className="flex items-start gap-3">
          <MCPServerIcon className="admin-mcp-card-icon" id="tavily" />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="min-w-0 flex-1 text-sm font-semibold text-foreground">Tavily</h2>
              <Chip size="sm" variant="soft">
                {t("notConfigured")}
              </Chip>
            </div>
            <p className="mt-1 line-clamp-2 min-h-10 text-sm leading-5 text-muted">
              {t("serverDescription")}
            </p>
          </div>
        </div>

        <div className="mt-4 min-w-0 rounded-2xl bg-surface-secondary p-3">
          <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted">
            URL
          </div>
          <AdminTruncatedValue
            className="mt-1 font-mono text-xs tabular-nums text-foreground/80"
            copyLabel={cardT("copyUrl")}
            value={TAVILY_MCP_URL}
          />
        </div>

        <div className="mt-auto flex items-center justify-between gap-3 pt-4">
          <Button
            className="gap-2 bg-[#3370FF] text-white hover:bg-[#2B60E8]"
            size="sm"
            onPress={onConfigure}
          >
            <KeyRound className="size-4" />
            {t("configure")}
          </Button>
          <Switch aria-label={t("notConfiguredAria")} isDisabled isSelected={false}>
            <Switch.Content>
              <Switch.Control>
                <Switch.Thumb className="admin-switch-thumb shadow-sm" />
              </Switch.Control>
            </Switch.Content>
          </Switch>
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
  const t = useTranslations("admin.mcpsPage.fields");
  return (
    <label className="grid gap-1.5 text-sm">
      <span className="flex items-baseline justify-between gap-3 text-xs font-medium text-muted">
        <span>
          {label}
          {optional ? <span className="font-normal"> · {t("optional")}</span> : null}
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
  const t = useTranslations("admin.mcpsPage.pairs");
  const savedKeys = Object.keys(savedHints ?? {});
  return (
    <Field label={label} optional hint={t("hint")}>
      <TextField value={value} variant="secondary" onChange={onChange}>
        <Label className="sr-only">{label}</Label>
        <TextArea className="min-h-24 font-mono" placeholder={placeholder} />
      </TextField>
      {savedKeys.length ? (
        <div className="text-muted flex flex-wrap items-center justify-between gap-2 text-xs">
          <span>{clearSaved ? t("cleared") : t("saved", { keys: savedKeys.join(", ") })}</span>
          <Button
            className="h-8 min-h-8 px-2"
            size="sm"
            variant="ghost"
            onPress={() => onClearSaved(!clearSaved)}
          >
            {clearSaved ? t("keep") : t("clear")}
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

function parsePairs(
  raw: string,
  field: string,
  message: (
    kind: "pairFormat" | "pairKey" | "pairValue" | "pairDuplicate",
    values: { field: string; line: number; key: string },
  ) => string,
): Record<string, string> | undefined {
  const result: Record<string, string> = {};
  const lines = raw.split("\n");
  for (const [index, rawLine] of lines.entries()) {
    const line = rawLine.trim();
    if (!line) continue;
    const separator = line.indexOf("=");
    if (separator <= 0) {
      throw new Error(message("pairFormat", { field, line: index + 1, key: "" }));
    }
    const key = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_.-]*$/.test(key)) {
      throw new Error(message("pairKey", { field, line: index + 1, key }));
    }
    if (!value) throw new Error(message("pairValue", { field, line: index + 1, key }));
    if (Object.hasOwn(result, key)) {
      throw new Error(message("pairDuplicate", { field, line: index + 1, key }));
    }
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
