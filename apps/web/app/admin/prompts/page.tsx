import { redirect } from "next/navigation";

export default function LegacyAdminPromptPage() {
  redirect("/admin/toolbox?tool=system-prompt");
}
