import NextAuth from "next-auth";
import Credentials from "next-auth/providers/credentials";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

type CocolaLoginUser = {
  id: string;
  username: string;
  email: string;
  name: string;
  role: "user" | "admin";
  enabled: boolean;
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

export const { handlers, auth, signIn, signOut } = NextAuth({
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
    jwt({ token, user }) {
      if (user) {
        const u = user as CocolaLoginUser;
        token.id = u.id;
        token.username = u.username;
        token.email = u.email;
        token.name = u.name;
        token.role = u.role;
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
      }
      return session;
    },
  },
});
