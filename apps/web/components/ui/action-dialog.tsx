"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { AlertTriangle, X, type LucideIcon } from "lucide-react";
import { useEffect, useId, useState, type FormEvent, type ReactNode } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

type DialogTone = "primary" | "warning" | "danger";

const toneStyles: Record<DialogTone, { icon: string; action: string; error: string }> = {
  primary: {
    icon: "bg-blue-500/10 text-blue-600",
    action: "bg-blue-600 text-white hover:bg-blue-700",
    error: "border-red-500/20 bg-red-500/10 text-red-600",
  },
  warning: {
    icon: "bg-amber-500/10 text-amber-600",
    action: "bg-amber-500 text-white hover:bg-amber-600",
    error: "border-red-500/20 bg-red-500/10 text-red-600",
  },
  danger: {
    icon: "bg-red-500/10 text-red-500",
    action: "bg-red-500 text-white hover:bg-red-600",
    error: "border-red-500/20 bg-red-500/10 text-red-600",
  },
};

export function ActionConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel = "Cancel",
  busy = false,
  error = null,
  tone = "warning",
  icon: Icon = AlertTriangle,
  onOpenChange,
  onConfirm,
}: {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  busy?: boolean;
  error?: string | null;
  tone?: DialogTone;
  icon?: LucideIcon;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  const styles = toneStyles[tone];
  return (
    <Dialog.Root open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[70] bg-slate-950/30 backdrop-blur-[2px] data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
        <Dialog.Content className="cocola-user-ui fixed left-1/2 top-1/2 z-[71] w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-border bg-background p-5 text-foreground shadow-2xl outline-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95">
          <div className="flex items-start gap-3">
            <div className={cn("grid size-10 shrink-0 place-items-center rounded-xl", styles.icon)}>
              <Icon className="size-5" />
            </div>
            <div className="min-w-0 flex-1">
              <Dialog.Title className="text-base font-semibold">{title}</Dialog.Title>
              <Dialog.Description asChild>
                <div className="mt-1.5 text-sm leading-6 text-muted-foreground">{description}</div>
              </Dialog.Description>
            </div>
            <Dialog.Close asChild>
              <button
                type="button"
                disabled={busy}
                aria-label="Close"
                className="grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground transition hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:opacity-50"
              >
                <X className="size-4" />
              </button>
            </Dialog.Close>
          </div>
          {error ? (
            <div className={cn("mt-4 rounded-xl border px-3 py-2 text-sm", styles.error)}>
              {error}
            </div>
          ) : null}
          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close asChild>
              <button
                type="button"
                disabled={busy}
                className="h-9 rounded-xl px-3 text-sm text-muted-foreground transition hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:opacity-50"
              >
                {cancelLabel}
              </button>
            </Dialog.Close>
            <button
              type="button"
              disabled={busy}
              onClick={onConfirm}
              className={cn(
                "h-9 rounded-xl px-4 text-sm font-medium transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-65",
                styles.action,
              )}
            >
              {busy ? "Working…" : confirmLabel}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function TextInputDialog({
  open,
  title,
  description,
  label,
  initialValue = "",
  placeholder,
  submitLabel,
  secondaryLabel,
  busy = false,
  error = null,
  icon: Icon,
  inputMode = "text",
  onOpenChange,
  onSubmit,
  onSecondary,
}: {
  open: boolean;
  title: string;
  description: ReactNode;
  label: string;
  initialValue?: string;
  placeholder?: string;
  submitLabel: string;
  secondaryLabel?: string;
  busy?: boolean;
  error?: string | null;
  icon: LucideIcon;
  inputMode?: "text" | "url";
  onOpenChange: (open: boolean) => void;
  onSubmit: (value: string) => void;
  onSecondary?: () => void;
}) {
  const [value, setValue] = useState(initialValue);
  const inputID = useId();

  useEffect(() => {
    if (open) setValue(initialValue);
  }, [initialValue, open]);

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const next = value.trim();
    if (next) onSubmit(next);
  };

  return (
    <Dialog.Root open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[70] bg-slate-950/30 backdrop-blur-[2px] data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
        <Dialog.Content className="cocola-user-ui fixed left-1/2 top-1/2 z-[71] w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-border bg-background p-5 text-foreground shadow-2xl outline-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95">
          <form onSubmit={submit}>
            <div className="flex items-start gap-3">
              <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-blue-500/10 text-blue-600">
                <Icon className="size-5" />
              </div>
              <div className="min-w-0 flex-1">
                <Dialog.Title className="text-base font-semibold">{title}</Dialog.Title>
                <Dialog.Description asChild>
                  <div className="mt-1.5 text-sm leading-6 text-muted-foreground">
                    {description}
                  </div>
                </Dialog.Description>
              </div>
              <Dialog.Close asChild>
                <button
                  type="button"
                  disabled={busy}
                  aria-label="Close"
                  className="grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground transition hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:opacity-50"
                >
                  <X className="size-4" />
                </button>
              </Dialog.Close>
            </div>

            <div className="mt-5 space-y-2">
              <Label htmlFor={inputID}>{label}</Label>
              <Input
                id={inputID}
                autoFocus
                inputMode={inputMode}
                value={value}
                placeholder={placeholder}
                disabled={busy}
                onChange={(event) => setValue(event.target.value)}
              />
            </div>

            {error ? (
              <div className="mt-3 rounded-xl border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm text-red-600">
                {error}
              </div>
            ) : null}

            <div className="mt-5 flex items-center justify-end gap-2">
              {secondaryLabel && onSecondary ? (
                <button
                  type="button"
                  disabled={busy}
                  onClick={onSecondary}
                  className="mr-auto h-9 rounded-xl px-3 text-sm font-medium text-red-600 transition hover:bg-red-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-500/25 disabled:opacity-50"
                >
                  {secondaryLabel}
                </button>
              ) : null}
              <Dialog.Close asChild>
                <button
                  type="button"
                  disabled={busy}
                  className="h-9 rounded-xl px-3 text-sm text-muted-foreground transition hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:opacity-50"
                >
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="submit"
                disabled={busy || !value.trim()}
                className="h-9 rounded-xl bg-blue-600 px-4 text-sm font-medium text-white transition hover:bg-blue-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/30 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {busy ? "Working…" : submitLabel}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
