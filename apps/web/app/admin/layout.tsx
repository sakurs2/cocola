import { AdminHeroUIProvider } from "@/components/admin/admin-heroui-provider";
import { AdminShell } from "@/components/admin/admin-shell";
import type { ReactNode } from "react";

export default function AdminLayout({ children }: { children: ReactNode }) {
  return (
    <AdminHeroUIProvider>
      <AdminShell>{children}</AdminShell>
    </AdminHeroUIProvider>
  );
}
