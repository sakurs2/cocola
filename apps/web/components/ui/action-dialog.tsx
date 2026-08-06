"use client";

import { AlertDialog, Button, Input, Label, TextField } from "@heroui/react";
import { Sheet } from "@cocola/ui-compat/sheet";
import { AlertTriangle, type LucideIcon } from "lucide-react";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";

type DialogTone = "primary" | "warning" | "danger";
const toneClass: Record<DialogTone, string> = {
  primary: "bg-accent-soft text-accent",
  warning: "bg-warning/10 text-warning",
  danger: "bg-danger/10 text-danger",
};

const toneStatus: Record<DialogTone, "accent" | "warning" | "danger"> = {
  primary: "accent",
  warning: "warning",
  danger: "danger",
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
  className,
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
  className?: string;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  return (
    <AlertDialog
      isOpen={open}
      onOpenChange={(next) => {
        if (!busy) onOpenChange(next);
      }}
    >
      <AlertDialog.Backdrop isDismissable={!busy} isKeyboardDismissDisabled={busy}>
        <AlertDialog.Container className={className} placement="center" size="sm">
          <AlertDialog.Dialog>
            <AlertDialog.Header className="items-start">
              <AlertDialog.Icon status={toneStatus[tone]}>
                <Icon className="size-5" />
              </AlertDialog.Icon>
              <div className="min-w-0">
                <AlertDialog.Heading>{title}</AlertDialog.Heading>
                <p className="mt-1 text-sm leading-6 text-muted">{description}</p>
              </div>
            </AlertDialog.Header>
            <AlertDialog.Body>
              {error ? (
                <div className="rounded-xl bg-danger/10 px-3 py-2.5 text-sm text-danger">
                  {error}
                </div>
              ) : (
                <div className={`${toneClass[tone]} rounded-xl px-3 py-2.5 text-sm`}>
                  {tone === "danger"
                    ? "This action cannot be undone."
                    : "Confirm this operation to continue."}
                </div>
              )}
            </AlertDialog.Body>
            <AlertDialog.Footer>
              <Button isDisabled={busy} variant="outline" onPress={() => onOpenChange(false)}>
                {cancelLabel}
              </Button>
              <Button
                isPending={busy}
                variant={tone === "danger" ? "danger" : "primary"}
                onPress={onConfirm}
              >
                {confirmLabel}
              </Button>
            </AlertDialog.Footer>
          </AlertDialog.Dialog>
        </AlertDialog.Container>
      </AlertDialog.Backdrop>
    </AlertDialog>
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
  useEffect(() => {
    if (open) setValue(initialValue);
  }, [initialValue, open]);
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const next = value.trim();
    if (next) onSubmit(next);
  };
  return (
    <Sheet isOpen={open} placement="right" onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Sheet.Backdrop>
        <Sheet.Content className="w-full md:w-[440px]">
          <Sheet.Dialog>
            <Sheet.CloseTrigger aria-label="Close editor" />
            <Sheet.Header>
              <span className="flex items-center gap-3">
                <span className="bg-accent-soft text-accent flex size-10 shrink-0 items-center justify-center rounded-2xl">
                  <Icon className="size-5" />
                </span>
                <span>
                  <Sheet.Heading>{title}</Sheet.Heading>
                  <span className="text-muted mt-1 block text-sm leading-6">{description}</span>
                </span>
              </span>
            </Sheet.Header>
            <Sheet.Body>
              <form className="grid gap-4" id="text-muted-sheet-form" onSubmit={submit}>
                <TextField isDisabled={busy} value={value} variant="secondary" onChange={setValue}>
                  <Label>{label}</Label>
                  <Input autoFocus inputMode={inputMode} placeholder={placeholder} />
                </TextField>
                {error ? (
                  <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">
                    {error}
                  </div>
                ) : null}
              </form>
            </Sheet.Body>
            <Sheet.Footer className="gap-2">
              {secondaryLabel && onSecondary ? (
                <Button
                  className="mr-auto"
                  isDisabled={busy}
                  variant="danger-soft"
                  onPress={onSecondary}
                >
                  {secondaryLabel}
                </Button>
              ) : null}
              <Button isDisabled={busy} variant="outline" onPress={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button
                form="text-muted-sheet-form"
                isDisabled={busy || !value.trim()}
                isPending={busy}
                type="submit"
              >
                {submitLabel}
              </Button>
            </Sheet.Footer>
          </Sheet.Dialog>
        </Sheet.Content>
      </Sheet.Backdrop>
    </Sheet>
  );
}
