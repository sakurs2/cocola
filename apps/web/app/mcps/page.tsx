"use client";

import { Card, Chip, Switch } from "@heroui/react";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { LoaderCircle, Plug } from "lucide-react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { useCallback, useEffect, useState } from "react";
import {
  WorkspacePageFrame,
  WorkspacePageHeader,
  WorkspaceSectionHeader,
} from "@/components/heroui-workspace/workspace-ui";

type MCPServer = {
  id: string;
  name: string;
  description: string;
  transport: string;
  command?: string;
  url_hint?: string;
  env_hints?: Record<string, string>;
  header_hints?: Record<string, string>;
  default_enabled: boolean;
  effective_enabled: boolean;
};

export default function MCPPage() {
  const t = useTranslations("connectors.mcp");
  const [mcps, setMcps] = useState<MCPServer[]>([]);
  const [loading, setLoading] = useState(true);
  const [workingId, setWorkingId] = useState<string | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const response = await fetch("/api/mcps", { cache: "no-store" });
      if (!response.ok) throw new Error(await readError(response));
      const data = (await response.json()) as { mcps?: MCPServer[] };
      setMcps(data.mcps ?? []);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const toggle = async (mcp: MCPServer) => {
    const previous = mcps;
    setWorkingId(mcp.id);
    setError("");
    setMcps((current) =>
      current.map((item) =>
        item.id === mcp.id ? { ...item, effective_enabled: !item.effective_enabled } : item,
      ),
    );
    try {
      const response = await fetch(
        `/api/mcps/${encodeURIComponent(mcp.id)}/${mcp.effective_enabled ? "disable" : "enable"}`,
        { method: "POST" },
      );
      if (!response.ok) throw new Error(await readError(response));
    } catch (cause) {
      setMcps(previous);
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setWorkingId(null);
    }
  };

  return (
    <WorkspacePageFrame>
      <WorkspacePageHeader
        description={t("description")}
        icon={<Plug className="size-5" />}
        title={t("title")}
      />

      {error ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      <WorkspaceSectionHeader
        description={t("count", { count: mcps.length })}
        title={t("published")}
      />

      {loading ? (
        <div className="grid min-h-48 place-items-center">
          <LoaderCircle className="text-muted size-5 animate-spin" />
        </div>
      ) : mcps.length ? (
        <section className="cocola-web-catalog-grid grid items-stretch gap-3 md:grid-cols-2 xl:grid-cols-4">
          {mcps.map((mcp) => {
            const working = workingId === mcp.id;
            const endpoint = mcp.transport === "stdio" ? mcp.command : mcp.url_hint;
            return (
              <Card key={mcp.id} className="cocola-web-catalog-card cocola-web-mcp-card p-4">
                <Card.Content className="flex h-full min-w-0 flex-col p-0">
                  <Link
                    className="group min-w-0 no-underline"
                    href={`/mcps/${encodeURIComponent(mcp.id)}`}
                  >
                    <span className="cocola-web-catalog-card-icon flex size-10 items-center justify-center rounded-2xl bg-orange-500/15 text-orange-500">
                      <Plug className="size-5" />
                    </span>
                    <span className="text-foreground mt-3 block truncate font-semibold">
                      {mcp.name || mcp.id}
                    </span>
                    <span className="text-muted mt-1 line-clamp-2 text-sm leading-5">
                      {mcp.description || t("noDescription")}
                    </span>
                    <span className="mt-3 flex flex-wrap gap-1.5">
                      <Chip size="sm" variant="soft">
                        {mcp.transport}
                      </Chip>
                      <Chip size="sm" variant="soft">
                        {mcp.default_enabled ? t("defaultOn") : t("defaultOff")}
                      </Chip>
                    </span>
                    {endpoint ? (
                      <code className="bg-surface-secondary text-muted mt-2 block truncate rounded-lg px-2.5 py-1.5 text-xs">
                        {endpoint}
                      </code>
                    ) : null}
                  </Link>
                  <div className="border-separator mt-3 flex items-center justify-end border-t pt-3">
                    <Switch
                      aria-label={t(mcp.effective_enabled ? "disable" : "enable", {
                        name: mcp.name || mcp.id,
                      })}
                      isDisabled={working}
                      isSelected={mcp.effective_enabled}
                      onChange={() => void toggle(mcp)}
                    >
                      <Switch.Content>
                        <Switch.Control>
                          <Switch.Thumb />
                        </Switch.Control>
                      </Switch.Content>
                    </Switch>
                  </div>
                </Card.Content>
              </Card>
            );
          })}
        </section>
      ) : (
        <EmptyState>
          <EmptyState.Header>
            <EmptyState.Media variant="icon">
              <Plug className="text-orange-500" />
            </EmptyState.Media>
            <EmptyState.Title>{t("empty")}</EmptyState.Title>
            <EmptyState.Description>{t("emptyDescription")}</EmptyState.Description>
          </EmptyState.Header>
        </EmptyState>
      )}
    </WorkspacePageFrame>
  );
}

async function readError(response: Response) {
  const text = await response.text();
  try {
    const json = JSON.parse(text);
    if (typeof json.error === "string") return json.error;
    if (json.error && typeof json.error === "object") {
      return json.error.message || json.error.code || text;
    }
    return json.message || text;
  } catch {
    return text || response.statusText;
  }
}
