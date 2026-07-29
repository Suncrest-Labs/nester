"use client";

import Link from "next/link";
import { type ReactNode } from "react";

interface NotificationActionLinkProps {
    href: string;
    children: ReactNode;
    className?: string;
    onClick?: () => void;
}

export function NotificationActionLink({
    href,
    children,
    className,
    onClick,
}: NotificationActionLinkProps) {
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
