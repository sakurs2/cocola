"use client";

import { useThread, type DataMessagePartProps } from "@assistant-ui/react";
import {
  BadgeCheck,
  Ban,
  Check,
  ChevronDown,
  ChevronUp,
  CirclePause,
  CircleX,
  CopyIcon,
  Ellipsis,
  LoaderCircle,
  Map as PlanModeIcon,
  Play,
  RotateCcw,
  ShieldCheck,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useId, useRef, useState, type FC } from "react";

import { useCocola, type PlanStatus, type UiPlanPart } from "@/app/runtime-provider";
import { MarkdownContent } from "@/components/assistant-ui/markdown-text";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  PLAN_ACTION_LABELS,
  PLAN_GATE_COPY,
  PLAN_STATUS_LABELS,
  normalizePlanStatus,
} from "@/lib/plan-mode.mjs";
import { cn } from "@/lib/utils";

const PLAN_STATUS_VIEW: Record<
  PlanStatus,
  {
    icon: LucideIcon;
    badge: string;
    accent: string;
    frame: string;
    header: string;
    iconFrame: string;
    notice: string;
    spin?: boolean;
  }
> = {
  ready: {
    icon: ShieldCheck,
    badge: "border-indigo-500/25 bg-indigo-500/10 text-indigo-700",
    accent: "bg-indigo-600",
    frame: "border-indigo-500/25 shadow-[0_18px_45px_-34px_rgba(79,70,229,0.75)]",
    header: "bg-indigo-500/[0.045]",
    iconFrame: "bg-indigo-600 text-white",
    notice: PLAN_GATE_COPY.noChanges,
  },
  executing: {
    icon: LoaderCircle,
    badge: "border-indigo-500/25 bg-indigo-500/10 text-indigo-700",
    accent: "bg-indigo-600",
    frame: "border-indigo-500/25 shadow-[0_18px_45px_-34px_rgba(79,70,229,0.75)]",
    header: "bg-indigo-500/[0.045]",
    iconFrame: "bg-indigo-500/10 text-indigo-700",
    notice: PLAN_GATE_COPY.executingNotice,
    spin: true,
  },
  completed: {
    icon: BadgeCheck,
    badge: "border-emerald-500/25 bg-emerald-500/10 text-emerald-700",
    accent: "bg-emerald-500",
    frame: "border-emerald-500/20",
    header: "bg-emerald-500/[0.035]",
    iconFrame: "bg-emerald-500/10 text-emerald-700",
    notice: PLAN_GATE_COPY.completedNotice,
  },
  stopped: {
    icon: CirclePause,
    badge: "border-amber-500/30 bg-amber-500/10 text-amber-700",
    accent: "bg-amber-500",
    frame: "border-amber-500/25",
    header: "bg-amber-500/[0.035]",
    iconFrame: "bg-amber-500/10 text-amber-700",
    notice: PLAN_GATE_COPY.stoppedNotice,
  },
  failed: {
    icon: CircleX,
    badge: "border-red-500/25 bg-red-500/10 text-red-700",
    accent: "bg-red-500",
    frame: "border-red-500/20",
    header: "bg-red-500/[0.035]",
    iconFrame: "bg-red-500/10 text-red-700",
    notice: PLAN_GATE_COPY.failedNotice,
  },
  superseded: {
    icon: RotateCcw,
    badge: "border-border bg-muted text-muted-foreground",
    accent: "bg-muted-foreground/35",
    frame: "border-border",
    header: "bg-muted/25",
    iconFrame: "bg-muted text-muted-foreground",
    notice: PLAN_GATE_COPY.supersededNotice,
  },
  cancelled: {
    icon: Ban,
    badge: "border-border bg-muted text-muted-foreground",
    accent: "bg-muted-foreground/35",
    frame: "border-border",
    header: "bg-muted/25",
    iconFrame: "bg-muted text-muted-foreground",
    notice: PLAN_GATE_COPY.cancelledNotice,
  },
};

export const PlanCardPart: FC<
  DataMessagePartProps<{
    planId: string;
    version: number;
    status: PlanStatus;
    contentMarkdown: string;
  }>
> = ({ data }) => {
  const { executePlan, cancelPlan, revisingPlanId, revisePlan } = useCocola();
  const isRunning = useThread((thread) => thread.isRunning);
  const status = normalizePlanStatus(data.status) as PlanStatus;
  const isInitiallyCollapsed = ["completed", "superseded", "cancelled"].includes(status);
  const [expanded, setExpanded] = useState(!isInitiallyCollapsed);
  const [pendingAction, setPendingAction] = useState<"execute" | "cancel" | null>(null);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  const copiedTimeoutRef = useRef<number | null>(null);
  const planTitleId = useId();
  const plan: UiPlanPart = {
    type: "plan",
    planId: data.planId,
    version: data.version,
    status,
    contentMarkdown: data.contentMarkdown,
  };
  const busy = pendingAction != null;
  const isRevising = revisingPlanId === data.planId;
  const approvalDisabled = busy || isRunning || isRevising;
  const statusView = PLAN_STATUS_VIEW[status];
  const StatusIcon = statusView.icon;
  const canCollapse = ["completed", "superseded", "cancelled"].includes(status);
  const canCancel = ["ready", "stopped"].includes(status);
  const hasDecisionFooter = ["ready", "stopped", "failed"].includes(status);
  const footerNotice =
    status === "ready"
      ? PLAN_GATE_COPY.approveNotice
      : status === "stopped"
        ? PLAN_GATE_COPY.continueNotice
        : PLAN_GATE_COPY.replanNotice;

  useEffect(
    () => () => {
      if (copiedTimeoutRef.current != null) {
        window.clearTimeout(copiedTimeoutRef.current);
      }
    },
    [],
  );

  const runAction = async (actionName: "execute" | "cancel", action: () => Promise<void>) => {
    setPendingAction(actionName);
    setError("");
    try {
      await action();
    } catch (actionError) {
      setError(actionError instanceof Error ? actionError.message : "The plan action failed.");
    } finally {
      setPendingAction(null);
    }
  };

  const copy = async () => {
    setError("");
    try {
      await navigator.clipboard.writeText(data.contentMarkdown);
      setCopied(true);
      if (copiedTimeoutRef.current != null) {
        window.clearTimeout(copiedTimeoutRef.current);
      }
      copiedTimeoutRef.current = window.setTimeout(() => {
        setCopied(false);
        copiedTimeoutRef.current = null;
      }, 1_400);
    } catch {
      setError(PLAN_GATE_COPY.copyFailed);
    }
  };

  const revise = () => revisePlan(plan);

  return (
    <section
      aria-labelledby={planTitleId}
      className={cn("relative my-4 overflow-hidden rounded-2xl border bg-card", statusView.frame)}
    >
      <div className={cn("absolute inset-y-0 left-0 w-1", statusView.accent)} aria-hidden="true" />
      <div
        className={cn(
          "flex flex-col gap-3 border-b border-border/70 py-4 pr-4 pl-5 sm:flex-row sm:items-start sm:justify-between sm:pr-5 sm:pl-6",
          statusView.header,
        )}
      >
        <div className="flex min-w-0 items-start gap-3">
          <span
            className={cn(
              "mt-0.5 grid size-9 shrink-0 place-items-center rounded-xl shadow-sm",
              statusView.iconFrame,
            )}
          >
            <PlanModeIcon className="size-[18px]" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <div className="text-[10px] font-bold tracking-[0.16em] text-indigo-700 uppercase">
              {PLAN_GATE_COPY.eyebrow}
            </div>
            <h3
              id={planTitleId}
              className="mt-0.5 text-lg font-semibold tracking-[-0.01em] text-foreground"
            >
              Plan v{data.version}
            </h3>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">{statusView.notice}</p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1.5 self-start">
          <span
            role="status"
            aria-live="polite"
            className={cn(
              "inline-flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-[11px] font-semibold",
              statusView.badge,
            )}
          >
            <StatusIcon
              className={cn(
                "size-3.5",
                statusView.spin && "animate-spin motion-reduce:animate-none",
              )}
              aria-hidden="true"
            />
            {PLAN_STATUS_LABELS[status]}
          </span>
          {canCollapse ? (
            <TooltipIconButton
              tooltip={expanded ? PLAN_GATE_COPY.hidePlan : PLAN_GATE_COPY.showPlan}
              variant="ghost"
              onClick={() => setExpanded((value) => !value)}
              aria-label={expanded ? PLAN_GATE_COPY.hidePlan : PLAN_GATE_COPY.showPlan}
              aria-expanded={expanded}
              className="size-8 rounded-full p-0 text-muted-foreground hover:bg-background/80 hover:text-foreground"
            >
              {expanded ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
            </TooltipIconButton>
          ) : null}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                aria-label={
                  pendingAction === "cancel"
                    ? PLAN_GATE_COPY.cancelling
                    : copied
                      ? PLAN_GATE_COPY.copied
                      : PLAN_GATE_COPY.moreActions
                }
                aria-busy={pendingAction === "cancel"}
                disabled={pendingAction === "cancel"}
                className={cn(
                  "grid size-8 place-items-center rounded-full text-muted-foreground transition-colors hover:bg-background/80 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  copied && "text-emerald-600",
                )}
              >
                {pendingAction === "cancel" ? (
                  <LoaderCircle
                    className="size-4 animate-spin motion-reduce:animate-none"
                    aria-hidden="true"
                  />
                ) : copied ? (
                  <Check className="size-4" aria-hidden="true" />
                ) : (
                  <Ellipsis className="size-4" aria-hidden="true" />
                )}
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="cocola-user-ui w-48 rounded-xl">
              <DropdownMenuItem disabled={busy} onSelect={() => void copy()}>
                <CopyIcon className="size-4" aria-hidden="true" />
                {PLAN_ACTION_LABELS.copy}
              </DropdownMenuItem>
              {canCancel ? (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    disabled={approvalDisabled}
                    onSelect={() => void runAction("cancel", () => cancelPlan(plan))}
                  >
                    <Ban className="size-4" aria-hidden="true" />
                    {PLAN_ACTION_LABELS.cancel}
                  </DropdownMenuItem>
                </>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
      {expanded ? (
        <div className="px-5 py-5 sm:px-6 sm:py-6">
          <MarkdownContent value={data.contentMarkdown} />
        </div>
      ) : null}
      {isRevising ? (
        <div className="flex items-center gap-2 border-t border-indigo-500/15 bg-indigo-500/[0.055] px-5 py-3 text-xs font-medium text-indigo-700 sm:px-6">
          <RotateCcw className="size-3.5" aria-hidden="true" />
          {PLAN_GATE_COPY.revisionInProgress}
        </div>
      ) : null}
      {error ? (
        <div
          role="alert"
          className="border-t border-destructive/15 bg-destructive/[0.045] px-5 py-3 text-xs text-destructive sm:px-6"
        >
          {error}
        </div>
      ) : null}
      {hasDecisionFooter ? (
        <div className="flex flex-col gap-3 border-t border-border/70 bg-muted/20 px-5 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-6">
          <p className="max-w-md text-xs leading-5 text-muted-foreground">{footerNotice}</p>
          <div className="flex w-full flex-col-reverse gap-2 sm:w-auto sm:flex-row">
            {status !== "failed" ? (
              <button
                type="button"
                disabled={busy || isRunning || isRevising}
                onClick={revise}
                className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl border border-border bg-background px-4 text-sm font-semibold text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 sm:w-auto"
              >
                <RotateCcw className="size-4" aria-hidden="true" />
                {status === "ready" ? PLAN_ACTION_LABELS.revise : PLAN_ACTION_LABELS.replan}
              </button>
            ) : null}
            {status === "failed" ? (
              <button
                type="button"
                disabled={busy || isRunning || isRevising}
                onClick={revise}
                className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl bg-indigo-600 px-4 text-sm font-semibold text-white shadow-sm transition-colors hover:bg-indigo-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 sm:w-auto"
              >
                <RotateCcw className="size-4" aria-hidden="true" />
                {PLAN_ACTION_LABELS.replan}
              </button>
            ) : (
              <button
                type="button"
                disabled={approvalDisabled}
                aria-busy={pendingAction === "execute"}
                onClick={() => void runAction("execute", () => executePlan(plan))}
                className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl bg-indigo-600 px-4 text-sm font-semibold text-white shadow-sm transition-colors hover:bg-indigo-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 sm:w-auto"
              >
                {pendingAction === "execute" ? (
                  <LoaderCircle
                    className="size-4 animate-spin motion-reduce:animate-none"
                    aria-hidden="true"
                  />
                ) : (
                  <Play className="size-4 fill-current" aria-hidden="true" />
                )}
                {pendingAction === "execute"
                  ? PLAN_GATE_COPY.startingExecution
                  : status === "ready"
                    ? PLAN_ACTION_LABELS.approve
                    : PLAN_ACTION_LABELS.continue}
              </button>
            )}
          </div>
        </div>
      ) : null}
    </section>
  );
};
