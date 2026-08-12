import { Trash2 } from "lucide-react";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { useTranslations } from "next-intl";

export function DeleteConfirmDialog({
  open,
  title,
  description,
  busy,
  error,
  confirmLabel,
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
  const t = useTranslations("common.actions");
  return (
    <ActionConfirmDialog
      busy={busy}
      confirmLabel={confirmLabel ?? t("deleteDefault")}
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
