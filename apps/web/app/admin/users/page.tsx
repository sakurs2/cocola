"use client";

import { UsersThree as UsersPageIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import {
  AdminAlert,
  AdminDrawer,
  AdminMetric,
  AdminRefreshButton,
  AdminTable,
  AdminToolbar,
} from "@/components/admin/admin-ui";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import {
  CheckCircle2,
  Copy,
  KeyRound,
  LoaderCircle,
  MoreHorizontal,
  Power,
  Search,
  Shield,
  ShieldCheck,
  Trash2,
  UserCog,
  UserPlus,
} from "lucide-react";
import { signOut, useSession } from "next-auth/react";
import { useCallback, useEffect, useMemo, useState } from "react";

type Role = "user" | "admin";

type AuthUser = {
  id: string;
  username: string;
  email: string;
  name?: string;
  tenant_id?: string;
  role: Role;
  enabled: boolean;
  created_by?: string;
  created_at?: string;
  updated_at?: string;
  last_login_at?: string;
};

type RoleFilter = "all" | Role;
type StatusFilter = "all" | "enabled" | "disabled";

type DrawerMode = "create" | "edit";

type UserForm = {
  username: string;
  email: string;
  tenant: string;
  role: Role;
  autoPassword: boolean;
  password: string;
};

const EMPTY_FORM: UserForm = {
  username: "",
  email: "",
  tenant: "",
  role: "user",
  autoPassword: true,
  password: "",
};

const NEW_TEAM = "__new__";

export default function AdminUsersPage() {
  const { data: session } = useSession();
  const [users, setUsers] = useState<AuthUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [actingId, setActingId] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AuthUser | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  // Filters
  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState<RoleFilter>("all");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");

  // Create / edit drawer
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerMode, setDrawerMode] = useState<DrawerMode>("create");
  const [editTarget, setEditTarget] = useState<AuthUser | null>(null);
  const [form, setForm] = useState<UserForm>(EMPTY_FORM);
  const [teamChoice, setTeamChoice] = useState<string>("");
  const [saving, setSaving] = useState(false);

  // Reset-password drawer
  const [resetTarget, setResetTarget] = useState<AuthUser | null>(null);
  const [resetAuto, setResetAuto] = useState(true);
  const [resetPasswordValue, setResetPasswordValue] = useState("");
  const [resetting, setResetting] = useState(false);

  // One-time credential reveal (after create / reset)
  const [credential, setCredential] = useState<{ email: string; password: string } | null>(null);

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

  const teams = useMemo(() => {
    const set = new Set<string>();
    for (const u of users) {
      const t = (u.tenant_id ?? "").trim();
      if (t) set.add(t);
    }
    return Array.from(set).sort((a, b) => a.localeCompare(b));
  }, [users]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return users.filter((u) => {
      if (roleFilter !== "all" && u.role !== roleFilter) return false;
      if (statusFilter === "enabled" && !u.enabled) return false;
      if (statusFilter === "disabled" && u.enabled) return false;
      if (!q) return true;
      return (
        u.username.toLowerCase().includes(q) ||
        u.email.toLowerCase().includes(q) ||
        (u.tenant_id ?? "").toLowerCase().includes(q)
      );
    });
  }, [users, query, roleFilter, statusFilter]);

  const currentUserEmail = session?.user?.email ?? "";

  const openCreate = () => {
    setDrawerMode("create");
    setEditTarget(null);
    setForm(EMPTY_FORM);
    setTeamChoice("");
    setError("");
    setDrawerOpen(true);
  };

  const openEdit = (user: AuthUser) => {
    setDrawerMode("edit");
    setEditTarget(user);
    setForm({
      username: user.username,
      email: user.email,
      tenant: user.tenant_id ?? "",
      role: user.role,
      autoPassword: true,
      password: "",
    });
    setTeamChoice(user.tenant_id ? user.tenant_id : "");
    setError("");
    setDrawerOpen(true);
  };

  const closeDrawer = () => {
    setDrawerOpen(false);
    setEditTarget(null);
  };

  const submitDrawer = async () => {
    setError("");
    setNotice("");
    setSaving(true);
    try {
      const tenant = form.tenant.trim();
      if (drawerMode === "create") {
        const password = form.autoPassword ? generatePassword() : form.password;
        if (!password) {
          setError("Password is required");
          setSaving(false);
          return;
        }
        const res = await fetch("/api/admin/users", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            username: form.username.trim(),
            email: form.email.trim(),
            tenant_id: tenant,
            role: form.role,
            password,
            enabled: true,
          }),
        });
        if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
        if (!res.ok) throw new Error(await responseError(res));
        closeDrawer();
        setNotice("User created");
        setCredential({ email: form.email.trim(), password });
        await refresh();
      } else if (editTarget) {
        const res = await fetch(`/api/admin/users/${encodeURIComponent(editTarget.id)}`, {
          method: "PATCH",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            username: form.username.trim(),
            email: form.email.trim(),
            tenant_id: tenant,
            role: form.role,
          }),
        });
        if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
        if (!res.ok) throw new Error(await responseError(res));
        closeDrawer();
        setNotice("User updated");
        await refresh();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const patchUser = async (
    user: AuthUser,
    patch: Partial<Pick<AuthUser, "role" | "enabled">>,
    successMsg: string,
  ) => {
    setError("");
    setNotice("");
    setActingId(user.id);
    try {
      const res = await fetch(`/api/admin/users/${encodeURIComponent(user.id)}`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(patch),
      });
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (!res.ok) throw new Error(await responseError(res));
      setNotice(successMsg);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActingId(null);
    }
  };

  const openReset = (user: AuthUser) => {
    setResetTarget(user);
    setResetAuto(true);
    setResetPasswordValue("");
    setError("");
  };

  const submitReset = async () => {
    if (!resetTarget) return;
    const password = resetAuto ? generatePassword() : resetPasswordValue.trim();
    if (!password) {
      setError("Password is required");
      return;
    }
    setError("");
    setNotice("");
    setResetting(true);
    setActingId(resetTarget.id);
    try {
      const res = await fetch(`/api/admin/users/${encodeURIComponent(resetTarget.id)}/password`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ password }),
      });
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (!res.ok) throw new Error(await responseError(res));
      const email = resetTarget.email;
      setResetTarget(null);
      setNotice("Password reset");
      setCredential({ email, password });
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setResetting(false);
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

  const canSubmitDrawer =
    Boolean(form.username.trim()) &&
    Boolean(form.email.trim()) &&
    (drawerMode === "edit" || form.autoPassword || Boolean(form.password));

  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <div className="admin-page-icon">
            <UsersPageIcon className="size-[18px]" weight="duotone" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">Users</h1>
            <p className="truncate text-xs text-muted-foreground">
              Whitelist accounts and admin access
            </p>
          </div>
          <AdminRefreshButton
            variant="outline"
            size="sm"
            onClick={() => void refresh()}
            disabled={loading}
            refreshing={loading}
          >
            Refresh
          </AdminRefreshButton>
        </div>
      </header>

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-6">
        {error && <AdminAlert tone="error">{error}</AdminAlert>}
        {notice && (
          <AdminAlert tone="success" icon={<CheckCircle2 className="size-4" />}>
            {notice}
          </AdminAlert>
        )}

        <section className="grid gap-3 md:grid-cols-3">
          <AdminMetric label="Users" value={String(stats.total)} tone="sky" />
          <AdminMetric label="Enabled" value={String(stats.enabled)} tone="green" />
          <AdminMetric label="Admins" value={String(stats.admins)} tone="violet" />
        </section>

        {/* Toolbar: search + filters + create */}
        <AdminToolbar>
          <div className="relative min-w-[240px] flex-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search username, email, or team"
              className="h-10 w-full rounded-md border border-input bg-background pl-9 pr-3 text-sm outline-none focus:border-ring"
            />
          </div>
          <select
            value={roleFilter}
            onChange={(e) => setRoleFilter(e.target.value as RoleFilter)}
            className="h-10 rounded-md border border-input bg-background px-3 text-sm outline-none focus:border-ring"
          >
            <option value="all">All roles</option>
            <option value="user">user</option>
            <option value="admin">admin</option>
          </select>
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
            className="h-10 rounded-md border border-input bg-background px-3 text-sm outline-none focus:border-ring"
          >
            <option value="all">All statuses</option>
            <option value="enabled">Enabled</option>
            <option value="disabled">Disabled</option>
          </select>
          <Button onClick={openCreate}>
            <UserPlus className="mr-2 size-4" />
            New user
          </Button>
        </AdminToolbar>

        <AdminTable>
          <table className="w-full min-w-[720px] text-sm">
            <thead className="border-b border-border bg-muted/50 text-xs text-muted-foreground">
              <tr>
                <th className="px-4 py-3 text-left font-medium">User</th>
                <th className="px-4 py-3 text-left font-medium">Role</th>
                <th className="px-4 py-3 text-left font-medium">Team</th>
                <th className="px-4 py-3 text-left font-medium">Status</th>
                <th className="px-4 py-3 text-left font-medium">Last login</th>
                <th className="px-4 py-3 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {loading && users.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-10 text-center text-muted-foreground">
                    Loading users...
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-10 text-center text-muted-foreground">
                    {users.length === 0 ? "No users found" : "No users match your filters"}
                  </td>
                </tr>
              ) : (
                filtered.map((user) => {
                  const busy = actingId === user.id;
                  const protectedAdmin = isProtectedAdmin(user);
                  const selfUser = isCurrentUser(user, currentUserEmail);
                  const roleLocked = selfUser || (protectedAdmin && user.role === "admin");
                  const disableLocked = selfUser || (protectedAdmin && user.enabled);
                  const deleteLocked = selfUser || protectedAdmin;
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
                        {user.tenant_id ? (
                          <span className="text-sm">{user.tenant_id}</span>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="px-4 py-3">
                        <StatusPill enabled={user.enabled} />
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        {formatTime(user.last_login_at)}
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex justify-end">
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button
                                variant="ghost"
                                size="icon"
                                className="size-9"
                                disabled={busy}
                                aria-label="Actions"
                              >
                                {busy ? (
                                  <LoaderCircle className="size-4 animate-spin" />
                                ) : (
                                  <MoreHorizontal className="size-4" />
                                )}
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem onSelect={() => openEdit(user)}>
                                <UserCog className="size-4" />
                                Edit
                              </DropdownMenuItem>
                              <DropdownMenuItem onSelect={() => openReset(user)}>
                                <KeyRound className="size-4" />
                                Reset password
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                disabled={roleLocked}
                                onSelect={() =>
                                  void patchUser(
                                    user,
                                    { role: user.role === "admin" ? "user" : "admin" },
                                    "User updated",
                                  )
                                }
                              >
                                <ShieldCheck className="size-4" />
                                {user.role === "admin" ? "Make user" : "Make admin"}
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                disabled={disableLocked}
                                onSelect={() =>
                                  void patchUser(
                                    user,
                                    { enabled: !user.enabled },
                                    user.enabled ? "User disabled" : "User enabled",
                                  )
                                }
                              >
                                <Power className="size-4" />
                                {user.enabled ? "Disable" : "Enable"}
                              </DropdownMenuItem>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem
                                variant="destructive"
                                disabled={deleteLocked}
                                onSelect={() => setDeleteTarget(user)}
                              >
                                <Trash2 className="size-4" />
                                Delete
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </div>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </AdminTable>
      </div>

      {/* Create / edit drawer */}
      <AdminDrawer
        open={drawerOpen}
        onOpenChange={(open) => {
          if (!open) closeDrawer();
        }}
        title={drawerMode === "create" ? "Create user" : "Edit user"}
        description={
          drawerMode === "create" ? "Passwords are stored as bcrypt hashes." : editTarget?.email
        }
        footer={
          <div className="flex justify-end gap-2">
            <Button variant="outline" disabled={saving} onClick={closeDrawer}>
              Cancel
            </Button>
            <Button disabled={saving || !canSubmitDrawer} onClick={() => void submitDrawer()}>
              {saving ? <LoaderCircle className="mr-2 size-4 animate-spin" /> : null}
              {drawerMode === "create" ? "Create" : "Save"}
            </Button>
          </div>
        }
      >
        <div className="space-y-4">
          <FieldInput
            label="Username"
            value={form.username}
            onChange={(username) => setForm((p) => ({ ...p, username }))}
          />
          <FieldInput
            label="Email"
            type="email"
            value={form.email}
            onChange={(email) => setForm((p) => ({ ...p, email }))}
          />

          <label className="grid gap-1 text-xs text-muted-foreground">
            Team
            <select
              value={teamChoice === "" && form.tenant ? form.tenant : teamChoice}
              onChange={(e) => {
                const v = e.target.value;
                setTeamChoice(v);
                if (v === NEW_TEAM) {
                  setForm((p) => ({ ...p, tenant: "" }));
                } else {
                  setForm((p) => ({ ...p, tenant: v }));
                }
              }}
              className="h-10 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none focus:border-ring"
            >
              <option value="">No team</option>
              {teams.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
              <option value={NEW_TEAM}>+ New team…</option>
            </select>
          </label>
          {teamChoice === NEW_TEAM ? (
            <FieldInput
              label="New team name"
              value={form.tenant}
              onChange={(tenant) => setForm((p) => ({ ...p, tenant }))}
            />
          ) : null}

          <label className="grid gap-1 text-xs text-muted-foreground">
            Role
            <select
              value={form.role}
              onChange={(e) => setForm((p) => ({ ...p, role: e.target.value as Role }))}
              className="h-10 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none focus:border-ring"
            >
              <option value="user">user</option>
              <option value="admin">admin</option>
            </select>
          </label>

          {drawerMode === "create" ? (
            <div className="space-y-2 rounded-md border border-border bg-muted/30 p-3">
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={form.autoPassword}
                  onChange={(e) => setForm((p) => ({ ...p, autoPassword: e.target.checked }))}
                  className="size-4 rounded border-input"
                />
                Auto-generate initial password
              </label>
              {form.autoPassword ? (
                <p className="text-xs text-muted-foreground">
                  A strong password is generated on create and shown once so you can copy it.
                </p>
              ) : (
                <FieldInput
                  label="Password"
                  type="password"
                  value={form.password}
                  onChange={(password) => setForm((p) => ({ ...p, password }))}
                />
              )}
            </div>
          ) : null}
        </div>
      </AdminDrawer>

      {/* Reset-password drawer */}
      {resetTarget ? (
        <div className="fixed inset-0 z-50 grid place-items-center bg-background/80 px-4 backdrop-blur-sm">
          <div className="w-full max-w-md rounded-lg border border-border bg-card p-5 shadow-xl">
            <div className="text-base font-semibold">Reset password</div>
            <p className="mt-1 text-sm text-muted-foreground">
              {resetTarget.username || resetTarget.email}
            </p>
            <div className="mt-4 space-y-3">
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={resetAuto}
                  onChange={(e) => setResetAuto(e.target.checked)}
                  className="size-4 rounded border-input"
                />
                Auto-generate new password
              </label>
              {resetAuto ? (
                <p className="text-xs text-muted-foreground">
                  A strong password is generated and shown once so you can copy it.
                </p>
              ) : (
                <FieldInput
                  label="New password"
                  type="password"
                  value={resetPasswordValue}
                  onChange={setResetPasswordValue}
                />
              )}
            </div>
            <div className="mt-5 flex justify-end gap-2">
              <Button variant="outline" disabled={resetting} onClick={() => setResetTarget(null)}>
                Cancel
              </Button>
              <Button disabled={resetting} onClick={() => void submitReset()}>
                {resetting ? <LoaderCircle className="mr-2 size-4 animate-spin" /> : null}
                Reset
              </Button>
            </div>
          </div>
        </div>
      ) : null}

      {/* One-time credential reveal */}
      {credential ? (
        <div className="fixed inset-0 z-[60] grid place-items-center bg-background/80 px-4 backdrop-blur-sm">
          <div className="w-full max-w-md rounded-lg border border-border bg-card p-5 shadow-xl">
            <div className="flex items-center gap-2 text-base font-semibold">
              <CheckCircle2 className="size-5 text-emerald-500" />
              Password ready
            </div>
            <p className="mt-2 text-sm text-muted-foreground">
              Copy this password now — it will not be shown again.
            </p>
            <div className="mt-4 space-y-2">
              <CredentialRow label="Email" value={credential.email} />
              <CredentialRow label="Password" value={credential.password} mono />
            </div>
            <div className="mt-5 flex justify-end gap-2">
              <Button
                variant="outline"
                onClick={() => void copyText(`${credential.email} / ${credential.password}`)}
              >
                <Copy className="mr-2 size-4" />
                Copy both
              </Button>
              <Button onClick={() => setCredential(null)}>Done</Button>
            </div>
          </div>
        </div>
      ) : null}

      {/* Delete confirm */}
      {deleteTarget ? (
        <div className="fixed inset-0 z-50 grid place-items-center bg-background/80 px-4 backdrop-blur-sm">
          <div className="w-full max-w-md rounded-lg border border-border bg-card p-5 shadow-xl">
            <div className="text-base font-semibold">Delete user</div>
            <p className="mt-2 text-sm text-muted-foreground">
              Delete {deleteTarget.username || deleteTarget.email}? This action cannot be restored,
              and the username and email will remain reserved.
            </p>
            <div className="mt-5 flex justify-end gap-2">
              <Button variant="outline" disabled={deleting} onClick={() => setDeleteTarget(null)}>
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

function FieldInput({
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

function CredentialRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-center gap-2 rounded-md border border-border bg-muted/30 px-3 py-2">
      <div className="w-16 shrink-0 text-xs text-muted-foreground">{label}</div>
      <div className={cn("min-w-0 flex-1 truncate text-sm", mono && "font-mono")}>{value}</div>
      <Button
        variant="ghost"
        size="icon"
        className="size-8"
        aria-label={`Copy ${label}`}
        onClick={async () => {
          await copyText(value);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1500);
        }}
      >
        {copied ? (
          <CheckCircle2 className="size-4 text-emerald-500" />
        ) : (
          <Copy className="size-4" />
        )}
      </Button>
    </div>
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

// Generate a strong, human-copyable password. Uses the Web Crypto API so the
// value is unpredictable; avoids ambiguous characters (0/O, 1/l/I).
function generatePassword(length = 16): string {
  const charset = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*";
  const n = charset.length;
  const out: string[] = [];
  const cryptoObj = globalThis.crypto;
  if (cryptoObj?.getRandomValues) {
    const buf = new Uint32Array(length);
    cryptoObj.getRandomValues(buf);
    for (let i = 0; i < length; i++) out.push(charset.charAt((buf[i] ?? 0) % n));
  } else {
    for (let i = 0; i < length; i++) {
      out.push(charset.charAt(Math.floor(Math.random() * n)));
    }
  }
  return out.join("");
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    // Clipboard may be unavailable (insecure context); silently ignore.
  }
}

async function responseError(res: Response) {
  try {
    const body = (await res.json()) as { error?: string | { code?: string; message?: string } };
    if (typeof body.error === "string" && body.error) return body.error;
    const errorBody = typeof body.error === "object" ? body.error : undefined;
    if (errorBody?.code === "PROTECTED_ADMIN") return "Bootstrap admin cannot be changed.";
    if (errorBody?.code === "SELF_PERMISSION_CHANGE") {
      return "You cannot change your own permissions.";
    }
    if (errorBody?.message) {
      return errorBody.message;
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

function isCurrentUser(user: AuthUser, currentUserEmail: string) {
  return currentUserEmail.trim().toLowerCase() === user.email.trim().toLowerCase();
}
