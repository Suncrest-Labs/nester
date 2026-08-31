import { AppShell } from "@/components/app-shell";
import { VaultDetailSkeleton } from "@/components/skeletons/page-skeletons";

/**
 * Route-level loading UI. Next.js renders this while the segment is being
 * prepared, so navigation never shows a blank frame. The navigation shell stays
 * mounted so the user can move elsewhere while this loads.
 */
export default function Loading() {
  return (
    <AppShell>
      <VaultDetailSkeleton />
    </AppShell>
  );
}
