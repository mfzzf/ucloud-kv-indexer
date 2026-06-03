import type { NextConfig } from "next";
import { fileURLToPath } from "url";
import { dirname } from "path";
const root = dirname(fileURLToPath(import.meta.url));
const gatewayBase = (process.env.KVI_API_BASE || "http://127.0.0.1:8095").replace(
  /\/$/,
  "",
);
const config: NextConfig = {
  reactStrictMode: true,
  turbopack: { root },
  async rewrites() {
    return [
      {
        source: "/api/kvi/:path*",
        destination: `${gatewayBase}/:path*`,
      },
    ];
  },
};
export default config;
