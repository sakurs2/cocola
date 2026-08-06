import { Trash2 } from "lucide-react";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";

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
    <ActionConfirmDialog
      busy={busy}
      confirmLabel={confirmLabel}
      description={description}
      error={error}
      icon={Trash2}
      open={open}
      title={title}
      tone="danger"
      onConfirm={onConfirm}
      onOpenChange={onOpenChange}
    />
  );
}
