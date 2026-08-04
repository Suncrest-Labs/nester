"use client";

import Link from "next/link";
import { type ReactNode } from "react";

interface NotificationActionLinkProps {
    href: string;
    children: ReactNode;
    className?: string;
    onClick?: () => void;
}

/**
 * Validates that `href` is either a safe same-origin path (starts with a
 * single `/` and not `//`) or an absolute http(s) URL. Protocol-relative URLs
 * and unsafe schemes such as `javascript:` are rejected and render nothing.
 */
function isSafeHref(href: string): boolean {
    // Same-origin path: must start with "/" but NOT "//" (protocol-relative)
    if (href.startsWith("/") && !href.startsWith("//")) {
        return true;
    }
    // Absolute http(s) URL
    try {
        const url = new URL(href);
        return url.protocol === "http:" || url.protocol === "https:";
    } catch {
        return false;
    }
}

export function NotificationActionLink({
    href,
    children,
    className,
    onClick,
}: NotificationActionLinkProps) {
    if (!isSafeHref(href)) {
        // Render a safe fallback span for rejected URLs
        return <span className={className}>{children}</span>;
    }

    if (href.startsWith("/")) {
        return (
            <Link href={href} className={className} onClick={onClick}>
                {children}
            </Link>
        );
    }

    return (
        <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className={className}
            onClick={onClick}
        >
            {children}
        </a>
    );
}
