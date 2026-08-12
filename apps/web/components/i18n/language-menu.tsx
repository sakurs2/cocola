"use client";

import { Dropdown, Tooltip } from "@heroui/react";
import { Check, Globe2 } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useEffect, useState, useTransition } from "react";

import { isLocale, type Locale, SUPPORTED_LOCALES } from "@/i18n/config";

export function LanguageMenu() {
  const locale = useLocale();
  const t = useTranslations("common.language");
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  const [error, setError] = useState("");

  useEffect(() => {
    if (!error) return;
    const timeout = window.setTimeout(() => setError(""), 4000);
    return () => window.clearTimeout(timeout);
  }, [error]);

  const selectLocale = async (key: React.Key) => {
    const nextLocale = String(key);
    if (!isLocale(nextLocale) || nextLocale === locale) return;

    setError("");
    try {
      const response = await fetch("/api/preferences/locale", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ locale: nextLocale }),
      });
      if (!response.ok) throw new Error("locale update failed");
      startTransition(() => router.refresh());
    } catch {
      setError(t("changeFailed"));
    }
  };

  const labels: Record<Locale, string> = {
    en: t("english"),
    "zh-CN": t("simplifiedChinese"),
  };

  return (
    <div className="relative flex shrink-0 items-center">
      <Dropdown>
        <Tooltip delay={0}>
          <Dropdown.Trigger
            aria-label={t("label")}
            className="hover:bg-default-hover flex size-8 items-center justify-center rounded-full"
            isDisabled={isPending}
          >
            <Globe2 className={`size-4 ${isPending ? "animate-pulse" : ""}`} />
          </Dropdown.Trigger>
          <Tooltip.Content placement="bottom">{t("label")}</Tooltip.Content>
        </Tooltip>
        <Dropdown.Popover placement="bottom end">
          <Dropdown.Menu aria-label={t("menuLabel")} onAction={selectLocale}>
            {SUPPORTED_LOCALES.map((option) => (
              <Dropdown.Item key={option} id={option} textValue={labels[option]}>
                <span className="flex w-full items-center justify-between gap-6">
                  <span>{labels[option]}</span>
                  {option === locale ? <Check className="text-accent size-4" /> : null}
                </span>
              </Dropdown.Item>
            ))}
          </Dropdown.Menu>
        </Dropdown.Popover>
      </Dropdown>
      {error ? (
        <span
          role="alert"
          className="bg-danger text-danger-foreground absolute right-0 top-full z-50 mt-2 w-max max-w-64 rounded-xl px-3 py-2 text-xs shadow-lg"
        >
          {error}
        </span>
      ) : null}
    </div>
  );
}
