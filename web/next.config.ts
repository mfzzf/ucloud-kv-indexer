import type { NextConfig } from "next";
import { fileURLToPath } from "url";
import { dirname } from "path";
const root = dirname(fileURLToPath(import.meta.url));
const config: NextConfig = {
  reactStrictMode: true,
  turbopack: { root },
};
export default config;
