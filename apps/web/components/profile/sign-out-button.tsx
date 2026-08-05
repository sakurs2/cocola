"use client";

import { LogOut } from "lucide-react";
import { Button } from "@heroui/react";
import { signOut } from "next-auth/react";
import { useState } from "react";

// Logout lives on the profile page (the sidebar no longer carries a sign-out
// affordance). Client component so it can call next-auth's signOut directly.
export function SignOutButton() {
  const [busy, setBusy] = useState(false);
  return (
    <Button
      isDisabled={busy}
      isPending={busy}
      variant="danger-soft"
      onPress={() => {
        setBusy(true);
        void signOut({ callbackUrl: "/login" });
      }}
    >
      <LogOut className="size-4" />
      Sign out
    </Button>
  );
}
