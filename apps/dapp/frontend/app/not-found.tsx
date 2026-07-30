import Link from "next/link";
import Image from "next/image";
import { ArrowLeft, Home } from "lucide-react";

export default function NotFound() {
    return (
        <div className="min-h-screen bg-white dark:bg-[#100F0F] flex flex-col items-center justify-center px-4">
            {/* Logo */}
            <Link href="/" className="flex items-center gap-2.5 mb-16">
                <Image
                    src="/logo.png"
                    alt="Nester"
                    width={36}
                    height={36}
                    className="rounded-xl"
                />
                <span className="font-heading text-[15px] font-medium text-black dark:text-white">
                    Nester
                </span>
            </Link>

            {/* Error code */}
            <p className="font-mono text-[120px] sm:text-[160px] leading-none font-bold text-black/[0.04] dark:text-white/[0.04] select-none">
                404
            </p>

            <div className="-mt-4 sm:-mt-6 text-center">
                <h1 className="text-xl sm:text-2xl text-black dark:text-white mb-3">
                    Page not found
                </h1>
                <p className="text-sm text-black/40 dark:text-white/40 max-w-sm leading-relaxed">
                    The page you&apos;re looking for doesn&apos;t exist or has
                    been moved.
                </p>
            </div>

            {/* Actions */}
            <div className="mt-10 flex items-center gap-3">
                <Link
                    href="/"
                    className="inline-flex items-center gap-2 rounded-full bg-black dark:bg-blue-600 px-5 py-2.5 text-sm text-white transition-opacity hover:opacity-75"
                >
                    <Home className="h-3.5 w-3.5" />
                    Go home
                </Link>
                <Link
                    href="/dashboard"
                    className="inline-flex items-center gap-2 rounded-full border border-black/10 dark:border-white/10 px-5 py-2.5 text-sm text-black/60 dark:text-white/60 transition-colors hover:border-black/20 dark:hover:border-white/20 hover:text-black dark:hover:text-white"
                >
                    <ArrowLeft className="h-3.5 w-3.5" />
                    Dashboard
                </Link>
            </div>

            {/* Footer note */}
            <p className="mt-16 font-mono text-[11px] text-black/20 dark:text-white/20">
                nester.finance
            </p>
        </div>
    );
}
