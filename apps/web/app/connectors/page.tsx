"use client";

import { ShieldCheck } from "lucide-react";
import { useTranslations } from "next-intl";
import { GitHubConnectorCard } from "@/components/connectors/github-connector-card";
import { WorkspaceFeishuConnectorCard } from "@/components/connectors/workspace-feishu-connector-card";
import {
  WorkspacePageFrame,
  WorkspacePageHeader,
} from "@/components/heroui-workspace/workspace-ui";

export default function ConnectorsPage() {
  const t = useTranslations("connectors");

  return (
    <WorkspacePageFrame>
      <WorkspacePageHeader
        description={t("description")}
        icon={<ShieldCheck className="size-5" />}
        title={t("title")}
      />

      <section className="cocola-web-connector-grid grid grid-cols-1 items-stretch justify-start gap-4 sm:grid-cols-[repeat(2,minmax(0,300px))]">
        <GitHubConnectorCard />
        <WorkspaceFeishuConnectorCard />
      </section>
    </WorkspacePageFrame>
  );
}
