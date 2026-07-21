import { CocolaRuntimeProvider } from "@/app/runtime-provider";
import { AppSidebar } from "@/components/assistant-ui/app-sidebar";
import { WorkspaceToastProvider } from "@/components/assistant-ui/workspace-toast";
import { UsagePanel } from "@/components/profile/usage-panel";
import { SignOutButton } from "@/components/profile/sign-out-button";
import { MemoryPanel } from "@/components/profile/memory-panel";
import { GitHubIntegrationPanel } from "@/components/profile/github-integration-panel";
import { AccountSettingsPanel } from "@/components/profile/account-settings-panel";
import { isAuthFail, requireUser } from "@/lib/server-auth";
import { ArrowLeft, BadgeCheck, Mail, ShieldCheck, UserRound } from "lucide-react";
import Link from "next/link";
import { redirect } from "next/navigation";

export default async function ProfilePage() {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) redirect("/login");
  const user = authResult.user;

  const displayName = user.name || user.email || "User";
  const initial = displayName.trim().slice(0, 1).toUpperCase() || "U";
  const isAdmin = user.role === "admin";

  return (
    <CocolaRuntimeProvider>
      <WorkspaceToastProvider>
        <div className="cocola-user-ui workspace-grain flex h-screen bg-background text-foreground">
          <AppSidebar />
          <main className="min-w-0 flex-1 overflow-y-auto bg-transparent">
            <header className="border-b border-border">
              <div className="mx-auto flex h-16 max-w-5xl items-center gap-3 px-6">
                <Link
                  href="/"
                  className="grid size-9 place-items-center rounded-xl text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                  title="Back to chat"
                >
                  <ArrowLeft className="size-4" />
                </Link>
                <div className="grid size-9 place-items-center rounded-xl text-white brand-gradient">
                  <UserRound className="size-4" />
                </div>
                <div className="min-w-0 flex-1">
                  <h1 className="truncate text-base font-semibold">Profile</h1>
                  <p className="truncate text-xs text-muted-foreground">
                    Personal account information and token usage
                  </p>
                </div>
              </div>
            </header>

            <div className="mx-auto max-w-5xl space-y-6 px-6 py-6">
              {/* Identity card */}
              <section className="rounded-2xl border border-border bg-card shadow-card p-5">
                <div className="flex items-center gap-4">
                  <div className="grid size-14 shrink-0 place-items-center rounded-full bg-primary text-lg font-semibold text-primary-foreground">
                    {initial}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="truncate text-lg font-semibold">{displayName}</h2>
                      <RolePill role={user.role} />
                    </div>
                    <div className="mt-1 flex min-w-0 items-center gap-2 text-sm text-muted-foreground">
                      <Mail className="size-4 shrink-0" />
                      <span className="truncate">{user.email || "-"}</span>
                    </div>
                  </div>
                </div>
              </section>

              <AccountSettingsPanel initialAccount={user} />

              {/* Account status */}
              <section className="rounded-2xl border border-border bg-card shadow-card">
                <div className="flex items-center gap-3 border-b border-border px-4 py-3">
                  <div className="grid size-8 place-items-center rounded-xl bg-emerald-500/10">
                    <BadgeCheck className="size-4 text-emerald-600" />
                  </div>
                  <h2 className="text-sm font-semibold">Account Status</h2>
                </div>
                <div className="grid gap-3 p-4 sm:grid-cols-2">
                  <StatusTile label="Authentication" value="Active" tone="ok" />
                  <StatusTile
                    label="Access Level"
                    value={isAdmin ? "Administrator" : "User"}
                    tone={isAdmin ? "accent" : "muted"}
                  />
                </div>
              </section>

              {/* Usage & quota */}
              <UsagePanel />

              <MemoryPanel />

              <GitHubIntegrationPanel />

              <div className="flex items-center justify-between">
                <SignOutButton />
                <Link
                  href="/"
                  className="inline-flex h-9 items-center justify-center rounded-xl border border-input bg-background px-3 text-sm font-medium hover:bg-accent hover:text-accent-foreground"
                >
                  Back to chat
                </Link>
              </div>
            </div>
          </main>
        </div>
      </WorkspaceToastProvider>
    </CocolaRuntimeProvider>
  );
}

function RolePill({ role }: { role: string }) {
  const admin = role === "admin";
  return (
    <span
      className={
        admin
          ? "inline-flex items-center gap-1 rounded-xl border border-primary/30 bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary"
          : "inline-flex items-center gap-1 rounded-xl border border-border bg-muted px-2 py-0.5 text-xs text-muted-foreground"
      }
    >
      <ShieldCheck className="size-3" />
      {role}
    </span>
  );
}

function StatusTile({
  label,
  value,
  tone = "muted",
}: {
  label: string;
  value: string;
  tone?: "ok" | "accent" | "muted";
}) {
  const dot =
    tone === "ok" ? "bg-emerald-500" : tone === "accent" ? "bg-primary" : "bg-muted-foreground/40";
  return (
    <div className="rounded-xl border border-border bg-background px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 flex items-center gap-2 text-sm font-medium">
        <span className={`size-2 rounded-full ${dot}`} />
        {value}
      </div>
    </div>
  );
}
