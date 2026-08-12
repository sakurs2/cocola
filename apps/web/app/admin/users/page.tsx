"use client";

import {
  Avatar,
  Button,
  Checkbox,
  Chip,
  Dropdown,
  Input,
  Label,
  SearchField,
  TextField,
} from "@heroui/react";
import { type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Segment } from "@cocola/ui-compat/segment";
import {
  AdminAlert,
  AdminConfirmDialog,
  AdminDataGrid,
  AdminDrawer,
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";
import {
  CheckCircle2,
  Copy,
  KeyRound,
  LoaderCircle,
  MoreHorizontal,
  Power,
  Shield,
  ShieldCheck,
  Trash2,
  UserCog,
  UserPlus,
  Users as UsersPageIcon,
} from "lucide-react";
import { signOut, useSession } from "next-auth/react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useFormatter, useTranslations } from "next-intl";

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

type RoleFilter = "all" | Role;
type StatusFilter = "all" | "enabled" | "disabled";

type DrawerMode = "create" | "edit";

type UserForm = {
  username: string;
  email: string;
  role: Role;
  autoPassword: boolean;
  password: string;
};

const EMPTY_FORM: UserForm = {
  username: "",
  email: "",
  role: "user",
  autoPassword: true,
  password: "",
};

export default function AdminUsersPage() {
  const t = useTranslations("admin.usersPage");
  const format = useFormatter();
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

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return users.filter((u) => {
      if (roleFilter !== "all" && u.role !== roleFilter) return false;
      if (statusFilter === "enabled" && !u.enabled) return false;
      if (statusFilter === "disabled" && u.enabled) return false;
      if (!q) return true;
      return u.username.toLowerCase().includes(q) || u.email.toLowerCase().includes(q);
    });
  }, [users, query, roleFilter, statusFilter]);

  const currentUserEmail = session?.user?.email ?? "";

  const openCreate = () => {
    setDrawerMode("create");
    setEditTarget(null);
    setForm(EMPTY_FORM);
    setError("");
    setDrawerOpen(true);
  };

  const openEdit = (user: AuthUser) => {
    setDrawerMode("edit");
    setEditTarget(user);
    setForm({
      username: user.username,
      email: user.email,
      role: user.role,
      autoPassword: true,
      password: "",
    });
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
      if (drawerMode === "create") {
        const password = form.autoPassword ? generatePassword() : form.password;
        if (!password) {
          setError(t("errors.passwordRequired"));
          setSaving(false);
          return;
        }
        const res = await fetch("/api/admin/users", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            username: form.username.trim(),
            email: form.email.trim(),
            role: form.role,
            password,
            enabled: true,
          }),
        });
        if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
        if (!res.ok) throw new Error(await responseError(res));
        closeDrawer();
        setNotice(t("notices.created"));
        setCredential({ email: form.email.trim(), password });
        await refresh();
      } else if (editTarget) {
        const res = await fetch(`/api/admin/users/${encodeURIComponent(editTarget.id)}`, {
          method: "PATCH",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            username: form.username.trim(),
            email: form.email.trim(),
            role: form.role,
          }),
        });
        if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
        if (!res.ok) throw new Error(await responseError(res));
        closeDrawer();
        setNotice(t("notices.updated"));
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
      setError(t("errors.passwordRequired"));
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
      setNotice(t("notices.passwordReset"));
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
      setNotice(t("notices.deleted"));
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

  const columns: DataGridColumn<AuthUser>[] = [
    {
      id: "user",
      header: t("columns.user"),
      isRowHeader: true,
      minWidth: 320,
      cell: (user) => (
        <span className="flex min-w-0 items-center gap-3 py-1">
          <Avatar className="size-10">
            <Avatar.Fallback>{avatarInitials(user)}</Avatar.Fallback>
          </Avatar>
          <span className="block min-w-0 flex-1">
            <AdminTruncatedValue
              className="text-sm font-semibold"
              copyLabel={t("copy.name")}
              value={user.name || user.username || user.email}
            />
            <AdminTruncatedValue
              className="text-muted mt-0.5 text-xs"
              copyLabel={t("copy.identity")}
              value={`${user.username} · ${user.email}`}
            />
          </span>
        </span>
      ),
    },
    {
      id: "role",
      header: t("columns.role"),
      width: 130,
      cell: (user) => <RolePill role={user.role} />,
    },
    {
      id: "status",
      header: t("columns.status"),
      width: 130,
      cell: (user) => <StatusPill enabled={user.enabled} />,
    },
    {
      id: "login",
      header: t("columns.lastLogin"),
      minWidth: 180,
      cell: (user) => (
        <span className="text-muted text-sm tabular-nums">
          {formatUserTime(user.last_login_at, format)}
        </span>
      ),
    },
    {
      id: "actions",
      header: t("columns.actions"),
      align: "center",
      width: 80,
      cell: (user) => {
        const busy = actingId === user.id;
        const protectedAdmin = isProtectedAdmin(user);
        const selfUser = isCurrentUser(user, currentUserEmail);
        const roleLocked = selfUser || (protectedAdmin && user.role === "admin");
        const disableLocked = selfUser || (protectedAdmin && user.enabled);
        const deleteLocked = selfUser || protectedAdmin;
        return (
          <Dropdown>
            <Dropdown.Trigger
              aria-label={t("actionsFor", { username: user.username })}
              className="text-muted hover:bg-surface-secondary mx-auto grid size-9 place-items-center rounded-xl"
              isDisabled={busy}
            >
              {busy ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <MoreHorizontal className="size-4" />
              )}
            </Dropdown.Trigger>
            <Dropdown.Popover placement="bottom end">
              <Dropdown.Menu
                aria-label={t("actionsFor", { username: user.username })}
                onAction={(key) => {
                  if (key === "edit") openEdit(user);
                  if (key === "reset") openReset(user);
                  if (key === "role")
                    void patchUser(
                      user,
                      { role: user.role === "admin" ? "user" : "admin" },
                      t("notices.updated"),
                    );
                  if (key === "toggle")
                    void patchUser(
                      user,
                      { enabled: !user.enabled },
                      user.enabled ? t("notices.disabled") : t("notices.enabled"),
                    );
                  if (key === "delete") setDeleteTarget(user);
                }}
              >
                <Dropdown.Item id="edit" textValue={t("actions.edit")}>
                  <UserCog className="size-4" />
                  {t("actions.edit")}
                </Dropdown.Item>
                <Dropdown.Item id="reset" textValue={t("actions.resetPassword")}>
                  <KeyRound className="size-4" />
                  {t("actions.resetPassword")}
                </Dropdown.Item>
                <Dropdown.Item
                  id="role"
                  isDisabled={roleLocked}
                  textValue={user.role === "admin" ? t("actions.makeUser") : t("actions.makeAdmin")}
                >
                  <ShieldCheck className="size-4" />
                  {user.role === "admin" ? t("actions.makeUser") : t("actions.makeAdmin")}
                </Dropdown.Item>
                <Dropdown.Item
                  id="toggle"
                  isDisabled={disableLocked}
                  textValue={user.enabled ? t("actions.disable") : t("actions.enable")}
                >
                  <Power className="size-4" />
                  {user.enabled ? t("actions.disable") : t("actions.enable")}
                </Dropdown.Item>
                <Dropdown.Item
                  id="delete"
                  isDisabled={deleteLocked}
                  textValue={t("actions.delete")}
                >
                  <Trash2 className="text-danger size-4" />
                  <span className="text-danger">{t("actions.delete")}</span>
                </Dropdown.Item>
              </Dropdown.Menu>
            </Dropdown.Popover>
          </Dropdown>
        );
      },
    },
  ];

  return (
    <AdminPage>
      <AdminPageHeader
        icon={<UsersPageIcon className="size-5" />}
        title={t("title")}
        description={t("description")}
        actions={
          <Button onPress={openCreate}>
            <UserPlus className="size-4" />
            {t("new")}
          </Button>
        }
      />
      <AdminErrorDialog
        error={error}
        title={t("operationFailed")}
        onDismiss={() => setError("")}
        onRetry={() => void refresh()}
      />
      {notice && (
        <AdminAlert tone="success" icon={<CheckCircle2 className="size-4" />}>
          {notice}
        </AdminAlert>
      )}

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <SearchField
          aria-label={t("searchAria")}
          className="w-full lg:max-w-sm"
          value={query}
          onChange={setQuery}
        >
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input placeholder={t("searchPlaceholder")} />
            <SearchField.ClearButton />
          </SearchField.Group>
        </SearchField>
        <div className="flex flex-wrap gap-2">
          <Segment
            aria-label={t("roleFilter")}
            selectedKey={roleFilter}
            onSelectionChange={(key) => setRoleFilter(String(key) as RoleFilter)}
          >
            <Segment.Item id="all">{t("allRoles")}</Segment.Item>
            <Segment.Item id="user">{t("users")}</Segment.Item>
            <Segment.Item id="admin">{t("admins")}</Segment.Item>
          </Segment>
          <Segment
            aria-label={t("statusFilter")}
            selectedKey={statusFilter}
            onSelectionChange={(key) => setStatusFilter(String(key) as StatusFilter)}
          >
            <Segment.Item id="all">{t("all")}</Segment.Item>
            <Segment.Item id="enabled">{t("enabled")}</Segment.Item>
            <Segment.Item id="disabled">{t("disabled")}</Segment.Item>
          </Segment>
        </div>
      </div>

      <AdminDataGrid
        aria-label={t("tableAria")}
        columns={columns}
        contentClassName="min-w-[840px]"
        data={filtered}
        getRowId={(user) => user.id}
        selectionMode="none"
        variant="primary"
        renderEmptyState={() => (
          <EmptyState>
            <EmptyState.Header>
              <EmptyState.Media variant="icon">
                <UsersPageIcon className="text-blue-500" />
              </EmptyState.Media>
              <EmptyState.Title>
                {loading
                  ? t("empty.loading")
                  : users.length
                    ? t("empty.filtered")
                    : t("empty.none")}
              </EmptyState.Title>
              <EmptyState.Description>
                {loading ? t("empty.loadingDescription") : t("empty.description")}
              </EmptyState.Description>
            </EmptyState.Header>
          </EmptyState>
        )}
      />

      {/* Create / edit drawer */}
      <AdminDrawer
        open={drawerOpen}
        onOpenChange={(open) => {
          if (!open) closeDrawer();
        }}
        title={drawerMode === "create" ? t("drawer.createTitle") : t("drawer.editTitle")}
        description={drawerMode === "create" ? t("drawer.passwordHash") : editTarget?.email}
        footer={
          <div className="admin-theme-blue flex justify-end gap-2">
            <Button variant="outline" isDisabled={saving} onPress={closeDrawer}>
              {t("drawer.cancel")}
            </Button>
            <Button
              isDisabled={saving || !canSubmitDrawer}
              isPending={saving}
              onPress={() => void submitDrawer()}
            >
              {saving ? <LoaderCircle className="mr-2 size-4 animate-spin" /> : null}
              {drawerMode === "create" ? t("drawer.create") : t("drawer.save")}
            </Button>
          </div>
        }
      >
        <div className="admin-theme-blue admin-drawer-form space-y-4">
          <FieldInput
            label={t("drawer.username")}
            value={form.username}
            onChange={(username) => setForm((p) => ({ ...p, username }))}
          />
          <FieldInput
            label={t("drawer.email")}
            type="email"
            value={form.email}
            onChange={(email) => setForm((p) => ({ ...p, email }))}
          />

          <ChoiceDropdown
            label={t("drawer.role")}
            value={form.role}
            options={[
              { id: "user", label: t("drawer.userRole") },
              { id: "admin", label: t("drawer.adminRole") },
            ]}
            onChange={(role) => setForm((current) => ({ ...current, role: role as Role }))}
          />

          {drawerMode === "create" ? (
            <div className="bg-surface-secondary space-y-2 rounded-2xl p-4">
              <Checkbox
                isSelected={form.autoPassword}
                onChange={(selected) =>
                  setForm((current) => ({ ...current, autoPassword: selected }))
                }
              >
                {t("drawer.autoPassword")}
              </Checkbox>
              {form.autoPassword ? (
                <p className="text-xs text-muted">{t("drawer.autoPasswordDescription")}</p>
              ) : (
                <FieldInput
                  label={t("drawer.password")}
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
      <AdminDrawer
        open={Boolean(resetTarget)}
        onOpenChange={(open) => {
          if (!open) setResetTarget(null);
        }}
        title={t("reset.title")}
        description={resetTarget?.username || resetTarget?.email}
        footer={
          <div className="flex justify-end gap-2">
            <Button variant="outline" isDisabled={resetting} onPress={() => setResetTarget(null)}>
              {t("reset.cancel")}
            </Button>
            <Button isPending={resetting} onPress={() => void submitReset()}>
              {t("reset.action")}
            </Button>
          </div>
        }
      >
        <div className="space-y-3">
          <Checkbox isSelected={resetAuto} onChange={setResetAuto}>
            {t("reset.auto")}
          </Checkbox>
          {resetAuto ? (
            <p className="text-xs text-muted">{t("reset.autoDescription")}</p>
          ) : (
            <FieldInput
              label={t("reset.newPassword")}
              type="password"
              value={resetPasswordValue}
              onChange={setResetPasswordValue}
            />
          )}
        </div>
      </AdminDrawer>

      {/* One-time credential reveal */}
      <AdminDrawer
        open={Boolean(credential)}
        onOpenChange={(open) => {
          if (!open) setCredential(null);
        }}
        title={t("credential.title")}
        description={t("credential.description")}
        footer={
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              onPress={() =>
                credential && void copyText(`${credential.email} / ${credential.password}`)
              }
            >
              <Copy className="size-4" />
              {t("credential.copyBoth")}
            </Button>
            <Button onPress={() => setCredential(null)}>{t("credential.done")}</Button>
          </div>
        }
      >
        <div className="space-y-2">
          <CredentialRow label={t("credential.email")} value={credential?.email || ""} />
          <CredentialRow label={t("credential.password")} value={credential?.password || ""} mono />
        </div>
      </AdminDrawer>

      {/* Delete confirm */}
      <AdminConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
        title={t("delete.title")}
        description={t("delete.description", {
          user: deleteTarget?.username || deleteTarget?.email || t("delete.thisUser"),
        })}
        confirmLabel={t("delete.action")}
        busy={deleting}
        destructive
        onConfirm={() => void deleteUser()}
      />
    </AdminPage>
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
    <TextField value={value} variant="secondary" onChange={onChange}>
      <Label>{label}</Label>
      <Input type={type} />
    </TextField>
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
  return (
    <div className="bg-surface-secondary flex items-center gap-2 rounded-2xl px-3 py-2">
      <div className="w-16 shrink-0 text-xs text-muted">{label}</div>
      <AdminTruncatedValue
        className={`text-sm ${mono ? "font-mono" : ""}`}
        copyLabel={label}
        value={value}
      />
    </div>
  );
}

function ChoiceDropdown({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: { id: string; label: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <div>
      <Label>{label}</Label>
      <Dropdown>
        <Dropdown.Trigger
          aria-label={label}
          className="border-separator bg-default hover:bg-default-hover mt-2 flex h-11 w-full items-center justify-between rounded-2xl border px-3 text-sm"
          style={{ transform: "none" }}
        >
          <span>{options.find((option) => option.id === value)?.label || value}</span>
          <MoreHorizontal className="text-muted size-4" />
        </Dropdown.Trigger>
        <Dropdown.Popover placement="bottom start">
          <Dropdown.Menu aria-label={label} onAction={(key) => onChange(String(key))}>
            {options.map((option) => (
              <Dropdown.Item key={option.id} id={option.id} textValue={option.label}>
                {option.label}
              </Dropdown.Item>
            ))}
          </Dropdown.Menu>
        </Dropdown.Popover>
      </Dropdown>
    </div>
  );
}

function avatarInitials(user: AuthUser) {
  const source = (user.name || user.username || user.email || "?").trim();
  if (!source) return "?";
  const parts = source.split(/[\s._-]+/).filter(Boolean);
  if (parts.length >= 2 && parts[0] && parts[1]) {
    return (parts[0].charAt(0) + parts[1].charAt(0)).toUpperCase();
  }
  return source.slice(0, 2).toUpperCase();
}

function RolePill({ role }: { role: Role }) {
  const t = useTranslations("admin.usersPage.roles");
  const Icon = role === "admin" ? ShieldCheck : Shield;
  return (
    <Chip color={role === "admin" ? "accent" : "default"} size="sm" variant="soft">
      <Icon className="size-3.5" />
      {t(role)}
    </Chip>
  );
}

function StatusPill({ enabled }: { enabled: boolean }) {
  const t = useTranslations("admin.usersPage.status");
  return (
    <Chip color={enabled ? "success" : "default"} size="sm" variant="soft">
      {enabled ? t("enabled") : t("disabled")}
    </Chip>
  );
}

function formatUserTime(value: string | undefined, format: ReturnType<typeof useFormatter>) {
  if (!value) return "-";
  const ts = Date.parse(value);
  if (Number.isNaN(ts)) return value;
  return format.dateTime(new Date(ts), {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
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
