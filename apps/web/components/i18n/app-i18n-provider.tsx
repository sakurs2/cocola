"use client";

import { I18nProvider } from "@react-aria/i18n";
import { NextIntlClientProvider, type AbstractIntlMessages } from "next-intl";
import type { ReactNode } from "react";

import type { Locale } from "@/i18n/config";

export function AppI18nProvider({
  children,
  locale,
  messages,
  timeZone,
}: {
  children: ReactNode;
  locale: Locale;
  messages: AbstractIntlMessages;
  timeZone: string;
}) {
  return (
    <NextIntlClientProvider locale={locale} messages={messages} timeZone={timeZone}>
      <I18nProvider locale={locale}>{children}</I18nProvider>
    </NextIntlClientProvider>
  );
}
