import type { NextConfig } from "next";

import { assertBuildEnv } from "./lib/env";

assertBuildEnv();

const nextConfig: NextConfig = {
  /* config options here */
};

export default nextConfig;
