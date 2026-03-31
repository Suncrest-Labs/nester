"use client";

import { SkeletonCard, SkeletonChart, SkeletonLine, SkeletonTable } from "@/components/ui/skeleton";

export function DashboardPageSkeleton() {
    return (
        <div className="min-h-[50vh]">
            <div className="mb-8 md:mb-10">
                <SkeletonLine width="40%" height="1.75rem" className="max-w-xs" />
                <SkeletonLine width="28%" height="0.75rem" className="mt-2 max-w-[180px]" />
            </div>

            <div className="mb-8 grid grid-cols-2 gap-3 sm:mb-10 sm:gap-4 lg:grid-cols-4">
                {Array.from({ length: 4 }).map((_, i) => (
                    <SkeletonCard key={i} className="p-4 sm:p-5">
                        <div className="mb-3 flex justify-between sm:mb-4">
                            <SkeletonLine width="2.25rem" height="2.25rem" className="rounded-xl" />
                        </div>
                        <SkeletonLine width="55%" height="1.5rem" className="sm:h-8" />
                        <SkeletonLine width="70%" height="0.65rem" className="mt-2" />
                    </SkeletonCard>
                ))}
            </div>

            <div className="grid grid-cols-1 gap-4 sm:gap-6 lg:grid-cols-2">
                <SkeletonCard className="p-5 sm:p-6">
                    <div className="mb-5 flex justify-between sm:mb-6">
                        <SkeletonLine width="35%" height="1.1rem" />
                        <SkeletonLine width="22%" height="0.75rem" />
                    </div>
                    <SkeletonChart />
                </SkeletonCard>
                <SkeletonCard className="p-5 sm:p-6">
                    <div className="mb-5 flex justify-between sm:mb-6">
                        <SkeletonLine width="42%" height="1.1rem" />
                        <SkeletonLine width="28%" height="0.75rem" />
                    </div>
                    <div className="space-y-3">
                        <SkeletonLine width="100%" height="4rem" className="rounded-2xl" />
                        <SkeletonLine width="100%" height="3.5rem" className="rounded-2xl" />
                    </div>
                </SkeletonCard>
            </div>

            <SkeletonCard className="mt-4 p-5 sm:mt-6 sm:p-6">
                <SkeletonLine width="30%" height="1.1rem" className="mb-4" />
                <div className="space-y-3">
                    {Array.from({ length: 5 }).map((_, i) => (
                        <SkeletonLine key={i} width="100%" height="3.25rem" className="rounded-2xl" />
                    ))}
                </div>
            </SkeletonCard>
        </div>
    );
}

export function VaultsPageSkeleton() {
    return (
        <div>
            <div className="mb-8 md:mb-10">
                <SkeletonLine width="8rem" height="0.65rem" className="mb-2" />
                <SkeletonLine width="min(420px, 70%)" height="2.25rem" className="sm:h-10" />
                <SkeletonLine width="100%" height="0.85rem" className="mt-2 max-w-2xl" />
                <SkeletonLine width="85%" height="0.85rem" className="mt-1 max-w-2xl" />
            </div>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 sm:gap-6">
                {Array.from({ length: 4 }).map((_, i) => (
                    <SkeletonCard key={i} className="p-6 sm:rounded-3xl sm:p-8">
                        <div className="mb-6 flex justify-between">
                            <SkeletonLine width="2.75rem" height="2.75rem" className="rounded-xl" />
                            <div className="text-right space-y-1">
                                <SkeletonLine width="4rem" height="0.5rem" className="ml-auto" />
                                <SkeletonLine width="5rem" height="1.75rem" className="ml-auto" />
                            </div>
                        </div>
                        <SkeletonLine width="55%" height="1.25rem" className="mb-2" />
                        <SkeletonLine width="100%" height="0.75rem" />
                        <SkeletonLine width="92%" height="0.75rem" className="mt-1" />
                        <div className="mt-6 flex flex-wrap gap-2 border-t border-transparent pt-5">
                            <SkeletonLine width="4.5rem" height="1.5rem" className="rounded-full" />
                            <SkeletonLine width="3.5rem" height="1.5rem" className="rounded-full" />
                        </div>
                        <SkeletonLine width="100%" height="6rem" className="mt-4 rounded-2xl" />
                        <div className="mt-6 flex justify-between">
                            <SkeletonLine width="5rem" height="0.75rem" />
                            <SkeletonLine width="5rem" height="0.75rem" />
                        </div>
                    </SkeletonCard>
                ))}
            </div>

            <SkeletonCard className="mt-8 p-5 sm:mt-12 sm:rounded-3xl sm:p-8">
                <div className="grid grid-cols-1 gap-6 sm:grid-cols-3 sm:gap-8">
                    {Array.from({ length: 3 }).map((_, i) => (
                        <div key={i} className="flex flex-col gap-3">
                            <SkeletonLine width="2.5rem" height="2.5rem" className="rounded-xl" />
                            <SkeletonLine width="60%" height="0.9rem" />
                            <SkeletonLine width="100%" height="0.65rem" />
                            <SkeletonLine width="90%" height="0.65rem" />
                        </div>
                    ))}
                </div>
            </SkeletonCard>
        </div>
    );
}

export function SettlementsPageSkeleton() {
    return (
        <div className="mx-auto max-w-xl">
            <div className="mb-6 text-center sm:mb-8">
                <SkeletonLine width="40%" height="1.35rem" className="mx-auto" />
                <SkeletonLine width="75%" height="0.75rem" className="mx-auto mt-2" />
            </div>
            <SkeletonCard className="overflow-hidden">
                <div className="space-y-4 p-4 sm:p-5">
                    <SkeletonLine width="5rem" height="0.65rem" />
                    <div className="flex items-center gap-3">
                        <SkeletonLine width="100%" height="2.75rem" className="flex-1" />
                        <SkeletonLine width="5.5rem" height="2.5rem" className="rounded-full" />
                    </div>
                    <SkeletonLine width="40%" height="0.6rem" />
                </div>
                <div className="px-4 sm:px-5 py-2">
                    <SkeletonLine width="100%" height="1px" />
                </div>
                <div className="space-y-4 p-4 sm:p-5">
                    <SkeletonLine width="5rem" height="0.65rem" />
                    <div className="flex items-center gap-3">
                        <SkeletonLine width="100%" height="2.75rem" className="flex-1" />
                        <SkeletonLine width="5.5rem" height="2.5rem" className="rounded-full" />
                    </div>
                </div>
                <div className="space-y-3 border-t border-border p-4 sm:p-5">
                    <SkeletonLine width="4rem" height="0.65rem" />
                    <SkeletonLine width="100%" height="3rem" className="rounded-xl" />
                    <SkeletonLine width="6rem" height="0.65rem" />
                    <SkeletonLine width="100%" height="3rem" className="rounded-xl" />
                </div>
            </SkeletonCard>

            <div className="mt-8">
                <SkeletonLine width="40%" height="0.9rem" className="mb-3" />
                <SkeletonTable rows={5} cols={4} />
            </div>
        </div>
    );
}

export function HistoryPageSkeleton() {
    return (
        <div>
            <div className="mb-8 flex flex-col justify-between gap-4 md:flex-row md:items-end">
                <div>
                    <SkeletonLine width="10rem" height="0.65rem" className="mb-2" />
                    <SkeletonLine width="min(280px, 60%)" height="2rem" className="sm:h-10" />
                </div>
                <SkeletonLine width="8rem" height="2.75rem" className="rounded-full" />
            </div>
            <div className="mb-6 space-y-4">
                <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
                    <SkeletonLine width="100%" height="3rem" className="rounded-2xl lg:col-span-2" />
                    <SkeletonLine width="100%" height="3rem" className="rounded-2xl" />
                    <SkeletonLine width="100%" height="3rem" className="rounded-2xl" />
                </div>
                <SkeletonLine width="100%" height="4.5rem" className="rounded-2xl" />
            </div>
            <div className="space-y-3">
                {Array.from({ length: 5 }).map((_, i) => (
                    <SkeletonCard key={i} className="flex flex-wrap items-center gap-4 p-4 sm:p-5">
                        <SkeletonLine width="2.25rem" height="2.25rem" className="rounded-xl" />
                        <div className="min-w-0 flex-1 space-y-2">
                            <SkeletonLine width="40%" height="0.85rem" />
                            <SkeletonLine width="55%" height="0.65rem" />
                        </div>
                        <SkeletonLine width="5rem" height="0.75rem" className="ml-auto" />
                    </SkeletonCard>
                ))}
            </div>
        </div>
    );
}

export function NotificationsPageSkeleton() {
    return (
        <div>
            <div className="mb-8 flex flex-wrap items-start justify-between gap-4">
                <div>
                    <SkeletonLine width="12rem" height="2rem" className="sm:h-10" />
                    <SkeletonLine width="min(100%, 22rem)" height="0.75rem" className="mt-2" />
                </div>
                <SkeletonLine width="9rem" height="2.5rem" className="rounded-full" />
            </div>
            <SkeletonCard className="overflow-hidden rounded-3xl">
                <div className="divide-y divide-border">
                    {Array.from({ length: 3 }).map((_, i) => (
                        <div key={i} className="space-y-3 p-5">
                            <div className="flex justify-between gap-3">
                                <SkeletonLine width="45%" height="0.85rem" />
                                <SkeletonLine width="5rem" height="0.65rem" />
                            </div>
                            <SkeletonLine width="100%" height="0.7rem" />
                            <SkeletonLine width="88%" height="0.7rem" />
                            <SkeletonLine width="6rem" height="0.65rem" />
                        </div>
                    ))}
                </div>
            </SkeletonCard>
        </div>
    );
}

export function SettingsPageSkeleton() {
    return (
        <div className="max-w-4xl space-y-8">
            <div>
                <SkeletonLine width="8rem" height="1.75rem" className="sm:h-9" />
                <SkeletonLine width="min(100%, 24rem)" height="0.75rem" className="mt-2" />
            </div>
            {Array.from({ length: 4 }).map((_, s) => (
                <div key={s} className="space-y-4">
                    <div className="flex items-center gap-2.5 px-1">
                        <SkeletonLine width="1.75rem" height="1.75rem" className="rounded-lg" />
                        <SkeletonLine width="8rem" height="0.65rem" />
                    </div>
                    <SkeletonCard className="p-6">
                        <div className="space-y-4">
                            <SkeletonLine width="100%" height="3rem" className="rounded-xl" />
                            <SkeletonLine width="100%" height="3rem" className="rounded-xl" />
                            <SkeletonLine width="70%" height="0.75rem" />
                        </div>
                    </SkeletonCard>
                </div>
            ))}
        </div>
    );
}
