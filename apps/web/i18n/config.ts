export const SUPPORTED_LOCALES = ["en", "zh-CN"] as const;
export const DEFAULT_LOCALE = "en" as const;
export const DEFAULT_TIME_ZONE = "UTC";
export const LOCALE_COOKIE_NAME = "cocola_locale";
export const LOCALE_COOKIE_MAX_AGE = 60 * 60 * 24 * 365;

export type Locale = (typeof SUPPORTED_LOCALES)[number];

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && SUPPORTED_LOCALES.includes(value as Locale);
}

export function localeFromAcceptLanguage(value: string | null | undefined): Locale {
  if (!value) return DEFAULT_LOCALE;

  const candidates = value
    .split(",")
    .map((entry, index) => {
      const [language = "", ...parameters] = entry.trim().split(";");
      const qualityParameter = parameters.find((parameter) => parameter.trim().startsWith("q="));
      const quality = qualityParameter ? Number.parseFloat(qualityParameter.trim().slice(2)) : 1;
      return {
        index,
        language: language.toLowerCase(),
        quality: Number.isFinite(quality) ? quality : 0,
      };
    })
    .filter((candidate) => candidate.language && candidate.quality > 0)
    .sort((left, right) => right.quality - left.quality || left.index - right.index);

  const preferred = candidates[0]?.language;
  return preferred === "zh" || preferred?.startsWith("zh-") ? "zh-CN" : DEFAULT_LOCALE;
}

export function resolveLocale(
  cookieValue: string | null | undefined,
  acceptLanguage: string | null | undefined,
): Locale {
  return isLocale(cookieValue) ? cookieValue : localeFromAcceptLanguage(acceptLanguage);
}
