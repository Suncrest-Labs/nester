/**
 * Pages hidden while the app ships a single savings product (the Nester
 * Savings Rate). The page modules stay in the tree; these redirects keep
 * old links and bookmarks from landing on them.
 */
const RETIRED_PATHS = ["/vaults", "/dashboard/vaults", "/portfolio", "/stocks"];

/** Former dashboard sub-pages whose content now lives elsewhere. */
const MOVED_PATHS = [
  { source: "/dashboard/history", destination: "/activity" },
  { source: "/dashboard/analytics", destination: "/dashboard" },
];

export const retiredRouteRedirects = [
  ...RETIRED_PATHS.flatMap((path) => [
    { source: path, destination: "/dashboard", permanent: false },
    { source: `${path}/:path*`, destination: "/dashboard", permanent: false },
  ]),
  ...MOVED_PATHS.map((r) => ({ ...r, permanent: false })),
];
