"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { Command } from "cmdk";
import {
  Bot,
  CalendarClock,
  History,
  Search,
  Sparkles,
  PlugZap,
  UserRound,
  ShieldCheck,
  Plus,
} from "lucide-react";
import { useSession } from "next-auth/react";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { useCocola } from "@/app/runtime-provider";
import { cn } from "@/lib/utils";

export function CommandPalette() {
  const router = useRouter();
  const pathname = usePathname();
  const { data: session } = useSession();
  const { conversations, loadConversation, newConversation } = useCocola();
  const [open, setOpen] = useState(false);
  const isAdmin = session?.user?.role === "admin";

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setOpen((value) => !value);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const actions = useMemo(
    () => [
      {
        id: "new-chat",
        label: "New chat",
        hint: "Start a fresh workspace session",
        icon: Plus,
        run: () => {
          if (pathname !== "/") router.push("/");
          newConversation();
        },
      },
      {
        id: "tasks",
        label: "Tasks",
        hint: "Manage scheduled work",
        icon: CalendarClock,
        run: () => router.push("/tasks"),
      },
      {
        id: "skills",
        label: "Skills",
        hint: "Browse installed agent skills",
        icon: Sparkles,
        run: () => router.push("/skills"),
      },
      {
        id: "mcp",
        label: "MCP",
        hint: "Inspect configured tool servers",
        icon: PlugZap,
        run: () => router.push("/mcps"),
      },
      {
        id: "profile",
        label: "Profile",
        hint: "View account details",
        icon: UserRound,
        run: () => router.push("/profile"),
      },
      ...(isAdmin
        ? [
            {
              id: "admin",
              label: "Admin",
              hint: "Open admin monitoring",
              icon: ShieldCheck,
              run: () => router.push("/admin"),
            },
          ]
        : []),
    ],
    [isAdmin, newConversation, pathname, router],
  );

  const runAndClose = (run: () => void | Promise<void>) => {
    setOpen(false);
    void run();
  };

  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-foreground/12 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
        <Dialog.Content className="fixed left-1/2 top-[14vh] z-50 w-[min(92vw,640px)] -translate-x-1/2 overflow-hidden rounded-2xl border border-border bg-popover text-popover-foreground shadow-2xl outline-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95">
          <Dialog.Title className="sr-only">Command menu</Dialog.Title>
          <Command shouldFilter className="bg-transparent">
            <div className="flex items-center gap-3 border-b border-border px-4">
              <Search className="size-4 text-muted-foreground" />
              <Command.Input
                autoFocus
                placeholder="Search conversations or jump somewhere..."
                className="h-12 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              />
              <kbd className="rounded-md border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                esc
              </kbd>
            </div>
            <Command.List className="max-h-[420px] overflow-y-auto p-2">
              <Command.Empty className="px-3 py-10 text-center text-sm text-muted-foreground">
                No matching command or conversation.
              </Command.Empty>
              <Command.Group heading="Actions" className={groupClassName}>
                {actions.map((action) => (
                  <PaletteItem
                    key={action.id}
                    icon={action.icon}
                    label={action.label}
                    hint={action.hint}
                    onSelect={() => runAndClose(action.run)}
                  />
                ))}
              </Command.Group>
              <Command.Group heading="Conversations" className={cn(groupClassName, "mt-2")}>
                {conversations.map((conversation) => (
                  <PaletteItem
                    key={conversation.id}
                    icon={conversation.chat_type === "scheduled_task" ? Bot : History}
                    label={conversation.title || "Untitled"}
                    hint={new Date(conversation.updated_at).toLocaleString()}
                    onSelect={() =>
                      runAndClose(async () => {
                        if (pathname !== "/") {
                          router.push(`/?conversation=${encodeURIComponent(conversation.id)}`);
                          return;
                        }
                        await loadConversation(conversation.id);
                      })
                    }
                  />
                ))}
              </Command.Group>
            </Command.List>
          </Command>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

const groupClassName =
  "[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:text-muted-foreground";

function PaletteItem({
  icon: Icon,
  label,
  hint,
  onSelect,
}: {
  icon: typeof Search;
  label: string;
  hint: string;
  onSelect: () => void;
}) {
  return (
    <Command.Item
      value={`${label} ${hint}`}
      onSelect={onSelect}
      className="flex cursor-pointer items-center gap-3 rounded-xl px-3 py-2.5 text-sm outline-none data-[selected=true]:bg-accent data-[selected=true]:text-accent-foreground"
    >
      <span className="grid size-8 shrink-0 place-items-center rounded-lg border border-border bg-card text-muted-foreground">
        <Icon className="size-4" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate font-medium">{label}</span>
        <span className="block truncate text-xs text-muted-foreground">{hint}</span>
      </span>
    </Command.Item>
  );
}
