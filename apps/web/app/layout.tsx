import "./globals.css";
import { GeistSans } from "geist/font/sans";
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

// Geist (sans + mono) is self-hosted: the `geist` package ships the .woff2
// files inside node_modules and next/font/local inlines them at build time, so
// no request ever leaves for Google Fonts or any CDN. The two --font-* CSS
// variables are consumed by Tailwind's font-sans / font-mono (see
// tailwind.config.ts) and applied globally via `font-sans` on <body>.
export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html
      lang="en"
      className={`dark ${GeistSans.variable} ${GeistMono.variable} ${cormorantGaramond.variable}`}
    >
      <body className="min-h-screen bg-background font-sans text-foreground">
        <AuthSessionProvider>
          <WorkspaceShell>{children}</WorkspaceShell>
        </AuthSessionProvider>
      </body>
    </html>
  );
}
