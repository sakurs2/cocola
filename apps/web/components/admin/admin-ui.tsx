"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { motion } from "framer-motion";
import { X } from "lucide-react";
import { type ComponentPropsWithoutRef, type ReactNode } from "react";
import { cn } from "@/lib/utils";

export function AdminPage({
  children,
  className,
  width = "wide",
}: {
  children: ReactNode;
  className?: string;
  width?: "standard" | "wide";
}) {
  return (
    <motion.main
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.18, ease: "easeOut" }}
      className={cn("min-h-full bg-transparent text-foreground", className)}
    >
      <div
        className={cn(
          "mx-auto w-full space-y-5 px-4 py-5 sm:px-6 sm:py-6",
          width === "standard" ? "max-w-6xl" : "max-w-[100rem]",
        )}
      >
        {children}
      </div>
    </motion.main>
  );
}

export function AdminPageHeader({
  icon,
  eyebrow,
  title,
  description,
  actions,
  className,
}: {
  icon?: ReactNode;
  eyebrow?: string;
  title: string;
  description?: string;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <header
      className={cn(
        "admin-page-header flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between",
        className,
      )}
    >
      <div className="flex min-w-0 items-start gap-3">
        {icon ? <div className="admin-page-icon">{icon}</div> : null}
        <div className="min-w-0">
          {eyebrow ? (
            <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-primary/70">
              {eyebrow}
            </div>
          ) : null}
          <h1 className="text-xl font-semibold tracking-[-0.02em] text-foreground sm:text-2xl">
            {title}
          </h1>
          {description ? (
            <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">{description}</p>
          ) : null}
        </div>
      </div>
      {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
    </header>
  );
}

export function AdminPanel({
  title,
  description,
  actions,
  children,
  className,
  contentClassName,
}: {
  title?: string;
  description?: string;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  contentClassName?: string;
}) {
  return (
    <section className={cn("admin-glass-panel overflow-hidden rounded-2xl border", className)}>
      {title || description || actions ? (
        <div className="flex min-h-14 items-center justify-between gap-4 border-b border-border/70 px-4 py-3 sm:px-5">
          <div className="min-w-0">
            {title ? <h2 className="text-sm font-semibold text-foreground">{title}</h2> : null}
            {description ? (
              <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
            ) : null}
          </div>
          {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
        </div>
      ) : null}
      <div className={cn("p-4 sm:p-5", contentClassName)}>{children}</div>
    </section>
  );
}

export function AdminMetric({
  label,
  value,
  icon,
  detail,
  tone = "default",
  className,
}: {
  label: string;
  value: ReactNode;
  icon?: ReactNode;
  detail?: ReactNode;
  tone?: "default" | "sky" | "violet" | "green" | "amber" | "red";
  className?: string;
}) {
  return (
    <div className={cn("admin-metric rounded-2xl border px-4 py-3.5", className)} data-tone={tone}>
      <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
        <span>{label}</span>
        {icon ? <span className="admin-metric-icon">{icon}</span> : null}
      </div>
      <div className="mt-2 truncate text-2xl font-semibold tabular-nums tracking-[-0.03em] text-foreground">
        {value}
      </div>
      {detail ? <div className="mt-1 text-xs text-muted-foreground">{detail}</div> : null}
    </div>
  );
}

export function AdminToolbar({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        "admin-toolbar flex flex-col gap-3 rounded-2xl border p-3 sm:flex-row sm:flex-wrap sm:items-end",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function AdminTable({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("admin-table-surface overflow-x-auto rounded-2xl border", className)}>
      {children}
    </div>
  );
}

const statusTone = {
  neutral: "border-border bg-muted/60 text-muted-foreground",
  sky: "border-blue-500/25 bg-blue-500/10 text-blue-700",
  green: "border-emerald-500/25 bg-emerald-500/10 text-emerald-700",
  amber: "border-amber-500/25 bg-amber-500/10 text-amber-700",
  red: "border-destructive/25 bg-destructive/10 text-destructive",
} as const;

export function AdminStatusBadge({
  children,
  tone = "neutral",
  dot = false,
  className,
}: {
  children: ReactNode;
  tone?: keyof typeof statusTone;
  dot?: boolean;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex min-h-6 items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium",
        statusTone[tone],
        className,
      )}
    >
      {dot ? <span className="size-1.5 rounded-full bg-current" /> : null}
      {children}
    </span>
  );
}

export function AdminAlert({
  children,
  tone = "info",
  icon,
  className,
}: {
  children: ReactNode;
  tone?: "info" | "success" | "warning" | "error";
  icon?: ReactNode;
  className?: string;
}) {
  const tones = {
    info: "border-blue-500/25 bg-blue-500/10 text-blue-800",
    success: "border-emerald-500/25 bg-emerald-500/10 text-emerald-800",
    warning: "border-amber-500/25 bg-amber-500/10 text-amber-800",
    error: "border-destructive/25 bg-destructive/10 text-destructive",
  };
  return (
    <div
      className={cn(
        "flex items-start gap-2 rounded-xl border px-3 py-2.5 text-sm",
        tones[tone],
        className,
      )}
    >
      {icon ? <span className="mt-0.5 shrink-0">{icon}</span> : null}
      <div className="min-w-0">{children}</div>
    </div>
  );
}

export function AdminEmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex min-h-44 flex-col items-center justify-center px-6 py-10 text-center">
      {icon ? <div className="admin-empty-icon">{icon}</div> : null}
      <div className="mt-3 text-sm font-semibold text-foreground">{title}</div>
      {description ? (
        <p className="mt-1 max-w-md text-sm text-muted-foreground">{description}</p>
      ) : null}
      {action ? <div className="mt-4">{action}</div> : null}
    </div>
  );
}

export function AdminDrawer({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  size = "md",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  size?: "md" | "lg";
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/20 backdrop-blur-sm data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out data-[state=open]:fade-in" />
        <Dialog.Content
          className={cn(
            "admin-drawer fixed inset-y-2 right-2 z-50 flex flex-col overflow-hidden rounded-3xl border outline-none data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right",
            size === "lg" ? "w-[min(42rem,calc(100vw-1rem))]" : "w-[min(30rem,calc(100vw-1rem))]",
          )}
        >
          <div className="flex min-h-16 items-center gap-3 border-b border-border/70 px-5">
            <div className="min-w-0 flex-1">
              <Dialog.Title className="truncate text-base font-semibold">{title}</Dialog.Title>
              <Dialog.Description
                className={cn("text-xs text-muted-foreground", !description && "sr-only")}
              >
                {description || `${title} settings`}
              </Dialog.Description>
            </div>
            <Dialog.Close
              aria-label="Close"
              className="inline-flex size-9 items-center justify-center rounded-xl text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
            >
              <X className="size-4" />
            </Dialog.Close>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-5">{children}</div>
          {footer ? <div className="border-t border-border/70 p-4">{footer}</div> : null}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function AdminIconButton({ className, ...props }: ComponentPropsWithoutRef<"button">) {
  return (
    <button
      className={cn(
        "inline-flex size-9 items-center justify-center rounded-xl text-muted-foreground transition-colors hover:bg-white/55 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:pointer-events-none disabled:opacity-45",
        className,
      )}
      {...props}
    />
  );
}
