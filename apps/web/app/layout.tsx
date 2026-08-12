import "./globals.css";
import "./cocola-web-demo.css";
import "@fontsource-variable/inter/wght.css";
import { GeistMono } from "geist/font/mono";
import localFont from "next/font/local";
import { getLocale, getMessages, getTimeZone, getTranslations } from "next-intl/server";
import { AuthSessionProvider } from "@/components/auth-session-provider";
import { WorkspaceShell } from "@/components/assistant-ui/workspace-shell";
import { AppI18nProvider } from "@/components/i18n/app-i18n-provider";
import type { ReactNode } from "react";

export async function generateMetadata() {
  const t = await getTranslations("common.metadata");
  return { title: "cocola", description: t("description") };
}

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

export default async function RootLayout({ children }: { children: ReactNode }) {
  const [locale, messages, timeZone] = await Promise.all([
    getLocale(),
    getMessages(),
    getTimeZone(),
  ]);

  return (
    <html
      lang={locale}
      suppressHydrationWarning
      className={`${GeistMono.variable} ${cormorantGaramond.variable}`}
    >
      <body className="min-h-screen bg-background font-sans text-foreground">
        <AppI18nProvider locale={locale} messages={messages} timeZone={timeZone}>
          <AuthSessionProvider>
            <WorkspaceShell>{children}</WorkspaceShell>
          </AuthSessionProvider>
        </AppI18nProvider>
      </body>
    </html>
  );
}
