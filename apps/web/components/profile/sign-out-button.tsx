"use client";

import { LogOut } from "lucide-react";
import { signOut } from "next-auth/react";
import { useState } from "react";

// Logout lives on the profile page (the sidebar no longer carries a sign-out
// affordance). Client component so it can call next-auth's signOut directly.
export function SignOutButton() {
  const [busy, setBusy] = useState(false);
  return (
    <button
      type="button"
      disabled={busy}
      onClick={() => {
        setBusy(true);
        void signOut({ callbackUrl: "/login" });
      }}
      className="inline-flex h-9 items-center gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 text-sm font-medium text-destructive transition-colors hover:bg-destructive/10 disabled:opacity-50"
    >
      <LogOut className="size-4" />
      Sign out
    </button>
  );
}
