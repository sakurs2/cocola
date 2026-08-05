"use client";

import { Avatar, Card, Chip } from "@heroui/react";
import { Mail, ShieldCheck, UserRound } from "lucide-react";
import {
  WorkspacePageFrame,
  WorkspacePageHeader,
} from "@/components/heroui-workspace/workspace-ui";
import { AccountSettingsPanel } from "@/components/profile/account-settings-panel";
import { AgentInstructionsPanel } from "@/components/profile/agent-instructions-panel";
import { MemoryPanel } from "@/components/profile/memory-panel";
import { SignOutButton } from "@/components/profile/sign-out-button";
import { UsagePanel } from "@/components/profile/usage-panel";
import type { SessionUser } from "@/lib/server-auth";

export function ProfilePageContent({ user }: { user: SessionUser }) {
  const displayName = user.name || user.email || "User";
  const initial = displayName.trim().slice(0, 1).toUpperCase() || "U";
  const isAdmin = user.role === "admin";

  return (
    <WorkspacePageFrame>
      <WorkspacePageHeader
        description="Personal settings, agent instructions, and usage."
        icon={<UserRound className="size-5" />}
        title="Profile"
      />

      <Card className="p-5">
        <Card.Content className="!flex-row items-center gap-4 p-0">
          <Avatar className="size-14">
            <Avatar.Fallback className="text-lg font-semibold">{initial}</Avatar.Fallback>
          </Avatar>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="truncate text-lg font-semibold">{displayName}</h2>
              <Chip color={isAdmin ? "accent" : "default"} size="sm" variant="soft">
                <ShieldCheck className="size-3" />
                {user.role}
              </Chip>
            </div>
            <p className="text-muted mt-1 flex items-center gap-2 truncate text-sm">
              <Mail className="size-4 shrink-0" />
              {user.email || "-"}
            </p>
          </div>
          <Chip color="success" size="sm" variant="soft">
            Active
          </Chip>
        </Card.Content>
      </Card>

      <AccountSettingsPanel initialAccount={user} />
      <UsagePanel />
      <MemoryPanel />
      <AgentInstructionsPanel />

      <div className="flex justify-start">
        <SignOutButton />
      </div>
    </WorkspacePageFrame>
  );
}
