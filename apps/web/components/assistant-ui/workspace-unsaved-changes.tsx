"use client";

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
import { ActionConfirmDialog } from "@/components/ui/action-dialog";

type WorkspaceUnsavedChangesValue = {
  dirty: boolean;
  setDirty: (dirty: boolean) => void;
  runWithNavigationGuard: (action: () => void | Promise<void>) => void;
};

const cleanWorkspace: WorkspaceUnsavedChangesValue = {
  dirty: false,
  setDirty: () => undefined,
  runWithNavigationGuard: (action) => {
    void action();
  },
};

const WorkspaceUnsavedChangesContext = createContext<WorkspaceUnsavedChangesValue>(cleanWorkspace);

export function WorkspaceUnsavedChangesProvider({ children }: { children: ReactNode }) {
  const [dirty, setDirty] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const pendingAction = useRef<(() => void | Promise<void>) | null>(null);

  const runWithNavigationGuard = useCallback(
    (action: () => void | Promise<void>) => {
      if (!dirty) {
        void action();
        return;
      }
      pendingAction.current = action;
      setDialogOpen(true);
    },
    [dirty],
  );

  useEffect(() => {
    if (dirty) return;
    pendingAction.current = null;
    setDialogOpen(false);
  }, [dirty]);

  const value = useMemo(
    () => ({ dirty, setDirty, runWithNavigationGuard }),
    [dirty, runWithNavigationGuard],
  );

  return (
    <WorkspaceUnsavedChangesContext.Provider value={value}>
      {children}
      <ActionConfirmDialog
        open={dialogOpen}
        title="Discard unsaved changes?"
        description="This Wiki page has changes that have not been saved. Continue only if you do not need them."
        confirmLabel="Discard and continue"
        cancelLabel="Keep editing"
        tone="warning"
        onOpenChange={(open) => {
          setDialogOpen(open);
          if (!open) pendingAction.current = null;
        }}
        onConfirm={() => {
          const action = pendingAction.current;
          pendingAction.current = null;
          setDialogOpen(false);
          if (action) void action();
        }}
      />
    </WorkspaceUnsavedChangesContext.Provider>
  );
}

export function useWorkspaceUnsavedChanges() {
  return useContext(WorkspaceUnsavedChangesContext);
}
