import { NextResponse } from "next/server";

import { isLocale, LOCALE_COOKIE_MAX_AGE, LOCALE_COOKIE_NAME } from "@/i18n/config";

export async function POST(request: Request) {
  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "unsupported_locale" }, { status: 400 });
  }

  const locale =
    body && typeof body === "object" && "locale" in body
      ? (body as { locale?: unknown }).locale
      : undefined;
  if (!isLocale(locale)) {
    return NextResponse.json({ error: "unsupported_locale" }, { status: 400 });
  }

  const response = new NextResponse(null, { status: 204 });
  response.cookies.set({
    name: LOCALE_COOKIE_NAME,
    value: locale,
    httpOnly: true,
    maxAge: LOCALE_COOKIE_MAX_AGE,
    path: "/",
    sameSite: "lax",
    secure: requestUsesHTTPS(request),
  });
  return response;
}

function requestUsesHTTPS(request: Request) {
  const forwardedProtocol = request.headers.get("x-forwarded-proto")?.split(",", 1)[0]?.trim();
  if (forwardedProtocol) return forwardedProtocol.toLowerCase() === "https";
  return new URL(request.url).protocol === "https:";
}
