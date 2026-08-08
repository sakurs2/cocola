"use client";

import { motion } from "framer-motion";
import {
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  Copy,
  LoaderCircle,
  MoreHorizontal,
  RefreshCw,
} from "lucide-react";
import { type ReactNode, useLayoutEffect, useRef, useState } from "react";
import {
  AlertDialog,
  Button,
  Card,
  Chip,
  Dropdown,
  Tooltip,
  type ButtonProps,
} from "@heroui/react";
import { DataGrid, type DataGridProps } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Sheet } from "@cocola/ui-compat/sheet";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
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
      <div className="flex min-w-0 items-center gap-3">
        {icon ? <div className="admin-page-icon">{icon}</div> : null}
        <div className="min-w-0">
          {eyebrow ? (
            <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-accent/70">
              {eyebrow}
            </div>
          ) : null}
          <h1 className="text-xl font-semibold tracking-[-0.02em] text-foreground sm:text-2xl">
            {title}
          </h1>
          {description ? (
            <p className="mt-1 max-w-3xl text-sm leading-6 text-muted">{description}</p>
          ) : null}
        </div>
      </div>
      {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
    </header>
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
    <Card className={cn("px-4 py-3.5", className)} data-tone={tone}>
      <div className="flex items-center justify-between gap-3 text-xs text-muted">
        <span>{label}</span>
        {icon ? <span className="admin-metric-icon">{icon}</span> : null}
      </div>
      <div className="mt-2 truncate text-2xl font-semibold tabular-nums tracking-[-0.03em] text-foreground">
        {value}
      </div>
      {detail ? <div className="mt-1 text-xs text-muted">{detail}</div> : null}
    </Card>
  );
}

export function AdminPagination({
  page,
  pageSize,
  count,
  total,
  hasNext,
  loading = false,
  label,
  onPageChange,
  variant = "card",
}: {
  page: number;
  pageSize: number;
  count: number;
  total?: number;
  hasNext?: boolean;
  loading?: boolean;
  label: string;
  onPageChange: (page: number) => void;
  variant?: "card" | "embedded";
}) {
  const start = count > 0 ? page * pageSize + 1 : 0;
  const end = count > 0 ? page * pageSize + count : 0;
  const pageCount = total === undefined ? undefined : Math.max(1, Math.ceil(total / pageSize));
  const canGoNext =
    total === undefined ? Boolean(hasNext) : (page + 1) * pageSize < Math.max(total, 0);

  return (
    <div
      className={cn(
        "flex min-h-14 flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between",
        variant === "card"
          ? "rounded-xl border border-border/70 bg-surface/80"
          : "border-t border-border/70 bg-surface-secondary/15",
      )}
    >
      <div className="text-xs tabular-nums text-muted">
        {total === undefined ? `${start}–${end} ${label}` : `${start}–${end} of ${total} ${label}`}
      </div>
      <div className="flex items-center gap-2">
        <span className="min-w-20 text-center text-xs tabular-nums text-muted">
          Page {page + 1}
          {pageCount === undefined ? "" : ` of ${pageCount}`}
        </span>
        <Button
          size="sm"
          variant="outline"
          aria-label={`Previous page of ${label}`}
          isDisabled={page === 0 || loading}
          onPress={() => onPageChange(Math.max(0, page - 1))}
        >
          <ChevronLeft className="size-4" />
          Previous
        </Button>
        <Button
          size="sm"
          variant="outline"
          aria-label={`Next page of ${label}`}
          isDisabled={!canGoNext || loading}
          onPress={() => onPageChange(page + 1)}
        >
          Next
          <ChevronRight className="size-4" />
        </Button>
      </div>
    </div>
  );
}

const statusTone = {
  neutral: "border-border bg-surface-secondary text-muted",
  sky: "border-blue-500/25 bg-blue-500/10 text-blue-700 dark:text-blue-300",
  green: "border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  amber: "border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  red: "border-red-500/25 bg-red-500/10 text-red-700 dark:text-red-300",
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
    <Chip className={cn(statusTone[tone], className)} size="sm" variant="soft">
      {dot ? <span className="size-1.5 rounded-full bg-current" /> : null}
      {children}
    </Chip>
  );
}

export function AdminDataGrid<T extends object>({
  scrollContainerClassName,
  ...props
}: DataGridProps<T>) {
  return (
    <DataGrid
      {...props}
      scrollContainerClassName={cn("admin-data-grid-scroll", scrollContainerClassName)}
    />
  );
}

export type AdminRowAction = {
  id: string;
  label: string;
  icon?: ReactNode;
  disabled?: boolean;
  destructive?: boolean;
};

export function AdminRowActions({
  label,
  actions,
  busy = false,
  onAction,
}: {
  label: string;
  actions: AdminRowAction[];
  busy?: boolean;
  onAction: (id: string) => void;
}) {
  return (
    <Dropdown>
      <Dropdown.Trigger
        aria-label={label}
        className="text-muted hover:bg-surface-secondary mx-auto grid size-9 place-items-center rounded-xl outline-none transition-colors focus-visible:ring-2 focus-visible:ring-focus"
        isDisabled={busy || actions.length === 0}
      >
        {busy ? (
          <LoaderCircle className="size-4 animate-spin" />
        ) : (
          <MoreHorizontal className="size-4" />
        )}
      </Dropdown.Trigger>
      <Dropdown.Popover placement="bottom end">
        <Dropdown.Menu aria-label={label} onAction={(key) => onAction(String(key))}>
          {actions.map((action) => (
            <Dropdown.Item
              key={action.id}
              id={action.id}
              isDisabled={action.disabled}
              textValue={action.label}
              variant={action.destructive ? "danger" : undefined}
            >
              <span
                className={cn(
                  "flex min-w-0 items-center gap-2",
                  action.destructive && "text-danger",
                )}
              >
                {action.icon ? <span className="shrink-0">{action.icon}</span> : null}
                <span>{action.label}</span>
              </span>
            </Dropdown.Item>
          ))}
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
}

export function AdminTruncatedValue({
  value,
  copyLabel = "value",
  className,
}: {
  value: string;
  copyLabel?: string;
  className?: string;
}) {
  const ref = useRef<HTMLSpanElement>(null);
  const [truncated, setTruncated] = useState(false);
  const [copied, setCopied] = useState(false);

  useLayoutEffect(() => {
    const node = ref.current;
    if (!node) return;
    const update = () => setTruncated(node.scrollWidth > node.clientWidth + 1);
    update();
    const observer = new ResizeObserver(update);
    observer.observe(node);
    return () => observer.disconnect();
  }, [value]);

  const copy = async () => {
    await navigator.clipboard.writeText(value);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  };

  const text = (
    <span ref={ref} className={cn("block min-w-0 flex-1 truncate", className)}>
      {value}
    </span>
  );
  return (
    <span className="group flex min-w-0 items-center gap-1">
      {truncated ? (
        <Tooltip delay={0}>
          <span className="min-w-0 flex-1">{text}</span>
          <Tooltip.Content className="max-w-sm break-all">{value}</Tooltip.Content>
        </Tooltip>
      ) : (
        text
      )}
      <Button
        isIconOnly
        aria-label={copied ? `${copyLabel} copied` : `Copy ${copyLabel}`}
        className={cn(
          "size-7 min-w-7 shrink-0 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100",
          !truncated && "invisible",
        )}
        isDisabled={!truncated}
        size="sm"
        variant="ghost"
        onPress={() => void copy()}
      >
        {copied ? (
          <CheckCircle2 className="text-success size-3.5" />
        ) : (
          <Copy className="size-3.5" />
        )}
      </Button>
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
    error: "border-danger/25 bg-danger/10 text-danger",
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

export function AdminErrorDialog({
  error,
  title = "Operation failed",
  retryLabel = "Try again",
  onDismiss,
  onRetry,
}: {
  error: string | null | undefined;
  title?: string;
  retryLabel?: string;
  onDismiss: () => void;
  onRetry?: () => void;
}) {
  return (
    <AlertDialog
      isOpen={Boolean(error)}
      onOpenChange={(open) => {
        if (!open) onDismiss();
      }}
    >
      <AlertDialog.Backdrop isDismissable>
        <AlertDialog.Container placement="center" size="sm">
          <AlertDialog.Dialog>
            <AlertDialog.Header className="items-start">
              <AlertDialog.Icon status="danger">
                <CircleAlert className="size-5" />
              </AlertDialog.Icon>
              <div className="min-w-0">
                <AlertDialog.Heading>{title}</AlertDialog.Heading>
                <p className="mt-1 text-sm leading-6 text-muted">
                  Cocola could not complete the requested admin operation.
                </p>
              </div>
            </AlertDialog.Header>
            <AlertDialog.Body>
              <p className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-danger/10 px-3 py-2.5 text-sm text-danger">
                {error}
              </p>
            </AlertDialog.Body>
            <AlertDialog.Footer>
              <Button variant="outline" onPress={onDismiss}>
                Close
              </Button>
              {onRetry ? (
                <Button
                  variant="danger"
                  onPress={() => {
                    onDismiss();
                    onRetry();
                  }}
                >
                  {retryLabel}
                </Button>
              ) : null}
            </AlertDialog.Footer>
          </AlertDialog.Dialog>
        </AlertDialog.Container>
      </AlertDialog.Backdrop>
    </AlertDialog>
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
    <EmptyState>
      <EmptyState.Header>
        {icon ? <EmptyState.Media variant="icon">{icon}</EmptyState.Media> : null}
        <EmptyState.Title>{title}</EmptyState.Title>
        {description ? <EmptyState.Description>{description}</EmptyState.Description> : null}
      </EmptyState.Header>
      {action ? <EmptyState.Content>{action}</EmptyState.Content> : null}
    </EmptyState>
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
  className,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  size?: "md" | "lg";
  className?: string;
}) {
  return (
    <Sheet isOpen={open} placement="right" onOpenChange={onOpenChange}>
      <Sheet.Backdrop>
        <Sheet.Content
          className={cn(size === "lg" ? "w-full md:w-[672px]" : "w-full md:w-[480px]", className)}
        >
          <Sheet.Dialog>
            <Sheet.CloseTrigger aria-label="Close" />
            <Sheet.Header>
              <Sheet.Heading>{title}</Sheet.Heading>
              {description ? <p className="text-muted text-sm leading-6">{description}</p> : null}
            </Sheet.Header>
            <Sheet.Body>{children}</Sheet.Body>
            {footer ? <Sheet.Footer>{footer}</Sheet.Footer> : null}
          </Sheet.Dialog>
        </Sheet.Content>
      </Sheet.Backdrop>
    </Sheet>
  );
}

export function AdminConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = "Confirm",
  busy = false,
  destructive = false,
  error = null,
  onConfirm,
  className,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel?: string;
  busy?: boolean;
  destructive?: boolean;
  error?: string | null;
  onConfirm: () => void;
  className?: string;
}) {
  return (
    <ActionConfirmDialog
      busy={busy}
      className={className}
      confirmLabel={confirmLabel}
      description={description}
      error={error}
      open={open}
      title={title}
      tone={destructive ? "danger" : "primary"}
      onConfirm={onConfirm}
      onOpenChange={onOpenChange}
    />
  );
}

export function AdminRefreshButton({
  refreshing = false,
  iconClassName,
  className,
  children,
  disabled,
  title,
  onClick,
  ...props
}: Omit<ButtonProps, "children" | "isDisabled" | "onPress"> & {
  children?: ReactNode;
  disabled?: boolean;
  title?: string;
  onClick?: () => void;
  refreshing?: boolean;
  iconClassName?: string;
}) {
  const [refreshCycle, setRefreshCycle] = useState(0);

  return (
    <Button
      aria-label={props["aria-label"] || title}
      className={cn("gap-2", className)}
      isDisabled={disabled}
      onPress={() => {
        setRefreshCycle((cycle) => cycle + 1);
        onClick?.();
      }}
      {...props}
    >
      <RefreshCw
        key={refreshCycle}
        aria-hidden="true"
        className={cn(
          "size-4 shrink-0",
          refreshing ? "animate-spin" : refreshCycle > 0 && "admin-refresh-spin-once",
          iconClassName,
        )}
      />
      {children}
    </Button>
  );
}
