"use client";

import { Moon, Sun } from "@gravity-ui/icons";
import { Button, Tooltip } from "@heroui/react";
import { useEffect, useSyncExternalStore } from "react";

type ColorMode = "light" | "dark";

const COLOR_MODE_KEY = "cocola:color-mode";
const subscribers = new Set<() => void>();

function applyColorMode(mode: ColorMode) {
  document.documentElement.classList.toggle("dark", mode === "dark");
  document.documentElement.classList.toggle("light", mode === "light");
  document.documentElement.dataset.theme = mode;
  document.documentElement.style.colorScheme = mode;
}

export function WorkspaceThemeToggle() {
  const mode = useSyncExternalStore(
    (subscriber) => {
      subscribers.add(subscriber);
      return () => subscribers.delete(subscriber);
    },
    () => (document.documentElement.dataset.theme === "dark" ? "dark" : "light"),
    () => "light",
  );

  useEffect(() => {
    const stored = window.localStorage.getItem(COLOR_MODE_KEY);
    const initialMode: ColorMode =
      stored === "light" || stored === "dark"
        ? stored
        : window.matchMedia("(prefers-color-scheme: dark)").matches
          ? "dark"
          : "light";
    applyColorMode(initialMode);
    subscribers.forEach((subscriber) => subscriber());
  }, []);

  const nextMode: ColorMode = mode === "light" ? "dark" : "light";
  const label = `Switch to ${nextMode} mode`;

  return (
    <Tooltip delay={0}>
      <Button
        isIconOnly
        aria-label={label}
        className="shrink-0"
        size="sm"
        variant="ghost"
        onPress={() => {
          applyColorMode(nextMode);
          window.localStorage.setItem(COLOR_MODE_KEY, nextMode);
          subscribers.forEach((subscriber) => subscriber());
        }}
      >
        {nextMode === "dark" ? <Moon className="size-4" /> : <Sun className="size-4" />}
      </Button>
      <Tooltip.Content>{label}</Tooltip.Content>
    </Tooltip>
  );
}
