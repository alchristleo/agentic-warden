import type { NextConfig } from "next";

import { assertBuildEnv } from "./lib/env";
import { securityHeaders } from "./lib/headers";

assertBuildEnv();

const nextConfig: NextConfig = {
  poweredByHeader: false,
  async headers() {
    if (process.env.NODE_ENV !== "production") return [];
    return [{ source: "/:path*", headers: securityHeaders() }];
  },
};

export default nextConfig;
