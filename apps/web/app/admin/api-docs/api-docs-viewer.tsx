"use client";

import SwaggerUI from "swagger-ui-react";
import { BookOpenText, RefreshCw } from "lucide-react";
import { useMemo, useState } from "react";

export type ApiDocSpec = {
  id: string;
  label: string;
  description: string;
  url: string;
};

export function ApiDocsViewer({ specs }: { specs: ApiDocSpec[] }) {
  const [activeId, setActiveId] = useState(specs[0]?.id ?? "");
  const [refreshNonce, setRefreshNonce] = useState(0);
  const active = useMemo(
    () => specs.find((spec) => spec.id === activeId) ?? specs[0],
    [activeId, specs],
  );

  if (!active) {
    return (
      <div className="rounded-lg border border-border bg-card p-6 text-sm text-muted-foreground">
        No API documentation specs are configured.
      </div>
    );
  }

  const activeUrl = `${active.url}?v=${refreshNonce}`;

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          <div className="grid size-10 shrink-0 place-items-center rounded-md bg-muted">
            <BookOpenText className="size-5 text-muted-foreground" />
          </div>
          <div className="min-w-0">
            <h2 className="text-sm font-semibold">{active.label}</h2>
            <p className="mt-1 text-sm text-muted-foreground">{active.description}</p>
          </div>
        </div>
        <button
          type="button"
          onClick={() => setRefreshNonce((value) => value + 1)}
          className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border bg-background px-3 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
        >
          <RefreshCw className="size-4" />
          Refresh
        </button>
      </div>

      <div className="flex flex-wrap gap-2">
        {specs.map((spec) => (
          <button
            key={spec.id}
            type="button"
            onClick={() => setActiveId(spec.id)}
            className={
              spec.id === active.id
                ? "h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground"
                : "h-9 rounded-md border border-border bg-card px-3 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
            }
          >
            {spec.label}
          </button>
        ))}
      </div>

      <section className="api-docs-swagger overflow-hidden rounded-lg border border-border bg-white text-slate-950">
        <SwaggerUI
          key={activeUrl}
          url={activeUrl}
          deepLinking
          displayOperationId
          displayRequestDuration
          docExpansion="list"
          defaultModelsExpandDepth={1}
          persistAuthorization
        />
      </section>
    </div>
  );
}
