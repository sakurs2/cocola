"use client";

import { Button, Card, Chip } from "@heroui/react";
import { ArrowLeft, LoaderCircle, Plug } from "lucide-react";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useTranslations } from "next-intl";

type MCPServer = {
  id: string;
  name: string;
  description: string;
  transport: string;
  command?: string;
  url_hint?: string;
  default_enabled: boolean;
  effective_enabled: boolean;
};

export default function MCPDetailPage() {
  const t = useTranslations("connectors.mcp.detail");
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [mcp, setMcp] = useState<MCPServer | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const response = await fetch("/api/mcps", { cache: "no-store", signal: controller.signal });
        if (!response.ok) throw new Error(await readError(response));
        const data = (await response.json()) as { mcps?: MCPServer[] };
        const found = (data.mcps || []).find((item) => item.id === id);
        if (!found) throw new Error(t("notFound"));
        if (!controller.signal.aborted) setMcp(found);
      } catch (cause) {
        if (!controller.signal.aborted)
          setError(cause instanceof Error ? cause.message : String(cause));
      }
    })();
    return () => controller.abort();
  }, [id, t]);

  const toggle = async () => {
    if (!mcp) return;
    const previous = mcp;
    const nextEnabled = !mcp.effective_enabled;
    setMcp({ ...mcp, effective_enabled: nextEnabled });
    setBusy(true);
    setError("");
    try {
      const response = await fetch(
        `/api/mcps/${encodeURIComponent(mcp.id)}/${nextEnabled ? "enable" : "disable"}`,
        { method: "POST" },
      );
      if (!response.ok) throw new Error(await readError(response));
    } catch (cause) {
      setMcp(previous);
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  if (!mcp && !error)
    return (
      <div className="cocola-web-page grid min-h-64 place-items-center p-8">
        <LoaderCircle className="text-muted size-5 animate-spin" />
      </div>
    );

  return (
    <div className="cocola-web-page mx-auto flex w-full max-w-6xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-wrap items-center gap-3">
        <Button
          isIconOnly
          aria-label={t("back")}
          variant="ghost"
          onPress={() => router.push("/mcps")}
        >
          <ArrowLeft className="size-4" />
        </Button>
        <span className="bg-accent-soft text-accent flex size-11 items-center justify-center rounded-2xl">
          <Plug className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-2xl font-semibold tracking-[-0.03em]">{mcp?.name || id}</h1>
          <p className="text-muted mt-1 text-sm">{mcp?.description || mcp?.id || id}</p>
        </div>
      </header>

      {error ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      {mcp ? (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Chip size="sm" variant="soft">
              {mcp.transport}
            </Chip>
            <Chip color={mcp.effective_enabled ? "success" : "warning"} size="sm" variant="soft">
              {mcp.effective_enabled ? t("enabled") : t("disabled")}
            </Chip>
            <Chip size="sm" variant="soft">
              {mcp.default_enabled ? t("defaultOn") : t("defaultOff")}
            </Chip>
            <Button
              className="ml-auto"
              isPending={busy}
              variant={mcp.effective_enabled ? "outline" : "primary"}
              onPress={() => void toggle()}
            >
              {mcp.effective_enabled ? t("disable") : t("enable")}
            </Button>
          </div>
          <Card className="p-5">
            <Card.Header className="p-0">
              <Card.Title>{t("connection")}</Card.Title>
              <Card.Description>{t("connectionDescription")}</Card.Description>
            </Card.Header>
            <Card.Content className="mt-5 grid gap-3 p-0 sm:grid-cols-2">
              <Info label={t("transport")} value={mcp.transport} />
              <Info
                label={t("effective")}
                value={mcp.effective_enabled ? t("enabled") : t("disabled")}
              />
              <Info
                label={mcp.transport.toLowerCase() === "stdio" ? t("command") : "URL"}
                value={
                  mcp.transport.toLowerCase() === "stdio" ? mcp.command || "—" : mcp.url_hint || "—"
                }
              />
              <Info label={t("defaultState")} value={mcp.default_enabled ? t("on") : t("off")} />
            </Card.Content>
          </Card>
          <Card className="p-5">
            <Card.Header className="p-0">
              <Card.Title>{t("behavior")}</Card.Title>
              <Card.Description>
                {mcp.effective_enabled ? t("enabledBehavior") : t("disabledBehavior")}
              </Card.Description>
            </Card.Header>
            <Card.Content className="mt-5 p-0">
              <code className="bg-surface-secondary text-muted block overflow-x-auto rounded-2xl p-4 text-sm">
                {mcp.transport.toLowerCase() === "stdio" ? mcp.command || "—" : mcp.url_hint || "—"}
              </code>
            </Card.Content>
          </Card>
        </>
      ) : null}
    </div>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-surface-secondary min-w-0 rounded-2xl px-4 py-3">
      <span className="text-muted block text-xs">{label}</span>
      <span className="mt-1 block break-all text-sm font-medium">{value}</span>
    </div>
  );
}

async function readError(response: Response) {
  const text = await response.text();
  try {
    const json = JSON.parse(text);
    if (typeof json.error === "string") return json.error;
    if (json.error && typeof json.error === "object")
      return json.error.message || json.error.code || text;
    return json.message || text;
  } catch {
    return text || response.statusText;
  }
}
