"use client";

import { Button } from "@heroui/react";
import { Sheet } from "@heroui-pro/react/sheet";
import { Trash2 } from "lucide-react";

export function DeleteConfirmDialog({ open, title, description, busy, error, confirmLabel = "Delete", onOpenChange, onConfirm }: { open: boolean; title: string; description: React.ReactNode; busy: boolean; error: string | null; confirmLabel?: string; onOpenChange: (open: boolean) => void; onConfirm: () => void }) {
  return <Sheet isOpen={open} placement="right" onOpenChange={(next) => !busy && onOpenChange(next)}><Sheet.Backdrop><Sheet.Content className="w-full md:w-[440px]"><Sheet.Dialog><Sheet.CloseTrigger aria-label="Close delete confirmation" /><Sheet.Header><span className="flex items-center gap-3"><span className="bg-danger/10 text-danger flex size-10 shrink-0 items-center justify-center rounded-2xl"><Trash2 className="size-5" /></span><span><Sheet.Heading>{title}</Sheet.Heading><span className="text-muted mt-1 block text-sm leading-6">{description}</span></span></span></Sheet.Header><Sheet.Body>{error ? <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div> : <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">This action cannot be undone.</div>}</Sheet.Body><Sheet.Footer className="gap-2"><Button isDisabled={busy} variant="outline" onPress={() => onOpenChange(false)}>Cancel</Button><Button isPending={busy} variant="danger" onPress={onConfirm}>{confirmLabel}</Button></Sheet.Footer></Sheet.Dialog></Sheet.Content></Sheet.Backdrop></Sheet>;
}
