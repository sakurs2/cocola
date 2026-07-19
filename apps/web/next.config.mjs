/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  transpilePackages: ["@cocola/ts-common"],
  // The Preview Proxy iframe (code-server, dev servers) is served under
  // /api/preview/{id}/{port}/ and relies on that trailing slash: code-server
  // emits RELATIVE asset paths and a `./?folder=...` redirect, which the browser
  // resolves against the iframe URL's directory. Next's default trailing-slash
  // 308 would strip the slash, making the browser resolve relatives one segment
  // up and drop the /{port}/ segment -> gateway 404. Skip that redirect so the
  // catch-all route keeps the slash and relative resolution stays correct.
  skipTrailingSlashRedirect: true,
};
export default nextConfig;
