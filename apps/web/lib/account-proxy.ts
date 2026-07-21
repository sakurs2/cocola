import { unstable_update } from "@/auth";
import { isAuthFail, requireUser, runtimeAuthHeaders, type SessionUser } from "@/lib/server-auth";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

type AccountRecord = SessionUser & { enabled: boolean };

export async function currentAccount(): Promise<SessionUser | Response> {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  return authResult.user;
}

export async function mutateAccount(
  req: Request,
  path: string,
  method: "PATCH" | "POST",
): Promise<Response> {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;

  const headers = new Headers(authHeaders);
  headers.set("content-type", "application/json");
  let upstream: Response;
  try {
    upstream = await fetch(`${ADMIN_URL}${path}`, {
      method,
      cache: "no-store",
      headers,
      body: await req.text(),
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return Response.json({ error: `admin-api unreachable: ${message}` }, { status: 502 });
  }

  const raw = await upstream.text();
  if (!upstream.ok) {
    return new Response(raw, {
      status: upstream.status,
      headers: { "content-type": upstream.headers.get("content-type") ?? "application/json" },
    });
  }
  const account = JSON.parse(raw) as AccountRecord;
  await unstable_update({
    user: {
      id: account.id,
      username: account.username,
      email: account.email,
      name: account.name,
      role: account.role,
      version: account.version,
    },
  });
  return Response.json(account);
}
