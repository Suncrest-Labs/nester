"use client";

import { cn } from "@/lib/utils";

export function EmptyState({
    icon: Icon,
    title,
    description,
    className,
    children,
}: {
    icon?: React.ComponentType<{ className?: string }>;
    title: string;
    description: string;
    className?: string;
    children?: React.ReactNode;
}) {
    return (
        <div
            className={cn(
                "flex flex-col items-center justify-center py-10 text-center sm:py-12",
                className
            )}
        >
            {Icon && (
                <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-secondary sm:h-14 sm:w-14">
                    <Icon className="h-5 w-5 text-muted-foreground sm:h-6 sm:w-6" />
                </div>
            )}
            <p className="text-sm font-medium text-foreground/80">{title}</p>
            <p className="mt-1 max-w-sm text-xs leading-relaxed text-muted-foreground sm:text-sm">
                {description}
            </p>
            {children ? <div className="mt-5">{children}</div> : null}
        </div>
    );
}
