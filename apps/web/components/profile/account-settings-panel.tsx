"use client";

import { Button, Card, Chip, Input, Label, TextField } from "@heroui/react";
import { Sheet } from "@heroui-pro/react/sheet";
import type { SessionUser } from "@/lib/server-auth";
import { AlertCircle, CheckCircle2, KeyRound, Lock, Save } from "lucide-react";
import { useRouter } from "next/navigation";
import { useMemo, useState, type FormEvent } from "react";

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
  const [passwordSheetOpen, setPasswordSheetOpen] = useState(false);
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
  const passwordValid =
    password.current.length > 0 && password.next.length >= 8 && password.next === password.confirm;

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
      setProfile({ name: next.name, username: next.username, email: next.email, currentPassword: "" });
      setProfileNotice({ tone: "success", message: "Account details updated." });
      router.refresh();
    } catch (cause) {
      setProfileNotice({
        tone: "error",
        message: cause instanceof Error ? cause.message : "Could not save account details.",
      });
    } finally {
      setSavingProfile(false);
    }
  }

  async function changePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPasswordNotice(null);
    if (!passwordValid) return;
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
      setPasswordSheetOpen(false);
      setProfileNotice({ tone: "success", message: "Password changed." });
      router.refresh();
    } catch (cause) {
      setPasswordNotice({
        tone: "error",
        message: cause instanceof Error ? cause.message : "Could not change password.",
      });
    } finally {
      setSavingPassword(false);
    }
  }

  const setPasswordOpen = (open: boolean) => {
    if (savingPassword) return;
    setPasswordSheetOpen(open);
    if (!open) {
      setPassword({ current: "", next: "", confirm: "" });
      setPasswordNotice(null);
    }
  };

  return (
    <>
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1.25fr)_minmax(300px,0.75fr)]">
        <Card className="p-5">
          <form onSubmit={saveProfile}>
            <Card.Header className="p-0">
              <Card.Title>Personal information</Card.Title>
              <Card.Description>Update your display name and sign-in identity.</Card.Description>
            </Card.Header>
            <Card.Content className="mt-5 grid gap-4 p-0 sm:grid-cols-2">
              <AccountField
                autoComplete="name"
                label="Display name"
                maxLength={128}
                value={profile.name}
                onChange={(name) => setProfile((current) => ({ ...current, name }))}
              />
              <AccountField
                autoComplete="username"
                label="Username"
                maxLength={64}
                value={profile.username}
                onChange={(username) => setProfile((current) => ({ ...current, username }))}
              />
              <AccountField
                autoComplete="email"
                className="sm:col-span-2"
                label="Email"
                maxLength={254}
                type="email"
                value={profile.email}
                onChange={(email) => setProfile((current) => ({ ...current, email }))}
              />
              {emailChanged ? (
                <AccountField
                  autoComplete="current-password"
                  className="sm:col-span-2"
                  label="Current password"
                  type="password"
                  value={profile.currentPassword}
                  onChange={(currentPassword) =>
                    setProfile((current) => ({ ...current, currentPassword }))
                  }
                />
              ) : null}
              <NoticeLine notice={profileNotice} />
            </Card.Content>
            <Card.Footer className="mt-5 justify-between p-0">
              <span className="text-muted text-xs">ID · {account.id}</span>
              <Button
                isDisabled={!profileChanged || savingProfile || (emailChanged && !profile.currentPassword)}
                isPending={savingProfile}
                type="submit"
                variant="primary"
              >
                <Save className="size-4" />
                {savingProfile ? "Saving…" : "Save changes"}
              </Button>
            </Card.Footer>
          </form>
        </Card>

        <Card className="self-start p-5">
          <Card.Header className="p-0">
            <Card.Title>Sign-in security</Card.Title>
            <Card.Description>Manage how you sign in to Cocola.</Card.Description>
          </Card.Header>
          <Card.Content className="mt-5 p-0">
            <div className="bg-surface-secondary flex items-center gap-3 rounded-2xl p-4">
              <span className="bg-accent-soft text-accent flex size-10 shrink-0 items-center justify-center rounded-2xl">
                <Lock className="size-4" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-semibold">Password</span>
                <span className="text-muted mt-0.5 block text-xs">At least 8 characters</span>
              </span>
              <Chip color="success" size="sm" variant="soft">Configured</Chip>
            </div>
          </Card.Content>
          <Card.Footer className="mt-4 justify-end p-0">
            <Button variant="outline" onPress={() => setPasswordOpen(true)}>Change password</Button>
          </Card.Footer>
        </Card>
      </div>

      <Sheet isOpen={passwordSheetOpen} placement="right" onOpenChange={setPasswordOpen}>
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[440px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label="Close password settings" />
              <Sheet.Header>
                <Sheet.Heading>Change password</Sheet.Heading>
                <p className="text-muted text-sm leading-6">
                  Use at least 8 characters and avoid reusing a password from another service.
                </p>
              </Sheet.Header>
              <Sheet.Body>
                <form className="grid gap-4" id="change-password-form" onSubmit={changePassword}>
                  <AccountField
                    autoComplete="current-password"
                    label="Current password"
                    type="password"
                    value={password.current}
                    onChange={(current) => setPassword((value) => ({ ...value, current }))}
                  />
                  <AccountField
                    autoComplete="new-password"
                    label="New password"
                    type="password"
                    value={password.next}
                    onChange={(next) => setPassword((value) => ({ ...value, next }))}
                  />
                  <AccountField
                    autoComplete="new-password"
                    label="Confirm password"
                    type="password"
                    value={password.confirm}
                    onChange={(confirm) => setPassword((value) => ({ ...value, confirm }))}
                  />
                  {password.confirm && password.next !== password.confirm ? (
                    <p className="text-danger text-xs">The passwords do not match.</p>
                  ) : null}
                  <NoticeLine notice={passwordNotice} />
                </form>
              </Sheet.Body>
              <Sheet.Footer className="gap-2">
                <Button variant="outline" onPress={() => setPasswordOpen(false)}>Cancel</Button>
                <Button
                  form="change-password-form"
                  isDisabled={!passwordValid || savingPassword}
                  isPending={savingPassword}
                  type="submit"
                  variant="primary"
                >
                  <KeyRound className="size-4" />
                  Change password
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
    </>
  );
}

function AccountField({
  autoComplete,
  className,
  label,
  maxLength,
  type = "text",
  value,
  onChange,
}: {
  autoComplete: string;
  className?: string;
  label: string;
  maxLength?: number;
  type?: "text" | "email" | "password";
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <TextField className={className} isRequired value={value} variant="secondary" onChange={onChange}>
      <Label>{label}</Label>
      <Input autoComplete={autoComplete} maxLength={maxLength} type={type} />
    </TextField>
  );
}

function NoticeLine({ notice }: { notice: Notice }) {
  if (!notice) return null;
  const Icon = notice.tone === "success" ? CheckCircle2 : AlertCircle;
  return (
    <div
      className={notice.tone === "success" ? "text-success flex items-start gap-2 text-sm" : "text-danger flex items-start gap-2 text-sm"}
      role={notice.tone === "error" ? "alert" : "status"}
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
