"use client";

import { AlertDialog, Button, Card, Chip, Switch, Tooltip } from "@heroui/react";
import { ItemCardGroup } from "@cocola/ui-compat/item-card-group";
import {
  BrainCircuit,
  CalendarClock,
  CircleAlert,
  Database,
  LoaderCircle,
  RefreshCw,
  SlidersHorizontal,
  Sparkles,
  Tag,
  Trash2,
  UserRound,
  type LucideIcon,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslations } from "next-intl";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";

type MemorySettings = {
  global_enabled: boolean;
  use_enabled: boolean;
  learn_enabled: boolean;
};

type MemoryItem = {
  id: string;
  category: MemoryCategory;
  title: string;
  abstract?: string;
  content?: string;
};

type MemoryCategory = "profile" | "preferences" | "entities" | "events";

const CATEGORIES: Array<{ id: MemoryCategory; icon: LucideIcon }> = [
  { id: "profile", icon: UserRound },
  { id: "preferences", icon: SlidersHorizontal },
  { id: "entities", icon: Tag },
  { id: "events", icon: CalendarClock },
];
const DEFAULT_CATEGORY = CATEGORIES[0]!;

export function MemoryPanel() {
  const t = useTranslations("profile.memory");
  const [settings, setSettings] = useState<MemorySettings>({
    global_enabled: false,
    use_enabled: false,
    learn_enabled: false,
  });
  const [category, setCategory] = useState<MemoryCategory>("profile");
  const [items, setItems] = useState<MemoryItem[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [details, setDetails] = useState<Record<string, string>>({});
  const [expandedID, setExpandedID] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deletingID, setDeletingID] = useState<string | null>(null);
  const [clearOpen, setClearOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<MemoryItem | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [degraded, setDegraded] = useState(false);
  const requestID = useRef(0);
  const requestController = useRef<AbortController | null>(null);

  const load = useCallback(
    async (showError = false, cursor = "", append = false) => {
      const id = ++requestID.current;
      requestController.current?.abort();
      const controller = new AbortController();
      requestController.current = controller;
      if (append) setLoadingMore(true);
      else setLoading(true);
      try {
        const settingsResponse = await fetch("/api/memory/settings", {
          cache: "no-store",
          signal: controller.signal,
        });
        if (!settingsResponse.ok) throw new Error(await readMemoryError(settingsResponse));
        const nextSettings = (await settingsResponse.json()) as MemorySettings;
        if (id !== requestID.current) return;
        setSettings(nextSettings);
        if (!nextSettings.global_enabled) {
          setItems([]);
          setNextCursor("");
          setDegraded(false);
          return;
        }
        const query = new URLSearchParams({ category, limit: "30" });
        if (cursor) query.set("cursor", cursor);
        const itemsResponse = await fetch(`/api/memory/items?${query.toString()}`, {
          cache: "no-store",
          signal: controller.signal,
        });
        if (!itemsResponse.ok) throw new Error(await readMemoryError(itemsResponse));
        const body = (await itemsResponse.json()) as {
          items?: MemoryItem[];
          next_cursor?: string;
        };
        if (id !== requestID.current) return;
        setItems((current) => (append ? [...current, ...(body.items ?? [])] : (body.items ?? [])));
        setNextCursor(body.next_cursor ?? "");
        setDegraded(false);
      } catch (cause) {
        if (cause instanceof DOMException && cause.name === "AbortError") return;
        if (id !== requestID.current) return;
        setDegraded(true);
        if (showError) setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        if (id === requestID.current) {
          setLoading(false);
          setLoadingMore(false);
        }
      }
    },
    [category],
  );

  useEffect(() => {
    void load(false);
    return () => requestController.current?.abort();
  }, [load]);

  const updateSettings = async (patch: Partial<MemorySettings>) => {
    if (!settings.global_enabled || saving) return;
    const previous = settings;
    const next = { ...settings, ...patch };
    setSettings(next);
    setSaving(true);
    try {
      const response = await fetch("/api/memory/settings", {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          use_enabled: next.use_enabled,
          learn_enabled: next.learn_enabled,
        }),
      });
      if (!response.ok) throw new Error(await readMemoryError(response));
      setSettings((await response.json()) as MemorySettings);
      setDegraded(false);
    } catch (cause) {
      setSettings(previous);
      setDegraded(true);
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  };

  const toggleDetail = async (item: MemoryItem) => {
    if (expandedID === item.id) {
      setExpandedID(null);
      return;
    }
    setExpandedID(item.id);
    if (details[item.id] !== undefined) return;
    try {
      const response = await fetch(`/api/memory/items/${encodeURIComponent(item.id)}`, {
        cache: "no-store",
      });
      if (!response.ok) throw new Error(await readMemoryError(response));
      const detail = (await response.json()) as MemoryItem;
      setDetails((current) => ({
        ...current,
        [item.id]: detail.content || detail.abstract || t("noDetail"),
      }));
    } catch (cause) {
      setExpandedID(null);
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  };

  const deleteItem = async () => {
    if (!deleteTarget) return;
    setDeletingID(deleteTarget.id);
    try {
      const response = await fetch(`/api/memory/items/${encodeURIComponent(deleteTarget.id)}`, {
        method: "DELETE",
      });
      if (!response.ok) throw new Error(await readMemoryError(response));
      setItems((current) => current.filter((item) => item.id !== deleteTarget.id));
      setDetails((current) => {
        const next = { ...current };
        delete next[deleteTarget.id];
        return next;
      });
      setExpandedID((current) => (current === deleteTarget.id ? null : current));
      setDeleteTarget(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setDeletingID(null);
    }
  };

  const clearAll = async () => {
    setDeletingID("*");
    try {
      const response = await fetch("/api/memory/items", { method: "DELETE" });
      if (!response.ok) throw new Error(await readMemoryError(response));
      setItems([]);
      setNextCursor("");
      setDetails({});
      setExpandedID(null);
      setClearOpen(false);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setDeletingID(null);
    }
  };

  const stateLabel = degraded
    ? t("degraded")
    : settings.global_enabled
      ? settings.use_enabled || settings.learn_enabled
        ? t("active")
        : t("paused")
      : t("disabled");
  const stateColor = degraded ? "danger" : settings.global_enabled ? "success" : "default";
  const currentCategory = useMemo(
    () => CATEGORIES.find((candidate) => candidate.id === category) ?? DEFAULT_CATEGORY,
    [category],
  );

  return (
    <>
      <ItemCardGroup>
        <ItemCardGroup.Header>
          <ItemCardGroup.Title>{t("title")}</ItemCardGroup.Title>
        </ItemCardGroup.Header>
        <Card className="overflow-hidden">
          <Card.Header className="flex-row items-center gap-3 border-b border-divider p-4">
            <span className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-violet-500/10 text-violet-600">
              <BrainCircuit className="size-4" />
            </span>
            <div className="min-w-0 flex-1">
              <Card.Title className="text-sm">{t("personal")}</Card.Title>
              <Card.Description className="mt-0.5 text-xs">{t("description")}</Card.Description>
            </div>
            <Chip color={stateColor} size="sm" variant="soft">
              {stateLabel}
            </Chip>
          </Card.Header>

          <Card.Content className="space-y-4 p-4">
            <div className="grid gap-2 sm:grid-cols-2">
              <MemorySwitch
                description={t("useDescription")}
                disabled={!settings.global_enabled || saving}
                label={t("use")}
                selected={settings.use_enabled}
                onChange={(value) => void updateSettings({ use_enabled: value })}
              />
              <MemorySwitch
                description={t("learnDescription")}
                disabled={!settings.global_enabled || saving}
                label={t("learn")}
                selected={settings.learn_enabled}
                onChange={(value) => void updateSettings({ learn_enabled: value })}
              />
            </div>

            <div className="flex items-center gap-2 overflow-x-auto pb-1">
              {CATEGORIES.map(({ id, icon: Icon }) => (
                <Button
                  key={id}
                  className="shrink-0"
                  size="sm"
                  variant={category === id ? "secondary" : "ghost"}
                  onPress={() => setCategory(id)}
                >
                  <Icon className="size-3.5" />
                  {t(id)}
                </Button>
              ))}
              <Tooltip delay={0}>
                <Tooltip.Trigger>
                  <Button
                    isIconOnly
                    aria-label={t("refresh")}
                    className="ml-auto shrink-0"
                    isDisabled={loading}
                    size="sm"
                    variant="ghost"
                    onPress={() => void load(true)}
                  >
                    <RefreshCw className={`size-4 ${loading ? "animate-spin" : ""}`} />
                  </Button>
                </Tooltip.Trigger>
                <Tooltip.Content>{t("refresh")}</Tooltip.Content>
              </Tooltip>
            </div>

            <div className="divide-y divide-divider rounded-xl border border-divider">
              {loading ? (
                <div className="flex min-h-24 items-center justify-center gap-2 text-sm text-muted">
                  <LoaderCircle className="size-4 animate-spin" />
                  {t("loading")}
                </div>
              ) : degraded ? (
                <MemoryEmpty icon={CircleAlert} label={t("unavailable")} />
              ) : !settings.global_enabled ? (
                <MemoryEmpty icon={Database} label={t("adminDisabled")} />
              ) : items.length === 0 ? (
                <MemoryEmpty
                  icon={currentCategory.icon}
                  label={t("empty", { category: t(currentCategory.id) })}
                />
              ) : (
                items.map((item) => (
                  <MemoryRow
                    key={item.id}
                    content={details[item.id]}
                    deleting={deletingID === item.id}
                    expanded={expandedID === item.id}
                    item={item}
                    onDelete={() => setDeleteTarget(item)}
                    onToggle={() => void toggleDetail(item)}
                  />
                ))
              )}
            </div>

            {nextCursor && !loading && !degraded ? (
              <div className="flex justify-center">
                <Button
                  isPending={loadingMore}
                  size="sm"
                  variant="outline"
                  onPress={() => void load(true, nextCursor, true)}
                >
                  {t("loadMore")}
                </Button>
              </div>
            ) : null}

            {settings.global_enabled ? (
              <div className="flex justify-end">
                <Button
                  isDisabled={deletingID !== null}
                  size="sm"
                  variant="danger-soft"
                  onPress={() => setClearOpen(true)}
                >
                  <Trash2 className="size-3.5" />
                  {t("clearAll")}
                </Button>
              </div>
            ) : null}
          </Card.Content>
        </Card>
      </ItemCardGroup>

      <ActionConfirmDialog
        busy={deletingID === "*"}
        confirmLabel={t("clearAll")}
        description={t("clearDescription")}
        icon={Trash2}
        open={clearOpen}
        title={t("clearTitle")}
        tone="danger"
        onConfirm={() => void clearAll()}
        onOpenChange={setClearOpen}
      />
      <ActionConfirmDialog
        busy={Boolean(deleteTarget && deletingID === deleteTarget.id)}
        confirmLabel={t("delete")}
        description={deleteTarget ? t("deleteDescription", { title: deleteTarget.title }) : ""}
        icon={Trash2}
        open={Boolean(deleteTarget)}
        title={t("deleteTitle")}
        tone="danger"
        onConfirm={() => void deleteItem()}
        onOpenChange={(open) => {
          if (!open && !deletingID) setDeleteTarget(null);
        }}
      />
      <MemoryErrorDialog
        error={error}
        onDismiss={() => setError(null)}
        onRetry={() => void load(true)}
      />
    </>
  );
}

function MemorySwitch({
  description,
  disabled,
  label,
  selected,
  onChange,
}: {
  description: string;
  disabled: boolean;
  label: string;
  selected: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-xl bg-surface-secondary px-3 py-2.5">
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        <div className="truncate text-xs text-muted">{description}</div>
      </div>
      <Switch isDisabled={disabled} isSelected={selected} onChange={onChange}>
        <Switch.Content>
          <Switch.Control>
            <Switch.Thumb />
          </Switch.Control>
        </Switch.Content>
      </Switch>
    </div>
  );
}

function MemoryRow({
  content,
  deleting,
  expanded,
  item,
  onDelete,
  onToggle,
}: {
  content?: string;
  deleting: boolean;
  expanded: boolean;
  item: MemoryItem;
  onDelete: () => void;
  onToggle: () => void;
}) {
  const t = useTranslations("profile.memory");
  return (
    <div className="p-3">
      <div className="flex min-w-0 items-center gap-2">
        <Button className="min-w-0 flex-1 justify-start px-2" variant="ghost" onPress={onToggle}>
          <Sparkles className="size-3.5 shrink-0 text-violet-500" />
          <span className="truncate text-sm">{item.title}</span>
        </Button>
        <Tooltip delay={0}>
          <Tooltip.Trigger>
            <Button
              isIconOnly
              aria-label={t("deleteAria", { title: item.title })}
              isPending={deleting}
              size="sm"
              variant="ghost"
              onPress={onDelete}
            >
              <Trash2 className="size-3.5 text-danger" />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>{t("delete")}</Tooltip.Content>
        </Tooltip>
      </div>
      {expanded ? (
        <div className="mx-2 mt-2 whitespace-pre-wrap break-words rounded-lg bg-surface-secondary px-3 py-2 text-sm leading-6 text-muted">
          {content === undefined ? (
            <span className="inline-flex items-center gap-2">
              <LoaderCircle className="size-3.5 animate-spin" /> {t("loading")}
            </span>
          ) : (
            content
          )}
        </div>
      ) : item.abstract ? (
        <p className="mx-2 mt-1 line-clamp-2 text-xs leading-5 text-muted">{item.abstract}</p>
      ) : null}
    </div>
  );
}

function MemoryEmpty({ icon: Icon, label }: { icon: LucideIcon; label: string }) {
  return (
    <div className="flex min-h-24 flex-col items-center justify-center gap-2 p-4 text-center text-sm text-muted">
      <Icon className="size-5 opacity-60" />
      {label}
    </div>
  );
}

function MemoryErrorDialog({
  error,
  onDismiss,
  onRetry,
}: {
  error: string | null;
  onDismiss: () => void;
  onRetry: () => void;
}) {
  const t = useTranslations("profile.memory");
  const common = useTranslations("common.actions");
  return (
    <AlertDialog isOpen={Boolean(error)} onOpenChange={(open) => !open && onDismiss()}>
      <AlertDialog.Backdrop isDismissable>
        <AlertDialog.Container placement="center" size="sm">
          <AlertDialog.Dialog>
            <AlertDialog.Header className="items-start">
              <AlertDialog.Icon status="danger">
                <CircleAlert className="size-5" />
              </AlertDialog.Icon>
              <div className="min-w-0">
                <AlertDialog.Heading>{t("errorTitle")}</AlertDialog.Heading>
                <p className="mt-1 text-sm text-muted">{t("errorDescription")}</p>
              </div>
            </AlertDialog.Header>
            <AlertDialog.Body>
              <p className="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-danger/10 px-3 py-2.5 text-sm text-danger">
                {error}
              </p>
            </AlertDialog.Body>
            <AlertDialog.Footer>
              <Button variant="outline" onPress={onDismiss}>
                {common("close")}
              </Button>
              <Button
                variant="danger"
                onPress={() => {
                  onDismiss();
                  onRetry();
                }}
              >
                {t("tryAgain")}
              </Button>
            </AlertDialog.Footer>
          </AlertDialog.Dialog>
        </AlertDialog.Container>
      </AlertDialog.Backdrop>
    </AlertDialog>
  );
}

async function readMemoryError(response: Response) {
  try {
    const body = await response.json();
    if (typeof body?.error === "string") return body.error;
    if (typeof body?.error?.message === "string") return body.error.message;
    if (typeof body?.message === "string") return body.message;
  } catch {
    // Fall back to the HTTP status.
  }
  return `${response.status} ${response.statusText}`;
}
