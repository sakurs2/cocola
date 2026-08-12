"use client";

import { useThread, type DataMessagePartProps } from "@assistant-ui/react";
import {
  Button,
  Card,
  Chip,
  Dropdown,
  Label,
  Separator,
  Spinner,
  TextArea,
  TextField,
  Tooltip,
} from "@heroui/react";
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
  ListChecks,
  RotateCcw,
  ShieldCheck,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useId, useRef, useState, type FC, type Key } from "react";
import { useTranslations } from "next-intl";

import { useCocola, type PlanStatus, type UiPlanPart } from "@/app/runtime-provider";
import { MarkdownContent } from "@/components/assistant-ui/markdown-text";
import { normalizePlanStatus } from "@/lib/plan-mode.mjs";

type PlanStatusColor = "accent" | "danger" | "default" | "success" | "warning";

const PLAN_STATUS_VIEW: Record<PlanStatus, { color: PlanStatusColor; icon: LucideIcon | null }> = {
  ready: { color: "accent", icon: ShieldCheck },
  executing: { color: "accent", icon: null },
  completed: { color: "success", icon: BadgeCheck },
  stopped: { color: "warning", icon: CirclePause },
  failed: { color: "danger", icon: CircleX },
  superseded: { color: "default", icon: RotateCcw },
  cancelled: { color: "default", icon: Ban },
};

export const PlanCardPart: FC<
  DataMessagePartProps<{
    planId: string;
    version: number;
    status: PlanStatus;
    contentMarkdown: string;
  }>
> = ({ data }) => {
  const t = useTranslations("chat.plan");
  const { executePlan, cancelPlan, revisingPlanId, revisePlan, questionInputLocked } = useCocola();
  const isRunning = useThread((thread) => thread.isRunning);
  const status = normalizePlanStatus(data.status) as PlanStatus;
  const isInitiallyCollapsed = ["completed", "superseded", "cancelled"].includes(status);
  const [expanded, setExpanded] = useState(!isInitiallyCollapsed);
  const [pendingAction, setPendingAction] = useState<"execute" | "cancel" | null>(null);
  const [revisionOpen, setRevisionOpen] = useState(false);
  const [revisionText, setRevisionText] = useState("");
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
  const approvalDisabled = busy || isRunning || isRevising || questionInputLocked;
  const statusView = PLAN_STATUS_VIEW[status];
  const StatusIcon = statusView.icon;
  const canCollapse = ["completed", "superseded", "cancelled"].includes(status);
  const canCancel = ["ready", "stopped"].includes(status);
  const hasDecisionFooter = ["ready", "stopped", "failed"].includes(status);
  const footerNotice =
    status === "ready"
      ? t("approveNotice")
      : status === "stopped"
        ? t("continueNotice")
        : t("replanNotice");
  const FooterIcon = status === "failed" ? CircleX : ShieldCheck;

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
      setError(actionError instanceof Error ? actionError.message : t("actionFailed"));
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
      setError(t("copyFailed"));
    }
  };

  const revise = () => {
    setError("");
    setRevisionOpen(true);
  };

  const submitRevision = () => {
    const feedback = revisionText.trim();
    if (!feedback) return;
    setError("");
    if (!revisePlan(plan, feedback)) {
      setError(t("revisionUnavailable"));
      return;
    }
    setRevisionText("");
    setRevisionOpen(false);
  };

  const handleMenuAction = (key: Key) => {
    if (key === "copy") {
      void copy();
      return;
    }
    if (key === "cancel" && canCancel) {
      void runAction("cancel", () => cancelPlan(plan));
    }
  };

  return (
    <Card aria-labelledby={planTitleId} className="my-4 w-full max-w-none overflow-hidden p-0">
      <Card.Header className="flex-col items-stretch gap-2 px-4 py-3 sm:flex-row sm:items-start sm:justify-between sm:gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <span className="bg-accent-soft text-accent grid size-8 shrink-0 place-items-center rounded-xl">
            <ListChecks className="size-4" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <Card.Title id={planTitleId} className="text-base tracking-[-0.02em]">
              {t("title")}
            </Card.Title>
            <Card.Description className="mt-0.5 text-xs tabular-nums">
              {t("version", { version: data.version })}
            </Card.Description>
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-1 self-end sm:self-start">
          <Chip color={statusView.color} size="sm" variant="soft">
            {status === "executing" ? (
              <Spinner color="current" size="sm" />
            ) : StatusIcon ? (
              <StatusIcon className="size-3.5" aria-hidden="true" />
            ) : null}
            {t(`status.${status}`)}
          </Chip>
          {canCollapse ? (
            <Tooltip delay={0}>
              <Button
                isIconOnly
                aria-label={expanded ? t("hide") : t("show")}
                aria-expanded={expanded}
                size="sm"
                variant="ghost"
                onPress={() => setExpanded((value) => !value)}
              >
                {expanded ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
              </Button>
              <Tooltip.Content>{expanded ? t("hide") : t("show")}</Tooltip.Content>
            </Tooltip>
          ) : null}
          <Dropdown>
            <Dropdown.Trigger
              aria-label={
                pendingAction === "cancel"
                  ? t("cancelling")
                  : copied
                    ? t("copied")
                    : t("moreActions")
              }
              className="text-muted hover:bg-default hover:text-foreground inline-flex size-8 items-center justify-center rounded-full disabled:cursor-not-allowed disabled:opacity-50"
              isDisabled={pendingAction === "cancel"}
            >
              {pendingAction === "cancel" ? (
                <Spinner color="current" size="sm" />
              ) : copied ? (
                <Check className="text-success size-4" aria-hidden="true" />
              ) : (
                <Ellipsis className="size-4" aria-hidden="true" />
              )}
            </Dropdown.Trigger>
            <Dropdown.Popover className="min-w-44" placement="bottom end">
              <Dropdown.Menu aria-label={t("moreActions")} onAction={handleMenuAction}>
                <Dropdown.Item id="copy" isDisabled={busy} textValue={t("copy")}>
                  <CopyIcon className="text-muted size-4 shrink-0" aria-hidden="true" />
                  <span data-slot="label">{t("copy")}</span>
                </Dropdown.Item>
                {canCancel ? (
                  <Dropdown.Item
                    id="cancel"
                    isDisabled={approvalDisabled}
                    textValue={t("cancel")}
                    variant="danger"
                  >
                    <Ban className="size-4 shrink-0" aria-hidden="true" />
                    <span data-slot="label">{t("cancel")}</span>
                  </Dropdown.Item>
                ) : null}
              </Dropdown.Menu>
            </Dropdown.Popover>
          </Dropdown>
        </div>
      </Card.Header>

      {expanded ? (
        <>
          <Separator />
          <Card.Content className="px-4 py-3">
            <MarkdownContent
              className="text-sm leading-6 [&_h1]:mt-3 [&_h2]:mt-3 [&_h3]:mt-3 [&_hr]:my-3 [&_li]:my-0.5 [&_ol]:my-2 [&_p]:my-1.5 [&_ul]:my-2"
              value={data.contentMarkdown}
            />
          </Card.Content>
        </>
      ) : null}

      {revisionOpen && !isRevising ? (
        <>
          <Separator />
          <Card.Content className="grid gap-2.5 px-4 py-3">
            <div>
              <p className="text-sm font-medium text-foreground">{t("whatChange")}</p>
              <p className="mt-0.5 text-xs text-muted">{t("feedback")}</p>
            </div>
            <TextField
              className="w-full"
              value={revisionText}
              variant="secondary"
              onChange={setRevisionText}
            >
              <Label className="sr-only">{t("requestedChanges")}</Label>
              <TextArea
                autoFocus
                className="min-h-20 resize-y"
                maxLength={16 * 1024}
                placeholder={t("placeholder")}
                rows={3}
              />
            </TextField>
            <div className="flex justify-end gap-2">
              <Button
                size="sm"
                variant="outline"
                onPress={() => {
                  setRevisionOpen(false);
                  setRevisionText("");
                }}
              >
                {t("cancelRevision")}
              </Button>
              <Button isDisabled={!revisionText.trim()} size="sm" onPress={submitRevision}>
                {t("generateRevision")}
              </Button>
            </div>
          </Card.Content>
        </>
      ) : null}

      {isRevising ? (
        <>
          <Separator />
          <div className="bg-accent-soft text-accent flex items-center gap-2 px-4 py-2 text-xs font-medium">
            <RotateCcw className="size-3.5" aria-hidden="true" />
            {t("revisionProgress")}
          </div>
        </>
      ) : null}

      {error ? (
        <>
          <Separator />
          <div role="alert" className="bg-danger-soft text-danger px-4 py-2 text-xs">
            {error}
          </div>
        </>
      ) : null}

      {hasDecisionFooter && !revisionOpen ? (
        <>
          <Separator />
          <Card.Footer className="flex-col items-stretch gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-muted flex min-w-0 items-center gap-2 text-xs leading-4">
              <FooterIcon
                className={
                  status === "failed"
                    ? "text-danger size-4 shrink-0"
                    : "text-accent size-4 shrink-0"
                }
                aria-hidden="true"
              />
              {footerNotice}
            </p>
            <div className="flex shrink-0 gap-2">
              {status !== "failed" ? (
                <Button
                  className="flex-1 sm:flex-none"
                  isDisabled={busy || isRunning || isRevising || questionInputLocked}
                  size="sm"
                  variant="outline"
                  onPress={revise}
                >
                  {status === "ready" ? t("revise") : t("replan")}
                </Button>
              ) : null}
              {status === "failed" ? (
                <Button
                  className="flex-1 sm:flex-none"
                  isDisabled={busy || isRunning || isRevising || questionInputLocked}
                  size="sm"
                  onPress={revise}
                >
                  {t("replan")}
                </Button>
              ) : (
                <Button
                  className="flex-1 sm:flex-none"
                  isDisabled={approvalDisabled}
                  isPending={pendingAction === "execute"}
                  size="sm"
                  onPress={() => void runAction("execute", () => executePlan(plan))}
                >
                  {pendingAction === "execute"
                    ? t("starting")
                    : status === "ready"
                      ? t("approve")
                      : t("continue")}
                </Button>
              )}
            </div>
          </Card.Footer>
        </>
      ) : null}
    </Card>
  );
};
