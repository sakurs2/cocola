import { ApiDocsViewer, type ApiDocSpec } from "./api-docs-viewer";
import { isAuthFail, requireAdmin } from "@/lib/server-auth";
import { ShieldAlert } from "lucide-react";
import { redirect } from "next/navigation";

const SPECS: ApiDocSpec[] = [
  {
    id: "gateway",
    label: "Gateway API",
    description: "Public chat, conversation history, and artifact download APIs.",
    url: "/api/admin/api-docs/gateway",
  },
  {
    id: "admin-api",
    label: "Admin API",
    description:
      "Operator APIs for users, models, skills, MCP, scheduling, sandbox, audit, and traces.",
    url: "/api/admin/api-docs/admin-api",
  },
  {
    id: "llm-gateway",
    label: "LLM Gateway API",
    description: "Anthropic-compatible model gateway, quota, usage, and health APIs.",
    url: "/api/admin/api-docs/llm-gateway",
  },
];

export default async function AdminApiDocsPage() {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) {
    if (authResult.response.status === 401) {
      redirect("/login?callbackUrl=/admin/api-docs");
    }
    return (
      <main className="min-h-screen bg-background text-foreground">
        <div className="mx-auto max-w-4xl px-6 py-10">
          <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-6">
            <div className="flex items-start gap-3">
              <ShieldAlert className="mt-0.5 size-5 text-destructive" />
              <div>
                <h1 className="text-base font-semibold text-destructive">Admin access required</h1>
                <p className="mt-1 text-sm text-muted-foreground">
                  API documentation includes privileged admin endpoints and is only available to
                  administrators.
                </p>
              </div>
            </div>
          </div>
        </div>
      </main>
    );
  }

  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center px-6">
          <div className="min-w-0">
            <h1 className="truncate text-base font-semibold">API Docs</h1>
            <p className="truncate text-xs text-muted-foreground">
              Swagger documentation for cocola server APIs
            </p>
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-7xl px-6 py-6">
        <ApiDocsViewer specs={SPECS} />
      </div>
    </main>
  );
}
