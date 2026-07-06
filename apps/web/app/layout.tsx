import "./globals.css";
import { AuthSessionProvider } from "@/components/auth-session-provider";
import { WorkspaceShell } from "@/components/assistant-ui/workspace-shell";
import type { ReactNode } from "react";

export const metadata = {
  title: "cocola",
  description: "Open-source enterprise AI agent platform",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className="dark">
      <body className="min-h-screen bg-background text-foreground">
        <AuthSessionProvider>
          <WorkspaceShell>{children}</WorkspaceShell>
        </AuthSessionProvider>
      </body>
    </html>
  );
}
