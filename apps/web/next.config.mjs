import { fileURLToPath } from "node:url";

/** @type {import('next').NextConfig} */
const workspaceRoot = fileURLToPath(new URL("../..", import.meta.url));

const nextConfig = {
  reactStrictMode: true,
  outputFileTracingRoot: workspaceRoot,
  transpilePackages: ["@cocola/ts-common"],
  webpack(config) {
    // HeroUI Pro imports Motion's compatibility entrypoints. Cocola already
    // ships the same runtime as framer-motion, so resolve those thin wrapper
    // paths directly instead of loading a duplicate animation runtime.
    config.resolve.alias = {
      ...config.resolve.alias,
      "motion/react": "framer-motion",
      "motion/react-m": "framer-motion/m",
    };
    return config;
  },
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
