"use client";

import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import {
  ChevronRight as CaretRight,
  Check,
  MoreHorizontal as DotsThree,
  Folder,
  FolderOpen,
  Pencil as PencilSimple,
  Trash2 as Trash,
} from "lucide-react";
import type { ConversationFolder, ConversationSummary } from "@/app/runtime-provider";
import { cn } from "@/lib/utils";

export function ConversationActionsMenu({
  conversation,
  folders,
  onRename,
  onDelete,
  onMove,
  triggerClassName,
}: {
  conversation: ConversationSummary;
  folders: ConversationFolder[];
  onRename: () => void;
  onDelete: () => void;
  onMove: (folderId: string | null) => void;
  triggerClassName?: string;
}) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          aria-label={`Actions for ${conversation.title || "Untitled"}`}
          className={cn(
            "grid size-7 shrink-0 place-items-center rounded-lg text-muted-foreground opacity-0 transition hover:bg-black/5 hover:text-foreground focus:opacity-100 focus:outline-none group-hover:opacity-100 data-[state=open]:opacity-100",
            triggerClassName,
          )}
        >
          <DotsThree className="size-4" />
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="end"
          sideOffset={5}
          className="cocola-user-ui z-50 min-w-44 rounded-xl border border-border bg-popover p-1 text-foreground shadow-xl outline-none"
        >
          <MenuItem onSelect={onRename}>
            <PencilSimple className="size-4" />
            Rename
          </MenuItem>
          {conversation.chat_type !== "scheduled_task" ? (
            <DropdownMenu.Sub>
              <DropdownMenu.SubTrigger className="flex cursor-default select-none items-center gap-2 rounded-lg px-2 py-1.5 text-sm outline-none focus:bg-accent data-[state=open]:bg-accent">
                <FolderOpen className="size-4" />
                <span className="flex-1">Move to folder</span>
                <CaretRight className="size-3.5" />
              </DropdownMenu.SubTrigger>
              <DropdownMenu.Portal>
                <DropdownMenu.SubContent
                  sideOffset={6}
                  alignOffset={-5}
                  className="cocola-user-ui z-[51] min-w-48 rounded-xl border border-border bg-popover p-1 text-popover-foreground shadow-xl outline-none"
                >
                  <MenuItem onSelect={() => onMove(null)}>
                    <Folder className="size-4" />
                    <span className="flex-1">No folder</span>
                    {!conversation.folder_id ? <Check className="size-4" /> : null}
                  </MenuItem>
                  {folders.length > 0 ? (
                    <DropdownMenu.Separator className="my-1 h-px bg-border" />
                  ) : null}
                  {folders.map((folder) => (
                    <MenuItem key={folder.id} onSelect={() => onMove(folder.id)}>
                      <Folder className="size-4" />
                      <span className="max-w-40 flex-1 truncate">{folder.name}</span>
                      {conversation.folder_id === folder.id ? (
                        <Check className="size-4" />
                      ) : null}
                    </MenuItem>
                  ))}
                </DropdownMenu.SubContent>
              </DropdownMenu.Portal>
            </DropdownMenu.Sub>
          ) : null}
          <DropdownMenu.Separator className="my-1 h-px bg-border" />
          <MenuItem destructive onSelect={onDelete}>
            <Trash className="size-4" />
            Delete
          </MenuItem>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

function MenuItem({
  children,
  destructive = false,
  onSelect,
}: {
  children: React.ReactNode;
  destructive?: boolean;
  onSelect: () => void;
}) {
  return (
    <DropdownMenu.Item
      onSelect={onSelect}
      className={cn(
        "flex cursor-default select-none items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-foreground outline-none focus:bg-accent focus:text-foreground data-[highlighted]:bg-accent data-[highlighted]:text-foreground",
        destructive &&
          "text-red-500 focus:bg-red-500/10 focus:text-red-600 data-[highlighted]:bg-red-500/10 data-[highlighted]:text-red-600",
      )}
    >
      {children}
    </DropdownMenu.Item>
  );
}
