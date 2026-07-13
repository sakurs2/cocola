import { auth } from "@/auth";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";
const RUNTIME_TOKEN_TTL_SECONDS = 600;

export type SessionUser = {
  id: string;
  username: string;
  email: string;
  name: string;
  role: "user" | "admin";
};

type AuthOk = { user: SessionUser };
type AuthFail = { response: Response };

type AuthUserRecord = {
  id: string;
  username: string;
  email: string;
  name?: string;
  role: "user" | "admin";
  enabled: boolean;
};

export function isAuthFail(v: AuthOk | AuthFail): v is AuthFail {
  return "response" in v;
}

function accountDisabledResponse() {
  return Response.json(
    { error: { code: "ACCOUNT_DISABLED", message: "account disabled" } },
    { status: 401, headers: { "x-cocola-auth": "account-disabled" } },
  );
}

export async function requireUser(): Promise<AuthOk | AuthFail> {
  const session = await auth();
  const user = session?.user;
  if (!user?.email || !user.id) {
    return { response: Response.json({ error: "unauthenticated" }, { status: 401 }) };
  }

  let record: AuthUserRecord;
  try {
    const upstream = await fetch(
      `${ADMIN_URL}/admin/users/lookup?email=${encodeURIComponent(user.email)}`,
      {
        method: "GET",
        cache: "no-store",
        headers: adminHeaders(
          {
            id: user.id,
            username: user.username || "",
            email: user.email,
            name: user.name || user.email,
            role: user.role === "admin" ? "admin" : "user",
          },
          undefined,
        ),
      },
    );
    if (upstream.status === 403) {
      try {
        const body = (await upstream.clone().json()) as { error?: { code?: string } };
        if (body.error?.code === "ACCOUNT_DISABLED") {
          return { response: accountDisabledResponse() };
        }
      } catch {
        // Fall through to the generic unauthenticated response.
      }
    }
    if (upstream.status === 404 || upstream.status === 401 || upstream.status === 403) {
      return { response: Response.json({ error: "unauthenticated" }, { status: 401 }) };
    }
    if (!upstream.ok) {
      const text = await upstream.text();
      return {
        response: Response.json(
          { error: `auth user lookup failed: admin-api ${upstream.status}: ${text}` },
          { status: 502 },
        ),
      };
    }
    record = (await upstream.json()) as AuthUserRecord;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return { response: Response.json({ error: `admin-api unreachable: ${msg}` }, { status: 502 }) };
  }

  if (!record.enabled) {
    return { response: accountDisabledResponse() };
  }

  return {
    user: {
      id: record.id,
      username: record.username,
      email: record.email,
      name: record.name || record.email,
      role: record.role === "admin" ? "admin" : "user",
    },
  };
}

export async function requireAdmin(): Promise<AuthOk | AuthFail> {
  const res = await requireUser();
  if (isAuthFail(res)) return res;
  if (res.user.role !== "admin") {
    return { response: Response.json({ error: "forbidden" }, { status: 403 }) };
  }
  return res;
}

export function adminHeaders(user: SessionUser, contentType?: string): HeadersInit {
  const headers: Record<string, string> = { "x-cocola-admin": user.email };
  if (contentType) headers["content-type"] = contentType;
  const key = process.env.COCOLA_ADMIN_KEY;
  if (key) headers.authorization = `Bearer ${key}`;
  return headers;
}

export async function runtimeAuthHeaders(user: SessionUser): Promise<HeadersInit | Response> {
  let upstream: Response;
  try {
    upstream = await fetch(`${ADMIN_URL}/admin/runtime-token`, {
      method: "POST",
      cache: "no-store",
      headers: adminHeaders(user, "application/json"),
      body: JSON.stringify({
        email: user.email,
        ttl_seconds: RUNTIME_TOKEN_TTL_SECONDS,
      }),
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return Response.json({ error: `admin-api unreachable: ${msg}` }, { status: 502 });
  }

  if (!upstream.ok) {
    if (upstream.status === 403) {
      try {
        const body = (await upstream.clone().json()) as { error?: { code?: string } };
        if (body.error?.code === "ACCOUNT_DISABLED") {
          return accountDisabledResponse();
        }
      } catch {
        // Fall through to the upstream diagnostic.
      }
    }
    const text = await upstream.text();
    return Response.json(
      { error: `runtime token failed: admin-api ${upstream.status}: ${text}` },
      { status: 502 },
    );
  }

  const body = (await upstream.json()) as { token?: string; ttl_seconds?: number };
  if (!body.token) {
    return Response.json(
      { error: "runtime token missing from admin-api response" },
      { status: 502 },
    );
  }
  return { authorization: `Bearer ${body.token}` };
}
