"use client";

import { Eye, EyeOff } from "lucide-react";
import { Button, Card, Input, Label, TextField } from "@heroui/react";
import { signIn } from "next-auth/react";
import { useTranslations } from "next-intl";
import { useSearchParams } from "next/navigation";
import { FormEvent, Suspense, useState } from "react";

import { CocolaTagline } from "@/components/assistant-ui/cocola-tagline";
import { CocolaWordmark } from "@/components/assistant-ui/cocola-wordmark";
import { CocolaLogo } from "@/components/cocola-logo";
import { LanguageMenu } from "@/components/i18n/language-menu";

export default function LoginPage() {
  return (
    <Suspense>
      <LoginForm />
    </Suspense>
  );
}

function LoginForm() {
  const t = useTranslations("auth.signIn");
  const search = useSearchParams();
  const callbackUrl = safeCallbackPath(search.get("callbackUrl"));
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const initialErrorKey = initialLoginErrorKey(search.get("error"), search.get("reason"));
  const [error, setError] = useState(initialErrorKey ? t(initialErrorKey) : "");
  const [pending, setPending] = useState(false);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setPending(true);
    setError("");
    try {
      const preflight = await fetch("/api/auth/preflight", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ identifier: identifier.trim(), password }),
      });
      if (!preflight.ok) {
        if (
          preflight.status === 403 &&
          preflight.headers.get("x-cocola-auth") === "account-disabled"
        ) {
          setError(t("accountDisabled"));
          return;
        }
        setError(t("invalidCredentials"));
        return;
      }
      const res = await signIn("credentials", {
        identifier: identifier.trim(),
        password,
        redirect: false,
        redirectTo: callbackUrl,
      });
      if (res?.ok && !res.error && res.url) {
        window.location.href = callbackUrl;
        return;
      }
      setError(t("invalidCredentials"));
    } catch {
      setError(t("failed"));
    } finally {
      setPending(false);
    }
  };

  return (
    <main className="cocola-user-ui workspace-grain bg-surface-secondary flex min-h-screen flex-col items-center justify-center gap-8 p-5 text-foreground">
      <div className="absolute right-4 top-4">
        <LanguageMenu />
      </div>
      <div className="flex items-center">
        <div className="flex items-center">
          <CocolaLogo className="h-28 w-28 shrink-0 sm:h-32 sm:w-32" />
          <div className="-ml-6 flex flex-col items-center text-center">
            <CocolaWordmark className="cocola-wordmark -my-4 h-32 w-auto max-w-[min(90vw,460px)] sm:h-36" />
            <CocolaTagline />
          </div>
        </div>
      </div>

      <Card className="w-full max-w-md p-6 sm:p-7">
        <form className="grid gap-5" onSubmit={submit}>
          <Card.Header className="p-0">
            <Card.Title>{t("title")}</Card.Title>
            <Card.Description>{t("description")}</Card.Description>
          </Card.Header>
          <Card.Content className="grid gap-4 p-0">
            <TextField isRequired value={identifier} variant="secondary" onChange={setIdentifier}>
              <Label>{t("identifier")}</Label>
              <Input autoComplete="username" autoFocus />
            </TextField>
            <TextField isRequired value={password} variant="secondary" onChange={setPassword}>
              <Label>{t("password")}</Label>
              <div className="relative">
                <Input
                  autoComplete="current-password"
                  className="w-full pr-11"
                  type={showPassword ? "text" : "password"}
                />
                <Button
                  isIconOnly
                  aria-label={showPassword ? t("hidePassword") : t("showPassword")}
                  className="absolute inset-y-0 right-1 my-auto"
                  size="sm"
                  variant="ghost"
                  onPress={() => setShowPassword((value) => !value)}
                >
                  {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </Button>
              </div>
            </TextField>
            {error ? (
              <div
                role="alert"
                aria-live="polite"
                className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm"
              >
                {error}
              </div>
            ) : null}
          </Card.Content>
          <Card.Footer className="p-0">
            <Button fullWidth isPending={pending} type="submit" variant="primary">
              {pending ? t("submitting") : t("submit")}
            </Button>
          </Card.Footer>
        </form>
      </Card>
    </main>
  );
}

function safeCallbackPath(value: string | null) {
  if (!value) return "/";
  if (value.startsWith("/") && !value.startsWith("//")) return value;
  try {
    const url = new URL(value);
    if (url.origin === window.location.origin) {
      return `${url.pathname}${url.search}${url.hash}`;
    }
  } catch {
    // Fall through to the safe default.
  }
  return "/";
}

function initialLoginErrorKey(
  error: string | null,
  reason: string | null,
): "accountDisabled" | "invalidCredentials" | null {
  if (reason === "account_disabled") {
    return "accountDisabled";
  }
  if (error) return "invalidCredentials";
  return null;
}
