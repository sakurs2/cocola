"use client";

import { AlertCircle, CheckCircle2 as CheckCircle } from "lucide-react";
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
  showError: (message: string) => void;
};

type ToastMessage = {
  id: number;
  text: string;
  tone: "success" | "error";
};

const ToastContext = createContext<ToastContextValue | null>(null);

export function WorkspaceToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const nextID = useRef(0);
  const reduceMotion = useReducedMotion();

  const showToast = useCallback((message: string, tone: ToastMessage["tone"]) => {
    nextID.current += 1;
    setToast({ id: nextID.current, text: message, tone });
  }, []);
  const showSuccess = useCallback((message: string) => showToast(message, "success"), [showToast]);
  const showError = useCallback((message: string) => showToast(message, "error"), [showToast]);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), toast.tone === "error" ? 3200 : 1800);
    return () => window.clearTimeout(timer);
  }, [toast]);

  return (
    <ToastContext.Provider value={{ showError, showSuccess }}>
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
              role={toast.tone === "error" ? "alert" : "status"}
              initial={reduceMotion ? false : { opacity: 0, scale: 0.94, y: 8 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={reduceMotion ? { opacity: 0 } : { opacity: 0, scale: 0.97, y: -4 }}
              transition={{ duration: 0.16, ease: "easeOut" }}
              className="flex max-w-[min(28rem,calc(100vw-3rem))] items-center gap-2.5 rounded-2xl border border-slate-700/60 bg-slate-950/90 px-4 py-3 text-sm font-medium text-white shadow-[0_18px_50px_rgb(15_23_42/0.28)] backdrop-blur-xl"
            >
              {toast.tone === "error" ? (
                <AlertCircle className="size-[18px] shrink-0 text-red-400" />
              ) : (
                <CheckCircle className="size-[18px] shrink-0 text-emerald-400" />
              )}
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
