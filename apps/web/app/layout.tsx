import "./globals.css";
import type { ReactNode } from "react";

export const metadata = {
  title: "cocola",
  description: "Open-source enterprise AI agent platform",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-neutral-50 text-neutral-900">{children}</body>
    </html>
  );
}
