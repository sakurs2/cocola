import NextAuth from "next-auth";
import Credentials from "next-auth/providers/credentials";
import { AUTH_SESSION_VERSION, authTokenUserID } from "@/lib/auth-session-policy.mjs";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

type CocolaLoginUser = {
  id: string;
  username: string;
  email: string;
  name: string;
  role: "user" | "admin";
  enabled: boolean;
  version: number;
};

async function authenticate(identifier: string, password: string): Promise<CocolaLoginUser | null> {
  const res = await fetch(`${ADMIN_URL}/auth/login`, {
    method: "POST",
    cache: "no-store",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ identifier, password }),
  });
  if (!res.ok) return null;
  const body = (await res.json()) as { user?: CocolaLoginUser };
  const user = body.user;
  if (!user?.enabled || !user.email || !user.id) return null;
  return user;
}

function applyTrustedUser(token: Record<string, unknown>, user: CocolaLoginUser) {
  token.id = user.id;
  token.username = user.username;
  token.email = user.email;
  token.name = user.name;
  token.role = user.role;
  token.version = user.version;
  token.authVersion = AUTH_SESSION_VERSION;
}

async function refreshAuthenticatedUser(userID: string): Promise<CocolaLoginUser | null> {
  const headers: Record<string, string> = { "x-cocola-admin": "auth-session-refresh" };
  const adminKey = process.env.COCOLA_ADMIN_KEY;
  if (adminKey) headers.authorization = `Bearer ${adminKey}`;
  try {
    const res = await fetch(`${ADMIN_URL}/admin/users/${encodeURIComponent(userID)}`, {
      method: "GET",
      cache: "no-store",
      headers,
    });
    if (!res.ok) return null;
    const user = (await res.json()) as CocolaLoginUser;
    if (!user.enabled || user.id !== userID || !user.email) return null;
    return user;
  } catch {
    return null;
  }
}

export const { handlers, auth, signIn, signOut, unstable_update } = NextAuth({
  session: { strategy: "jwt" },
  pages: { signIn: "/login" },
  providers: [
    Credentials({
      credentials: {
        identifier: { label: "Username or email", type: "text" },
        password: { label: "Password", type: "password" },
      },
      async authorize(credentials) {
        const identifier = String(credentials?.identifier ?? "").trim();
        const password = String(credentials?.password ?? "");
        if (!identifier || !password) return null;
        return authenticate(identifier, password);
      },
    }),
  ],
  callbacks: {
    authorized: ({ auth }) => Boolean(auth?.user),
    async jwt({ token, user, trigger }) {
      if (user) {
        applyTrustedUser(token, user as CocolaLoginUser);
        return token;
      }

      const userID = authTokenUserID(token);
      if (!userID) return null;
      if (trigger === "update") {
        const refreshed = await refreshAuthenticatedUser(userID);
        if (!refreshed) return null;
        applyTrustedUser(token, refreshed);
      }
      return token;
    },
    session({ session, token }) {
      if (session.user) {
        session.user.id = String(token.id ?? "");
        session.user.email = String(token.email ?? "");
        session.user.name = String(token.name ?? token.email ?? "");
        session.user.username = String(token.username ?? "");
        session.user.role = token.role === "admin" ? "admin" : "user";
        session.user.version = Number(token.version ?? 1);
      }
      return session;
    },
  },
});
