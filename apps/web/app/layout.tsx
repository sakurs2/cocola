import "./globals.css";
import "./cocola-web-demo.css";
import "@fontsource-variable/inter/wght.css";
import { GeistMono } from "geist/font/mono";
import localFont from "next/font/local";
import { AuthSessionProvider } from "@/components/auth-session-provider";
import { WorkspaceShell } from "@/components/assistant-ui/workspace-shell";
import type { ReactNode } from "react";

export const metadata = {
  title: "cocola",
  description: "Open-source enterprise AI agent platform",
};

// Cormorant Garamond (italic 500, latin subset) is self-hosted too: the .woff2
// lives in app/fonts and next/font/local inlines it at build time (no CDN call).
// Exposed as --font-cormorant, consumed by the homepage tagline only.
const cormorantGaramond = localFont({
  src: "./fonts/cormorant-garamond-italic-500-latin.woff2",
  weight: "500",
  style: "italic",
  display: "swap",
  variable: "--font-cormorant",
});

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${GeistMono.variable} ${cormorantGaramond.variable}`}
    >
      <body className="min-h-screen bg-background font-sans text-foreground">
        <AuthSessionProvider>
          <WorkspaceShell>{children}</WorkspaceShell>
        </AuthSessionProvider>
      </body>
    </html>
  );
}
