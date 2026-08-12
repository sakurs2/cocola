import { cookies, headers } from "next/headers";
import { getRequestConfig } from "next-intl/server";

import { DEFAULT_TIME_ZONE, LOCALE_COOKIE_NAME, resolveLocale } from "@/i18n/config";
import { messagesByLocale } from "@/i18n/messages";

export default getRequestConfig(async () => {
  const [cookieStore, headerStore] = await Promise.all([cookies(), headers()]);
  const locale = resolveLocale(
    cookieStore.get(LOCALE_COOKIE_NAME)?.value,
    headerStore.get("accept-language"),
  );

  return { locale, messages: messagesByLocale[locale], timeZone: DEFAULT_TIME_ZONE };
});
