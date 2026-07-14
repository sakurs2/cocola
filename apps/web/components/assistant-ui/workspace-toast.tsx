"use client";

import { CheckCircle } from "@phosphor-icons/react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";

type ToastContextValue = {
  showSuccess: (message: string) => void;
};

type ToastMessage = {
  id: number;
  text: string;
};

const ToastContext = createContext<ToastContextValue | null>(null);

export function WorkspaceToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const nextID = useRef(0);
  const reduceMotion = useReducedMotion();

  const showSuccess = useCallback((message: string) => {
    nextID.current += 1;
    setToast({ id: nextID.current, text: message });
  }, []);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 1800);
    return () => window.clearTimeout(timer);
  }, [toast]);

  return (
    <ToastContext.Provider value={{ showSuccess }}>
      {children}
      <div
        aria-live="polite"
        aria-atomic="true"
        className="pointer-events-none fixed inset-0 z-[100] grid place-items-center px-6"
      >
        <AnimatePresence mode="wait">
          {toast ? (
            <motion.div
              key={toast.id}
              role="status"
              initial={reduceMotion ? false : { opacity: 0, scale: 0.94, y: 8 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={reduceMotion ? { opacity: 0 } : { opacity: 0, scale: 0.97, y: -4 }}
              transition={{ duration: 0.16, ease: "easeOut" }}
              className="flex max-w-[min(28rem,calc(100vw-3rem))] items-center gap-2.5 rounded-2xl border border-slate-700/60 bg-slate-950/90 px-4 py-3 text-sm font-medium text-white shadow-[0_18px_50px_rgb(15_23_42/0.28)] backdrop-blur-xl"
            >
              <CheckCircle className="size-[18px] shrink-0 text-emerald-400" weight="fill" />
              <span className="truncate">{toast.text}</span>
            </motion.div>
          ) : null}
        </AnimatePresence>
      </div>
    </ToastContext.Provider>
  );
}

export function useWorkspaceToast(): ToastContextValue {
  const context = useContext(ToastContext);
  if (!context) throw new Error("useWorkspaceToast must be used within WorkspaceToastProvider");
  return context;
}
