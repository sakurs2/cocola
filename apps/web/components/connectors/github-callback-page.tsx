"use client";

import { GitFork, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";

export function GitHubCallbackPage({ step }: { step: "manifest" | "installation" | "oauth" }) {
  const [error, setError] = useState("");

  useEffect(() => {
    const query = new URLSearchParams(window.location.search);
    const upstreamError = query.get("error");
    if (upstreamError) {
      setError("GitHub cancelled or rejected this connector step.");
      return;
    }

    const run = async () => {
      if (step === "installation") {
        const response = await fetch("/api/connectors/github/installation/complete", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ return_to: "/connectors" }),
        });
        const result = (await response.json().catch(() => ({}))) as {
          authorization_url?: string;
          error?: { message?: string };
        };
        if (!response.ok || !result.authorization_url) {
          throw new Error(result.error?.message || "Could not start GitHub authorization");
        }
        window.location.replace(result.authorization_url);
        return;
      }

      const code = query.get("code") || "";
      const state =
        query.get("state") ||
        (step === "manifest" ? sessionStorage.getItem("cocola.github.manifest.state") : "") ||
        "";
      if (!code || !state) throw new Error("GitHub callback is missing its one-time state");
      const endpoint =
        step === "manifest"
          ? "/api/connectors/github/manifest/complete"
          : "/api/connectors/github/oauth/complete";
      const response = await fetch(endpoint, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ code, state }),
      });
      const result = (await response.json().catch(() => ({}))) as {
        return_to?: string;
        error?: { message?: string };
      };
      if (!response.ok) throw new Error(result.error?.message || "GitHub connection failed");
      if (step === "manifest") sessionStorage.removeItem("cocola.github.manifest.state");
      window.location.replace(result.return_to || "/connectors");
    };

    void run().catch((cause) => setError(cause instanceof Error ? cause.message : String(cause)));
  }, [step]);

  return (
    <div className="grid h-full place-items-center px-5">
      <div className="w-full max-w-md rounded-2xl border border-border bg-card p-7 text-center shadow-card">
        <div className="mx-auto grid size-12 place-items-center rounded-2xl bg-foreground text-background">
          <GitFork className="size-5" />
        </div>
        <h1 className="mt-4 text-lg font-semibold">Connecting GitHub</h1>
        {error ? (
          <>
            <p role="alert" className="mt-2 text-sm text-destructive">
              {error}
            </p>
            <a
              href="/connectors"
              className="mt-5 inline-flex h-10 items-center rounded-xl bg-foreground px-4 text-sm font-medium text-background"
            >
              Back to Connectors
            </a>
          </>
        ) : (
          <p className="mt-2 inline-flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Completing the secure handoff…
          </p>
        )}
      </div>
    </div>
  );
}
