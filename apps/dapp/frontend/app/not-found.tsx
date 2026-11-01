import Link from "next/link";
import Image from "next/image";
import { BUTTON_PRIMARY, BUTTON_SECONDARY } from "@/components/ui/page-kit";

export default function NotFound() {
    return (
        <div className="flex min-h-screen flex-col items-center justify-center bg-background px-4">
            <Link href="/" className="mb-16 flex items-center gap-2.5">
                <Image src="/logo.png" alt="Nester" width={32} height={32} className="rounded-lg" />
                <span className="text-[15px] font-semibold text-foreground">Nester</span>
            </Link>

            <div className="w-full max-w-md text-center">
                <p className="text-[13px] font-medium text-muted-foreground">Page not found</p>
                <p className="mt-3 font-heading text-6xl font-light tabular-nums text-foreground">404</p>
                <h1 className="mt-6 text-[16px] font-semibold text-foreground">
                    This page doesn&apos;t exist
                </h1>
                <p className="mt-2 text-sm text-muted-foreground">
                    The link may be old or mistyped. Head back to your savings to carry on.
                </p>

                <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
                    <Link href="/dashboard" className={BUTTON_PRIMARY}>
                        Go to savings
                    </Link>
                    <Link href="/" className={BUTTON_SECONDARY}>
                        Home
                    </Link>
                </div>
            </div>
        </div>
    );
}
