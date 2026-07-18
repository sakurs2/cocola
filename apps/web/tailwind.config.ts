import type { Config } from "tailwindcss";
import animate from "tailwindcss-animate";
import defaultTheme from "tailwindcss/defaultTheme";

// Tailwind v3 config wired to the shadcn CSS variables defined in
// app/globals.css. The assistant-ui components reference these semantic color
// names (bg-background, text-muted-foreground, border-border, bg-sidebar, …)
// plus the radius scale; without this mapping those classes resolve to nothing.
const config: Config = {
  darkMode: ["class"],
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}", "./lib/**/*.{ts,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        // Geist is the primary UI + code family, injected as the
        // --font-geist-* CSS variables by next/font/local in app/layout.tsx
        // (self-hosted, no external CDN). We keep Tailwind's default system
        // stack as fallback and add CJK faces so Chinese content renders with
        // a native, high-quality face instead of a serif fallback.
        sans: [
          "var(--font-geist-sans)",
          "PingFang SC",
          "Microsoft YaHei",
          "Noto Sans SC",
          ...defaultTheme.fontFamily.sans,
        ],
        // System/native stack (SF Pro + PingFang SC on macOS) used by the
        // session status panel so it matches the platform chrome look.
        system: [
          "-apple-system",
          "BlinkMacSystemFont",
          "\"SF Pro Text\"",
          "\"PingFang SC\"",
          "\"Segoe UI\"",
          "\"Microsoft YaHei\"",
          ...defaultTheme.fontFamily.sans,
        ],
        mono: [
          "var(--font-geist-mono)",
          ...defaultTheme.fontFamily.mono,
        ],
      },
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        sidebar: {
          DEFAULT: "hsl(var(--sidebar))",
          foreground: "hsl(var(--sidebar-foreground))",
          border: "hsl(var(--sidebar-border))",
          accent: "hsl(var(--sidebar-accent))",
          "accent-foreground": "hsl(var(--sidebar-accent-foreground))",
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
    },
  },
  plugins: [animate],
};
export default config;
