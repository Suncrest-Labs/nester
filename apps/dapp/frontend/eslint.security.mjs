import security from "eslint-plugin-security";

// Security-only lint surface, used by the blocking CI gate added for
// nester#1236.
//
// The gate is named "Lint (security rules)" and this is what it runs. Pointing
// it at the full `pnpm lint` surface instead made it fail on 47 pre-existing
// style and react-hooks errors in files no security change had touched, so
// enabling it that way would have red-lined every unrelated dapp PR — which is
// how a gate gets switched back off. The full ruleset still runs in the same
// job as a non-blocking step; tightening those 47 is separate work.
//
// Run with --no-config-lookup so eslint does not also load eslint.config.mjs
// from this directory and reintroduce the very rules this file exists to
// exclude.
export default [
  {
    ignores: [
      ".next/**",
      "out/**",
      "build/**",
      "node_modules/**",
      "next-env.d.ts",
      "**/*.test.ts",
      "**/*.test.tsx",
    ],
  },
  security.configs.recommended,
];
