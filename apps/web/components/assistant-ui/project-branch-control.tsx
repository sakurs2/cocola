"use client";

import * as Popover from "@radix-ui/react-popover";
import { Command } from "cmdk";
import { Check, ChevronDown, GitBranch, Loader2, Search } from "lucide-react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

const ProjectComposerBranchContext = createContext<ReactNode>(null);

export function ProjectComposerBranchProvider({
  children,
  control,
}: {
  children: ReactNode;
  control: ReactNode;
}) {
  return (
    <ProjectComposerBranchContext.Provider value={control}>
      {children}
    </ProjectComposerBranchContext.Provider>
  );
}

export function useProjectComposerBranchControl() {
  return useContext(ProjectComposerBranchContext);
}

type BranchOption = {
  name: string;
  sha: string;
  is_default: boolean;
  protected: boolean;
};

type BranchPage = {
  items?: BranchOption[];
  next_cursor?: string;
  error?: { message?: string };
};

export function ProjectBaseBranchPicker({
  projectID,
  value,
  onChange,
  disabled = false,
}: {
  projectID: string;
  value: string;
  onChange: (branch: string) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [branches, setBranches] = useState<BranchOption[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const loadingRef = useRef(false);
  const requestVersionRef = useRef(0);

  useEffect(() => {
    requestVersionRef.current += 1;
    loadingRef.current = false;
    setBranches([]);
    setNextCursor("");
    setLoaded(false);
    setLoading(false);
    setError("");
  }, [projectID]);

  const load = useCallback(
    async (cursor = "") => {
      if (loadingRef.current) return;
      loadingRef.current = true;
      const requestVersion = ++requestVersionRef.current;
      setLoading(true);
      setError("");
      try {
        const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
        const response = await fetch(
          `/api/projects/${encodeURIComponent(projectID)}/branches${query}`,
          { cache: "no-store" },
        );
        const body = (await response.json().catch(() => ({}))) as BranchPage;
        if (!response.ok) {
          throw new Error(body.error?.message || "Could not load branches");
        }
        if (requestVersion !== requestVersionRef.current) return;
        const incoming = Array.isArray(body.items) ? body.items : [];
        setBranches((current) => {
          const merged = new Map(current.map((branch) => [branch.name, branch]));
          for (const branch of incoming) merged.set(branch.name, branch);
          return [...merged.values()];
        });
        setNextCursor(body.next_cursor || "");
        setLoaded(true);
      } catch (loadError) {
        if (requestVersion !== requestVersionRef.current) return;
        setError(loadError instanceof Error ? loadError.message : "Could not load branches");
      } finally {
        if (requestVersion === requestVersionRef.current) {
          loadingRef.current = false;
          setLoading(false);
        }
      }
    },
    [projectID],
  );

  useEffect(() => {
    if (open && !loaded) void load();
  }, [load, loaded, open]);

  const options = useMemo(() => {
    if (!value || branches.some((branch) => branch.name === value)) return branches;
    return [{ name: value, sha: "", is_default: false, protected: false }, ...branches];
  }, [branches, value]);

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button
          type="button"
          disabled={disabled}
          aria-label="Select base branch"
          title="Select the branch this new task starts from"
          className="flex max-w-[12rem] min-w-0 items-center gap-1.5 rounded-full border border-border px-2.5 py-1.5 text-[12.5px] font-medium text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-70"
        >
          <GitBranch className="size-4 shrink-0 text-indigo-600" />
          <span className="truncate">{value || "Select branch"}</span>
          <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          side="top"
          align="start"
          sideOffset={10}
          className="cocola-user-ui z-50 w-72 overflow-hidden rounded-2xl border border-border bg-popover text-popover-foreground shadow-xl"
        >
          <Command>
            <div className="flex items-center gap-2 border-b border-border px-3">
              <Search className="size-4 text-muted-foreground" />
              <Command.Input
                placeholder="Find a branch..."
                className="h-10 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              />
            </div>
            <Command.List className="max-h-64 overflow-auto p-1.5">
              {!loading && !error ? (
                <Command.Empty className="px-3 py-8 text-center text-sm text-muted-foreground">
                  No branch found.
                </Command.Empty>
              ) : null}
              {options.map((branch) => (
                <Command.Item
                  key={branch.name}
                  value={branch.name}
                  className="flex cursor-pointer items-center gap-2 rounded-xl px-2 py-2 text-sm outline-none data-[selected=true]:bg-accent data-[selected=true]:text-accent-foreground"
                  onSelect={() => {
                    onChange(branch.name);
                    setOpen(false);
                  }}
                >
                  <GitBranch className="size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate font-medium">{branch.name}</span>
                  {branch.is_default ? (
                    <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
                      Default
                    </span>
                  ) : null}
                  {branch.name === value ? <Check className="size-4 shrink-0" /> : null}
                </Command.Item>
              ))}
              {loading ? (
                <div className="flex items-center justify-center gap-2 px-3 py-5 text-xs text-muted-foreground">
                  <Loader2 className="size-3.5 animate-spin" />
                  Loading branches
                </div>
              ) : null}
              {error ? (
                <div className="space-y-2 px-3 py-3 text-xs text-red-600">
                  <p>{error}</p>
                  <button
                    type="button"
                    className="font-medium underline underline-offset-2"
                    onClick={() => void load()}
                  >
                    Try again
                  </button>
                </div>
              ) : null}
            </Command.List>
            {nextCursor && !error ? (
              <div className="border-t border-border p-1.5">
                <button
                  type="button"
                  disabled={loading}
                  onClick={() => void load(nextCursor)}
                  className="w-full rounded-xl px-3 py-2 text-left text-xs font-medium text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-60"
                >
                  Load more branches
                </button>
              </div>
            ) : null}
          </Command>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

export function ProjectBranchBadge({
  branch,
  baseRef,
  baseSHA,
}: {
  branch: string;
  baseRef?: string;
  baseSHA?: string;
}) {
  const title =
    baseRef && baseSHA
      ? `Based on ${baseRef} @ ${baseSHA.slice(0, 7)}`
      : baseRef
        ? `Based on ${baseRef}`
        : "Current project task branch";
  return (
    <span
      title={title}
      className="flex max-w-[12rem] min-w-0 items-center gap-1.5 rounded-full border border-border px-2.5 py-1.5 text-[12.5px] font-medium text-foreground"
    >
      <GitBranch className="size-4 shrink-0 text-indigo-600" />
      <span className="truncate">{branch || "Loading branch"}</span>
    </span>
  );
}
