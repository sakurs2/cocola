"use client";

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

type WorkspaceUnsavedChangesValue = {
  dirty: boolean;
  setDirty: (dirty: boolean) => void;
  confirmNavigation: () => boolean;
};

const cleanWorkspace: WorkspaceUnsavedChangesValue = {
  dirty: false,
  setDirty: () => undefined,
  confirmNavigation: () => true,
};

const WorkspaceUnsavedChangesContext = createContext<WorkspaceUnsavedChangesValue>(cleanWorkspace);

export function WorkspaceUnsavedChangesProvider({ children }: { children: ReactNode }) {
  const [dirty, setDirty] = useState(false);
  const confirmNavigation = useCallback(
    () => !dirty || window.confirm("This page has unsaved changes. Discard them and continue?"),
    [dirty],
  );
  const value = useMemo(() => ({ dirty, setDirty, confirmNavigation }), [dirty, confirmNavigation]);

  return (
    <WorkspaceUnsavedChangesContext.Provider value={value}>
      {children}
    </WorkspaceUnsavedChangesContext.Provider>
  );
}

export function useWorkspaceUnsavedChanges() {
  return useContext(WorkspaceUnsavedChangesContext);
}
