"use client";

import { Eye, EyeOff } from "lucide-react";
import { signIn } from "next-auth/react";
import { useSearchParams } from "next/navigation";
import { FormEvent, Suspense, useState } from "react";

import { CocolaTagline } from "@/components/assistant-ui/cocola-tagline";
import { CocolaWordmark } from "@/components/assistant-ui/cocola-wordmark";
import { CocolaLogo } from "@/components/cocola-logo";

export default function LoginPage() {
  return (
    <Suspense>
      <LoginForm />
    </Suspense>
  );
}

function LoginForm() {
  const search = useSearchParams();
  const callbackUrl = safeCallbackPath(search.get("callbackUrl"));
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState(initialLoginError(search.get("error"), search.get("reason")));
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
          setError("This account has been disabled. Contact an administrator.");
          return;
        }
        setError("Sign in failed. Check your username/email and password.");
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
      setError("Sign in failed. Check your username/email and password.");
    } catch {
      setError("Sign in failed. Please try again.");
    } finally {
      setPending(false);
    }
  };

  return (
    <main className="cocola-user-ui workspace-grain cocola-login min-h-screen bg-background text-foreground">
      {/* Brand hero: logo + handwriting wordmark + shimmer tagline (same as home) */}
      <div className="cocola-login-brand">
        <div className="flex items-center">
          <CocolaLogo className="h-28 w-28 shrink-0 sm:h-32 sm:w-32" />
          <div className="-ml-6 flex flex-col items-center text-center">
            <CocolaWordmark className="cocola-wordmark -my-4 h-32 w-auto max-w-[min(90vw,460px)] sm:h-36" />
            <CocolaTagline />
          </div>
        </div>
      </div>

      {/* Login card */}
      <form onSubmit={submit} className="cocola-login-card">
        <div className="space-y-1">
          <h1 className="text-lg font-semibold">Sign in to cocola</h1>
          <p className="text-sm text-muted-foreground">Use an account enabled by an admin.</p>
        </div>
        <label className="space-y-1.5 text-sm">
          <span className="text-muted-foreground">Username or email</span>
          <input
            type="text"
            autoComplete="username"
            value={identifier}
            onChange={(event) => setIdentifier(event.target.value)}
            className="h-10 w-full rounded-xl border border-input bg-background px-3 text-sm outline-none focus:border-ring"
            required
          />
        </label>
        <label className="space-y-1.5 text-sm">
          <span className="text-muted-foreground">Password</span>
          <span className="relative block">
            <input
              type={showPassword ? "text" : "password"}
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              className="h-10 w-full rounded-xl border border-input bg-background px-3 pr-10 text-sm outline-none focus:border-ring"
              required
            />
            <button
              type="button"
              aria-label={showPassword ? "Hide password" : "Show password"}
              title={showPassword ? "Hide password" : "Show password"}
              onClick={() => setShowPassword((v) => !v)}
              className="absolute right-2 top-1/2 grid size-7 -translate-y-1/2 place-items-center rounded-xl text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
            </button>
          </span>
        </label>
        {error ? (
          <div
            role="alert"
            aria-live="polite"
            className="rounded-xl border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive"
          >
            {error}
          </div>
        ) : null}
        <button
          type="submit"
          disabled={pending}
          className="cocola-login-signin h-10 rounded-xl px-4 text-sm font-medium text-primary-foreground transition-opacity disabled:opacity-60"
        >
          {pending ? "Signing in..." : "Sign in"}
        </button>
      </form>
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

function initialLoginError(error: string | null, reason: string | null) {
  if (reason === "account_disabled") {
    return "This account has been disabled. Contact an administrator.";
  }
  if (error) return "Sign in failed. Check your username/email and password.";
  return "";
}
