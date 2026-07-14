"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { Trash, X } from "@phosphor-icons/react";

export function DeleteConfirmDialog({
  open,
  title,
  description,
  busy,
  error,
  confirmLabel = "Delete",
  onOpenChange,
  onConfirm,
}: {
  open: boolean;
  title: string;
  description: React.ReactNode;
  busy: boolean;
  error: string | null;
  confirmLabel?: string;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/30 backdrop-blur-[2px]" />
        <Dialog.Content className="cocola-user-ui fixed left-1/2 top-1/2 z-[51] w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-border bg-background p-5 text-foreground shadow-2xl outline-none">
          <div className="flex items-start gap-3">
            <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-red-500/10 text-red-500">
              <Trash className="size-5" weight="duotone" />
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
                className="grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground transition hover:bg-muted hover:text-foreground focus:outline-none disabled:opacity-50"
              >
                <X className="size-4" />
              </button>
            </Dialog.Close>
          </div>
          {error ? (
            <div className="mt-4 rounded-xl border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm text-red-600">
              {error}
            </div>
          ) : null}
          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close asChild>
              <button
                type="button"
                disabled={busy}
                className="h-9 rounded-xl px-3 text-sm text-muted-foreground transition hover:bg-muted hover:text-foreground focus:outline-none disabled:opacity-50"
              >
                Cancel
              </button>
            </Dialog.Close>
            <button
              type="button"
              disabled={busy}
              onClick={onConfirm}
              className="h-9 rounded-xl bg-red-500 px-4 text-sm font-medium text-white transition hover:bg-red-600 focus:outline-none disabled:cursor-not-allowed disabled:opacity-65"
            >
              {busy ? "Deleting..." : confirmLabel}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
