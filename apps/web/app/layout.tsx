import "./globals.css";
import type { ReactNode } from "react";

export const metadata = {
  title: "cocola",
  description: "Open-source enterprise AI agent platform",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className="dark">
      <body className="min-h-screen bg-background text-foreground">{children}</body>
    </html>
  );
}
