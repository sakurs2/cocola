"use client";

import type { SessionUser } from "@/lib/server-auth";
import { AlertCircle, CheckCircle2, KeyRound, Save, UserRound } from "lucide-react";
import { useRouter } from "next/navigation";
import { FormEvent, useMemo, useState } from "react";

type Notice = { tone: "success" | "error"; message: string } | null;

export function AccountSettingsPanel({ initialAccount }: { initialAccount: SessionUser }) {
  const router = useRouter();
  const [account, setAccount] = useState(initialAccount);
  const [profile, setProfile] = useState({
    name: initialAccount.name,
    username: initialAccount.username,
    email: initialAccount.email,
    currentPassword: "",
  });
  const [password, setPassword] = useState({ current: "", next: "", confirm: "" });
  const [savingProfile, setSavingProfile] = useState(false);
  const [savingPassword, setSavingPassword] = useState(false);
  const [profileNotice, setProfileNotice] = useState<Notice>(null);
  const [passwordNotice, setPasswordNotice] = useState<Notice>(null);
  const emailChanged = profile.email.trim().toLowerCase() !== account.email.toLowerCase();
  const profileChanged = useMemo(
    () =>
      profile.name.trim() !== account.name ||
      profile.username.trim().toLowerCase() !== account.username ||
      emailChanged,
    [account, emailChanged, profile],
  );

  async function saveProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setProfileNotice(null);
    if (emailChanged && !profile.currentPassword) {
      setProfileNotice({ tone: "error", message: "Enter your current password to change email." });
      return;
    }
    setSavingProfile(true);
    try {
      const response = await fetch("/api/account", {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name: profile.name.trim(),
          username: profile.username.trim(),
          email: profile.email.trim(),
          current_password: profile.currentPassword,
          expected_version: account.version,
        }),
      });
      const body = await response.json();
      if (!response.ok) throw new Error(accountError(body, "Could not save account details."));
      const next = body as SessionUser;
      setAccount(next);
      setProfile({
        name: next.name,
        username: next.username,
        email: next.email,
        currentPassword: "",
      });
      setProfileNotice({ tone: "success", message: "Account details updated." });
      router.refresh();
    } catch (error) {
      setProfileNotice({
        tone: "error",
        message: error instanceof Error ? error.message : "Could not save account details.",
      });
    } finally {
      setSavingProfile(false);
    }
  }

  async function changePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPasswordNotice(null);
    if (password.next.length < 8) {
      setPasswordNotice({ tone: "error", message: "New password must be at least 8 characters." });
      return;
    }
    if (password.next !== password.confirm) {
      setPasswordNotice({ tone: "error", message: "New passwords do not match." });
      return;
    }
    setSavingPassword(true);
    try {
      const response = await fetch("/api/account/password", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          current_password: password.current,
          new_password: password.next,
          expected_version: account.version,
        }),
      });
      const body = await response.json();
      if (!response.ok) throw new Error(accountError(body, "Could not change password."));
      setAccount(body as SessionUser);
      setPassword({ current: "", next: "", confirm: "" });
      setPasswordNotice({ tone: "success", message: "Password changed." });
      router.refresh();
    } catch (error) {
      setPasswordNotice({
        tone: "error",
        message: error instanceof Error ? error.message : "Could not change password.",
      });
    } finally {
      setSavingPassword(false);
    }
  }

  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,1.25fr)_minmax(300px,0.75fr)]">
      <section className="rounded-2xl border border-border bg-card shadow-card">
        <div className="flex items-center gap-3 border-b border-border px-4 py-3">
          <div className="grid size-8 place-items-center rounded-xl bg-sky-500/10">
            <UserRound className="size-4 text-sky-600" />
          </div>
          <div>
            <h2 className="text-sm font-semibold">Personal information</h2>
            <p className="text-xs text-muted-foreground">Used for your account and Git commits.</p>
          </div>
        </div>
        <form className="space-y-4 p-4" onSubmit={saveProfile}>
          <AccountField
            label="Display name"
            value={profile.name}
            onChange={(name) => setProfile((current) => ({ ...current, name }))}
            autoComplete="name"
            maxLength={128}
          />
          <AccountField
            label="Username"
            value={profile.username}
            onChange={(username) => setProfile((current) => ({ ...current, username }))}
            autoComplete="username"
            maxLength={64}
            hint="Your sign-in name. It must be unique."
          />
          <AccountField
            label="Email"
            type="email"
            value={profile.email}
            onChange={(email) => setProfile((current) => ({ ...current, email }))}
            autoComplete="email"
            maxLength={254}
            hint="Used for sign-in and as git user.email."
          />
          {emailChanged ? (
            <AccountField
              label="Current password"
              type="password"
              value={profile.currentPassword}
              onChange={(currentPassword) =>
                setProfile((current) => ({ ...current, currentPassword }))
              }
              autoComplete="current-password"
              hint="Required because your email is changing."
            />
          ) : null}
          <NoticeLine notice={profileNotice} />
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
            <span className="font-mono text-[11px] text-muted-foreground">ID {account.id}</span>
            <button
              type="submit"
              disabled={!profileChanged || savingProfile}
              className="inline-flex h-9 items-center gap-2 rounded-xl bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Save className="size-4" />
              {savingProfile ? "Saving…" : "Save changes"}
            </button>
          </div>
        </form>
      </section>

      <section className="rounded-2xl border border-border bg-card shadow-card">
        <div className="flex items-center gap-3 border-b border-border px-4 py-3">
          <div className="grid size-8 place-items-center rounded-xl bg-amber-500/10">
            <KeyRound className="size-4 text-amber-600" />
          </div>
          <div>
            <h2 className="text-sm font-semibold">Sign-in security</h2>
            <p className="text-xs text-muted-foreground">Change the password for this account.</p>
          </div>
        </div>
        <form className="space-y-4 p-4" onSubmit={changePassword}>
          <AccountField
            label="Current password"
            type="password"
            value={password.current}
            onChange={(current) => setPassword((value) => ({ ...value, current }))}
            autoComplete="current-password"
          />
          <AccountField
            label="New password"
            type="password"
            value={password.next}
            onChange={(next) => setPassword((value) => ({ ...value, next }))}
            autoComplete="new-password"
            hint="Use at least 8 characters."
          />
          <AccountField
            label="Confirm new password"
            type="password"
            value={password.confirm}
            onChange={(confirm) => setPassword((value) => ({ ...value, confirm }))}
            autoComplete="new-password"
          />
          <NoticeLine notice={passwordNotice} />
          <div className="flex justify-end border-t border-border pt-4">
            <button
              type="submit"
              disabled={savingPassword || !password.current || !password.next || !password.confirm}
              className="inline-flex h-9 items-center gap-2 rounded-xl border border-input bg-background px-3 text-sm font-medium hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
            >
              <KeyRound className="size-4" />
              {savingPassword ? "Changing…" : "Change password"}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}

function AccountField({
  label,
  value,
  onChange,
  hint,
  type = "text",
  autoComplete,
  maxLength,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  hint?: string;
  type?: "text" | "email" | "password";
  autoComplete: string;
  maxLength?: number;
}) {
  return (
    <label className="block space-y-1.5">
      <span className="text-sm font-medium">{label}</span>
      <input
        type={type}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        autoComplete={autoComplete}
        maxLength={maxLength}
        required
        className="h-10 w-full rounded-xl border border-input bg-background px-3 text-sm outline-none transition-shadow placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring"
      />
      {hint ? <span className="block text-xs text-muted-foreground">{hint}</span> : null}
    </label>
  );
}

function NoticeLine({ notice }: { notice: Notice }) {
  if (!notice) return null;
  const Icon = notice.tone === "success" ? CheckCircle2 : AlertCircle;
  return (
    <div
      role={notice.tone === "error" ? "alert" : "status"}
      className={
        notice.tone === "success"
          ? "flex items-start gap-2 text-sm text-emerald-600"
          : "flex items-start gap-2 text-sm text-destructive"
      }
    >
      <Icon className="mt-0.5 size-4 shrink-0" />
      <span>{notice.message}</span>
    </div>
  );
}

function accountError(body: unknown, fallback: string): string {
  const envelope = body as { error?: { code?: string; message?: string } };
  if (envelope?.error?.code === "VERSION_CONFLICT") {
    return "Your account changed in another tab. Refresh and try again.";
  }
  return envelope?.error?.message || fallback;
}
