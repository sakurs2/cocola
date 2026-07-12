export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

type LoginErr = { error?: { code?: string; message?: string } };

export async function POST(req: Request) {
  let body: { identifier?: string; password?: string };
  try {
    body = (await req.json()) as { identifier?: string; password?: string };
  } catch {
    return Response.json({ error: "invalid request" }, { status: 400 });
  }

  const identifier = String(body.identifier ?? "").trim();
  const password = String(body.password ?? "");
  if (!identifier || !password) {
    return Response.json({ error: "invalid request" }, { status: 400 });
  }

  let upstream: Response;
  try {
    upstream = await fetch(`${ADMIN_URL}/auth/login`, {
      method: "POST",
      cache: "no-store",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ identifier, password }),
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return Response.json({ error: `admin-api unreachable: ${msg}` }, { status: 502 });
  }

  if (upstream.ok) {
    return Response.json({ ok: true });
  }

  let code = "";
  try {
    const payload = (await upstream.json()) as LoginErr;
    code = payload.error?.code ?? "";
  } catch {
    // Keep the generic failure below.
  }

  if (code === "ACCOUNT_DISABLED") {
    return Response.json(
      { error: { code: "ACCOUNT_DISABLED", message: "account disabled" } },
      { status: 403, headers: { "x-cocola-auth": "account-disabled" } },
    );
  }

  if (upstream.status === 401) {
    return Response.json({ error: "invalid credentials" }, { status: 401 });
  }

  return Response.json({ error: "sign in failed" }, { status: upstream.status });
}
