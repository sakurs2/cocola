"use client";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  CheckCircle2,
  LoaderCircle,
  RefreshCw,
  Shield,
  ShieldCheck,
  Trash2,
  UserPlus,
  Users,
} from "lucide-react";
import { signOut } from "next-auth/react";
import { useCallback, useEffect, useMemo, useState } from "react";

type Role = "user" | "admin";

type AuthUser = {
  id: string;
  username: string;
  email: string;
  name?: string;
  role: Role;
  enabled: boolean;
  created_by?: string;
  created_at?: string;
  updated_at?: string;
  last_login_at?: string;
};

type UserForm = {
  username: string;
  email: string;
  role: Role;
  password: string;
};

const EMPTY_FORM: UserForm = {
  username: "",
  email: "",
  role: "user",
  password: "",
};

export default function AdminUsersPage() {
  const [users, setUsers] = useState<AuthUser[]>([]);
  const [form, setForm] = useState<UserForm>(EMPTY_FORM);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [actingId, setActingId] = useState<string | null>(null);
  const [resetDrafts, setResetDrafts] = useState<Record<string, string>>({});
  const [deleteTarget, setDeleteTarget] = useState<AuthUser | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const refresh = useCallback(async () => {
    setError("");
    setLoading(true);
    try {
      const res = await fetch("/api/admin/users", { cache: "no-store" });
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (!res.ok) throw new Error(await responseError(res));
      const body = (await res.json()) as { users?: AuthUser[] };
      setUsers(Array.isArray(body.users) ? body.users : []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const stats = useMemo(
    () => ({
      total: users.length,
      admins: users.filter((u) => u.role === "admin").length,
      enabled: users.filter((u) => u.enabled).length,
    }),
    [users],
  );

  const createUser = async () => {
    setError("");
    setNotice("");
    setSaving(true);
    try {
      const res = await fetch("/api/admin/users", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          username: form.username.trim(),
          email: form.email.trim(),
          role: form.role,
          password: form.password,
          enabled: true,
        }),
      });
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (!res.ok) throw new Error(await responseError(res));
      setForm(EMPTY_FORM);
      setNotice("User created");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const updateUser = async (user: AuthUser, patch: Partial<Pick<AuthUser, "role" | "enabled">>) => {
    setError("");
    setNotice("");
    setActingId(user.id);
    try {
      const res = await fetch(`/api/admin/users/${encodeURIComponent(user.id)}`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          role: patch.role ?? user.role,
          enabled: patch.enabled ?? user.enabled,
          username: user.username,
        }),
      });
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (!res.ok) throw new Error(await responseError(res));
      setNotice("User updated");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActingId(null);
    }
  };

  const resetPassword = async (user: AuthUser) => {
    const password = (resetDrafts[user.id] ?? "").trim();
    if (!password) {
      setError("Password is required");
      return;
    }
    setError("");
    setNotice("");
    setActingId(user.id);
    try {
      const res = await fetch(`/api/admin/users/${encodeURIComponent(user.id)}/password`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ password }),
      });
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (!res.ok) throw new Error(await responseError(res));
      setResetDrafts((drafts) => ({ ...drafts, [user.id]: "" }));
      setNotice("Password reset");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActingId(null);
    }
  };

  const deleteUser = async () => {
    if (!deleteTarget) return;
    setError("");
    setNotice("");
    setDeleting(true);
    setActingId(deleteTarget.id);
    try {
      const res = await fetch(`/api/admin/users/${encodeURIComponent(deleteTarget.id)}`, {
        method: "DELETE",
      });
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (!res.ok) throw new Error(await responseError(res));
      setDeleteTarget(null);
      setNotice("User deleted");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeleting(false);
      setActingId(null);
    }
  };

  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <div className="grid size-9 place-items-center rounded-md bg-primary text-primary-foreground">
            <Users className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">Users</h1>
            <p className="truncate text-xs text-muted-foreground">
              Whitelist accounts and admin access
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={() => void refresh()} disabled={loading}>
            {loading ? (
              <LoaderCircle className="mr-2 size-4 animate-spin" />
            ) : (
              <RefreshCw className="mr-2 size-4" />
            )}
            Refresh
          </Button>
        </div>
      </header>

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-6">
        {error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}
        {notice && (
          <div className="flex items-center gap-2 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-300">
            <CheckCircle2 className="size-4 shrink-0" />
            <span>{notice}</span>
          </div>
        )}

        <section className="grid gap-3 md:grid-cols-3">
          <Metric label="Users" value={String(stats.total)} />
          <Metric label="Enabled" value={String(stats.enabled)} />
          <Metric label="Admins" value={String(stats.admins)} />
        </section>

        <section className="rounded-lg border border-border bg-card">
          <div className="flex items-center gap-3 border-b border-border px-4 py-3">
            <div className="grid size-8 place-items-center rounded-md bg-muted">
              <UserPlus className="size-4 text-muted-foreground" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium">Create user</div>
              <div className="text-xs text-muted-foreground">
                Passwords are sent to the admin-api and stored as bcrypt hashes.
              </div>
            </div>
          </div>
          <div className="grid gap-3 p-4 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.2fr)_8rem_minmax(0,0.9fr)_auto]">
            <TextInput
              label="Username"
              value={form.username}
              onChange={(username) => setForm((prev) => ({ ...prev, username }))}
            />
            <TextInput
              label="Email"
              type="email"
              value={form.email}
              onChange={(email) => setForm((prev) => ({ ...prev, email }))}
            />
            <label className="grid gap-1 text-xs text-muted-foreground">
              Role
              <select
                value={form.role}
                onChange={(e) => setForm((prev) => ({ ...prev, role: e.target.value as Role }))}
                className="h-10 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none focus:border-ring"
              >
                <option value="user">user</option>
                <option value="admin">admin</option>
              </select>
            </label>
            <TextInput
              label="Password"
              type="password"
              value={form.password}
              onChange={(password) => setForm((prev) => ({ ...prev, password }))}
            />
            <div className="flex items-end">
              <Button
                className="w-full"
                disabled={saving || !form.username.trim() || !form.email.trim() || !form.password}
                onClick={() => void createUser()}
              >
                {saving ? <LoaderCircle className="mr-2 size-4 animate-spin" /> : null}
                Create
              </Button>
            </div>
          </div>
        </section>

        <section className="overflow-hidden rounded-lg border border-border bg-card">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[1024px] text-sm">
              <thead className="border-b border-border bg-muted/50 text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-3 text-left font-medium">User</th>
                  <th className="px-4 py-3 text-left font-medium">Role</th>
                  <th className="px-4 py-3 text-left font-medium">Status</th>
                  <th className="px-4 py-3 text-left font-medium">Last login</th>
                  <th className="px-4 py-3 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {loading && users.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="px-4 py-10 text-center text-muted-foreground">
                      Loading users...
                    </td>
                  </tr>
                ) : users.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="px-4 py-10 text-center text-muted-foreground">
                      No users found
                    </td>
                  </tr>
                ) : (
                  users.map((user) => {
                    const busy = actingId === user.id;
                    const protectedAdmin = isProtectedAdmin(user);
                    const roleLocked = protectedAdmin && user.role === "admin";
                    const disableLocked = protectedAdmin && user.enabled;
                    return (
                      <tr key={user.id} className="border-b border-border/70 last:border-0">
                        <td className="px-4 py-3">
                          <div className="font-medium">{user.username || user.email}</div>
                          <div className="max-w-[260px] truncate text-xs text-muted-foreground">
                            {user.username} / {user.email}
                          </div>
                        </td>
                        <td className="px-4 py-3">
                          <RolePill role={user.role} />
                        </td>
                        <td className="px-4 py-3">
                          <StatusPill enabled={user.enabled} />
                        </td>
                        <td className="px-4 py-3 text-muted-foreground">
                          {formatTime(user.last_login_at)}
                        </td>
                        <td className="px-4 py-3">
                          <div className="flex flex-wrap justify-end gap-2">
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={Boolean(actingId) || roleLocked}
                              title={roleLocked ? "Bootstrap admin cannot be demoted" : undefined}
                              onClick={() =>
                                void updateUser(user, {
                                  role: user.role === "admin" ? "user" : "admin",
                                })
                              }
                            >
                              {busy ? <LoaderCircle className="mr-2 size-4 animate-spin" /> : null}
                              {user.role === "admin" ? "Make user" : "Make admin"}
                            </Button>
                            <Button
                              variant={user.enabled ? "destructive" : "outline"}
                              size="sm"
                              disabled={Boolean(actingId) || disableLocked}
                              title={disableLocked ? "Bootstrap admin cannot be disabled" : undefined}
                              onClick={() => void updateUser(user, { enabled: !user.enabled })}
                            >
                              {user.enabled ? "Disable" : "Enable"}
                            </Button>
                            <input
                              type="password"
                              placeholder="New password"
                              value={resetDrafts[user.id] ?? ""}
                              onChange={(e) =>
                                setResetDrafts((drafts) => ({
                                  ...drafts,
                                  [user.id]: e.target.value,
                                }))
                              }
                              className="h-9 w-40 rounded-md border border-input bg-background px-3 text-sm outline-none focus:border-ring"
                            />
                            <Button
                              variant="secondary"
                              size="sm"
                              disabled={Boolean(actingId) || !(resetDrafts[user.id] ?? "").trim()}
                              onClick={() => void resetPassword(user)}
                            >
                              Reset
                            </Button>
                            <Button
                              variant="destructive"
                              size="sm"
                              disabled={Boolean(actingId) || protectedAdmin}
                              onClick={() => setDeleteTarget(user)}
                              title={
                                protectedAdmin ? "Bootstrap admin cannot be deleted" : "Delete user"
                              }
                            >
                              <Trash2 className="mr-2 size-4" />
                              Delete
                            </Button>
                          </div>
                        </td>
                      </tr>
                    );
                  })
                )}
              </tbody>
            </table>
          </div>
        </section>
      </div>
      {deleteTarget ? (
        <div className="fixed inset-0 z-50 grid place-items-center bg-background/80 px-4 backdrop-blur-sm">
          <div className="w-full max-w-md rounded-lg border border-border bg-card p-5 shadow-xl">
            <div className="text-base font-semibold">Delete user</div>
            <p className="mt-2 text-sm text-muted-foreground">
              Delete {deleteTarget.username || deleteTarget.email}? This action cannot be restored,
              and the username and email will remain reserved.
            </p>
            <div className="mt-5 flex justify-end gap-2">
              <Button
                variant="outline"
                disabled={deleting}
                onClick={() => setDeleteTarget(null)}
              >
                Cancel
              </Button>
              <Button variant="destructive" disabled={deleting} onClick={() => void deleteUser()}>
                {deleting ? <LoaderCircle className="mr-2 size-4 animate-spin" /> : null}
                Delete
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </main>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-2xl font-semibold">{value}</div>
    </div>
  );
}

function TextInput({
  label,
  value,
  onChange,
  type = "text",
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: string;
}) {
  return (
    <label className="grid gap-1 text-xs text-muted-foreground">
      {label}
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-10 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none focus:border-ring"
      />
    </label>
  );
}

function RolePill({ role }: { role: Role }) {
  const Icon = role === "admin" ? ShieldCheck : Shield;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-1 text-xs font-medium",
        role === "admin"
          ? "bg-amber-500/15 text-amber-700 dark:text-amber-300"
          : "bg-muted text-muted-foreground",
      )}
    >
      <Icon className="size-3.5" />
      {role}
    </span>
  );
}

function StatusPill({ enabled }: { enabled: boolean }) {
  return (
    <span
      className={cn(
        "inline-flex rounded-full px-2 py-1 text-xs font-medium",
        enabled
          ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300"
          : "bg-destructive/10 text-destructive",
      )}
    >
      {enabled ? "Enabled" : "Disabled"}
    </span>
  );
}

function formatTime(value?: string) {
  if (!value) return "-";
  const ts = Date.parse(value);
  if (Number.isNaN(ts)) return value;
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(ts));
}

async function responseError(res: Response) {
  try {
    const body = (await res.json()) as { error?: string | { code?: string; message?: string } };
    if (typeof body.error === "string" && body.error) return body.error;
    if (body.error?.code === "PROTECTED_ADMIN") return "Bootstrap admin cannot be changed.";
    if (body.error && typeof body.error.message === "string" && body.error.message) {
      return body.error.message;
    }
    return `${res.status} ${res.statusText}`;
  } catch {
    return `${res.status} ${res.statusText}`;
  }
}

function isAccountDisabledResponse(res: Response) {
  return res.headers.get("x-cocola-auth") === "account-disabled";
}

function redirectAccountDisabled() {
  void signOut({ callbackUrl: "/login?reason=account_disabled" });
}

function isProtectedAdmin(user: AuthUser) {
  return user.created_by === "bootstrap";
}
