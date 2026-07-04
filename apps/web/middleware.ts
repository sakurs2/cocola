import { auth } from "@/auth";

export default auth((req) => {
  const { pathname } = req.nextUrl;
  const user = req.auth?.user;

  if (pathname === "/login") {
    if (user) return Response.redirect(new URL("/", req.nextUrl.origin));
    return;
  }

  if (!user) {
    const url = new URL("/login", req.nextUrl.origin);
    url.searchParams.set("callbackUrl", req.nextUrl.pathname + req.nextUrl.search);
    return Response.redirect(url);
  }

  if (pathname.startsWith("/admin") && user.role !== "admin") {
    return new Response("forbidden", { status: 403 });
  }
});

export const config = {
  matcher: ["/((?!api|_next/static|_next/image|favicon.ico|brands).*)"],
};
