import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import React from "react";

import {
    NotificationsProvider,
    useNotifications,
} from "@/components/notifications-provider";
import { NotificationBell } from "@/components/notification-bell";
import NotificationsPage from "@/app/notifications/page";
import {
    coalesceNotifications,
    mapTypeToCategoryAndPriority,
    sanitizePreferences,
    fetchNotificationsApi,
    INITIAL_NOTIFICATIONS,
    type AppNotification,
} from "@/lib/notifications";

// Mock router & wallet
const mockPush = vi.fn();
vi.mock("next/navigation", () => ({
    useRouter: () => ({
        push: mockPush,
    }),
    usePathname: () => "/notifications",
}));

vi.mock("@/components/wallet-provider", () => ({
    useWallet: () => ({
        isConnected: true,
        address: "GXXXXXX",
    }),
}));

vi.mock("@/components/app-shell", () => ({
    AppShell: ({ children }: { children: React.ReactNode }) => <div data-testid="app-shell">{children}</div>,
}));

vi.mock("@/lib/notifications", async (importOriginal) => {
    const actual = await importOriginal<typeof import("@/lib/notifications")>();
    return {
        ...actual,
        fetchNotificationsApi: vi.fn().mockResolvedValue(actual.INITIAL_NOTIFICATIONS),
    };
});

function TestConsumer() {
    const {
        notifications,
        unreadCount,
        safetyCount,
        isDisconnected,
        isLoading,
        error,
        addNotification,
        markAsRead,
        markAllAsRead,
        dismissNotification,
        clearAll,
        updatePreference,
        preferences,
        setConnectionState,
    } = useNotifications();

    return (
        <div>
            <span data-testid="is-loading">{isLoading ? "loading" : "done"}</span>
            <span data-testid="error-state">{error || "none"}</span>
            <span data-testid="unread-count">{unreadCount}</span>
            <span data-testid="safety-count">{safetyCount}</span>
            <span data-testid="is-disconnected">{isDisconnected ? "yes" : "no"}</span>
            <button
                data-testid="add-normal"
                onClick={() =>
                    addNotification({
                        type: "deposit_confirmed",
                        title: "Deposit Success",
                        message: "Deposited 100 USDC",
                        actionUrl: "/vaults",
                    })
                }
            >
                Add Normal
            </button>
            <button
                data-testid="add-safety"
                onClick={() =>
                    addNotification({
                        type: "emergency_queue_fill",
                        category: "safety",
                        priority: "safety",
                        title: "Emergency Fill High",
                        message: "Queue filled to 90%",
                    })
                }
            >
                Add Safety
            </button>
            <button data-testid="mark-all-read" onClick={markAllAsRead}>
                Mark All Read
            </button>
            <button data-testid="clear-all" onClick={clearAll}>
                Clear All
            </button>
            <button
                data-testid="toggle-safety-email"
                onClick={() => updatePreference("safety", "email", false)}
            >
                Toggle Safety Email
            </button>
            <button
                data-testid="toggle-nudge-email"
                onClick={() => updatePreference("nudge", "email", true)}
            >
                Toggle Nudge Email
            </button>
            <button
                data-testid="set-disconnected"
                onClick={() => setConnectionState(false)}
            >
                Disconnect
            </button>
            <button
                data-testid="set-connected"
                onClick={() => setConnectionState(true)}
            >
                Reconnect
            </button>
            <div data-testid="notif-list">
                {notifications.map((n) => (
                    <div key={n.id} data-testid={`notif-item-${n.id}`}>
                        <span>{n.title}</span>
                        <span>{n.read ? "read" : "unread"}</span>
                        <span>{n.priority}</span>
                        <button onClick={() => markAsRead(n.id)}>Mark {n.id}</button>
                        <button onClick={() => dismissNotification(n.id)}>
                            Dismiss {n.id}
                        </button>
                    </div>
                ))}
            </div>
            <span data-testid="safety-email-pref">
                {preferences.categories.safety.email ? "enabled" : "disabled"}
            </span>
            <span data-testid="nudge-email-pref">
                {preferences.categories.nudge.email ? "enabled" : "disabled"}
            </span>
        </div>
    );
}

describe("Notification Center & Preferences Suite (#870)", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        window.localStorage.clear();
        vi.mocked(fetchNotificationsApi).mockResolvedValue(INITIAL_NOTIFICATIONS);
    });

    describe("1. Pure logic & Helper utilities", () => {
        it("maps notification types to correct category and priority", () => {
            expect(mapTypeToCategoryAndPriority("emergency_queue_fill")).toEqual({
                category: "safety",
                priority: "safety",
            });
            expect(mapTypeToCategoryAndPriority("security_event")).toEqual({
                category: "safety",
                priority: "safety",
            });
            expect(mapTypeToCategoryAndPriority("breaker_trip")).toEqual({
                category: "safety",
                priority: "safety",
            });
            expect(mapTypeToCategoryAndPriority("deposit_confirmed")).toEqual({
                category: "transactional",
                priority: "transactional",
            });
            expect(mapTypeToCategoryAndPriority("goal_milestone")).toEqual({
                category: "nudge",
                priority: "nudge",
            });
        });

        it("coalesces duplicate notifications within time window or matching coalesceKey", () => {
            const nowIso = new Date().toISOString();
            const items: AppNotification[] = [
                {
                    id: "n1",
                    type: "emergency_queue_fill",
                    category: "safety",
                    priority: "safety",
                    title: "Emergency Fill",
                    message: "First alert",
                    timestamp: nowIso,
                    read: false,
                    coalesceKey: "eq_1",
                    mergedIds: ["n1"],
                },
                {
                    id: "n2",
                    type: "emergency_queue_fill",
                    category: "safety",
                    priority: "safety",
                    title: "Emergency Fill",
                    message: "Second alert",
                    timestamp: nowIso,
                    read: false,
                    coalesceKey: "eq_1",
                    mergedIds: ["n2"],
                },
            ];

            const coalesced = coalesceNotifications(items);
            expect(coalesced).toHaveLength(1);
            expect(coalesced[0].count).toBe(2);
            expect(coalesced[0].mergedIds).toEqual(["n1", "n2"]);
        });

        it("sanitizes preferences ensuring safety category is always on", () => {
            const inputPrefs = {
                categories: {
                    safety: { in_app: false, email: false, push: false },
                    transactional: { in_app: true, email: false, push: false },
                    nudge: { in_app: false, email: false, push: false },
                },
            };

            const sanitized = sanitizePreferences(inputPrefs as any);
            expect(sanitized.categories.safety.in_app).toBe(true);
            expect(sanitized.categories.safety.email).toBe(true);
            expect(sanitized.categories.safety.push).toBe(true);
            expect(sanitized.categories.nudge.in_app).toBe(false);
        });
    });

    describe("2. NotificationsProvider State & Actions", () => {
        it("receives new notifications live and updates unread & safety counts", async () => {
            render(
                <NotificationsProvider>
                    <TestConsumer />
                </NotificationsProvider>
            );

            await waitFor(() => {
                expect(screen.getByTestId("is-loading").textContent).toBe("done");
            });

            const initialUnread = parseInt(screen.getByTestId("unread-count").textContent || "0");

            fireEvent.click(screen.getByTestId("add-normal"));

            await waitFor(() => {
                expect(parseInt(screen.getByTestId("unread-count").textContent || "0")).toBe(
                    initialUnread + 1
                );
            });

            fireEvent.click(screen.getByTestId("add-safety"));

            await waitFor(() => {
                expect(parseInt(screen.getByTestId("safety-count").textContent || "0")).toBeGreaterThan(0);
            });
        });

        it("syncs markAsRead and markAllAsRead", async () => {
            render(
                <NotificationsProvider>
                    <TestConsumer />
                </NotificationsProvider>
            );

            await waitFor(() => {
                expect(screen.getByTestId("is-loading").textContent).toBe("done");
            });

            fireEvent.click(screen.getByTestId("mark-all-read"));

            await waitFor(() => {
                expect(screen.getByTestId("unread-count").textContent).toBe("0");
            });
        });

        it("enforces safety preferences as always-on and prevents turning them off", async () => {
            render(
                <NotificationsProvider>
                    <TestConsumer />
                </NotificationsProvider>
            );

            await waitFor(() => {
                expect(screen.getByTestId("is-loading").textContent).toBe("done");
            });

            expect(screen.getByTestId("safety-email-pref").textContent).toBe("enabled");

            fireEvent.click(screen.getByTestId("toggle-safety-email"));

            await waitFor(() => {
                expect(screen.getByTestId("safety-email-pref").textContent).toBe("enabled");
            });

            fireEvent.click(screen.getByTestId("toggle-nudge-email"));

            await waitFor(() => {
                expect(screen.getByTestId("nudge-email-pref").textContent).toBe("enabled");
            });
        });

        it("handles REST fallback & reconciliation when connection status changes", async () => {
            render(
                <NotificationsProvider>
                    <TestConsumer />
                </NotificationsProvider>
            );

            await waitFor(() => {
                expect(screen.getByTestId("is-loading").textContent).toBe("done");
            });

            expect(screen.getByTestId("is-disconnected").textContent).toBe("no");

            const mockedFetch = vi.mocked(fetchNotificationsApi);
            mockedFetch.mockResolvedValueOnce([
                {
                    id: "reconciled-new-1",
                    type: "security_event",
                    category: "safety",
                    priority: "safety",
                    title: "Reconciled Security Event",
                    message: "Security breach checked on reconnect",
                    timestamp: new Date().toISOString(),
                    read: false,
                    mergedIds: ["reconciled-new-1"],
                },
            ]);

            fireEvent.click(screen.getByTestId("set-disconnected"));

            await waitFor(() => {
                expect(screen.getByTestId("is-disconnected").textContent).toBe("yes");
            });

            fireEvent.click(screen.getByTestId("set-connected"));

            await waitFor(() => {
                expect(screen.getByTestId("is-disconnected").textContent).toBe("no");
                expect(screen.getByTestId("notif-item-reconciled-new-1")).toBeDefined();
                expect(screen.getByText("Reconciled Security Event")).toBeDefined();
            });
        });

        it("surfaces error state when initial notification load fails", async () => {
            const mockedFetch = vi.mocked(fetchNotificationsApi);
            mockedFetch.mockRejectedValueOnce(new Error("Network connection error"));

            render(
                <NotificationsProvider>
                    <TestConsumer />
                </NotificationsProvider>
            );

            await waitFor(() => {
                expect(screen.getByTestId("error-state").textContent).toContain(
                    "Failed to load notifications stream"
                );
            });
        });

        it("renders empty state UI when notification load returns an empty list", async () => {
            const mockedFetch = vi.mocked(fetchNotificationsApi);
            mockedFetch.mockResolvedValueOnce([]);

            render(
                <NotificationsProvider>
                    <NotificationsPage />
                </NotificationsProvider>
            );

            await waitFor(() => {
                expect(screen.getByText("You are all caught up")).toBeDefined();
            });
        });
    });

    describe("3. NotificationBell Component", () => {
        it("renders bell icon and opens popover on click with keyboard support", async () => {
            render(
                <NotificationsProvider>
                    <NotificationBell />
                </NotificationsProvider>
            );

            const button = screen.getByRole("button", { name: /notifications/i });
            expect(button).toBeDefined();

            fireEvent.click(button);

            await waitFor(() => {
                expect(screen.getByRole("region", { name: /recent notifications/i })).toBeDefined();
            });

            fireEvent.keyDown(document, { key: "Escape" });

            await waitFor(() => {
                expect(screen.queryByRole("region", { name: /recent notifications/i })).toBeNull();
            });
        });
    });

    describe("4. NotificationsPage & Preferences UI", () => {
        it("renders stream, safety critical banner, and allows tab switching to preferences", async () => {
            render(
                <NotificationsProvider>
                    <NotificationsPage />
                </NotificationsProvider>
            );

            await waitFor(() => {
                expect(screen.getByText(/Notification Center/i)).toBeDefined();
            });

            expect(screen.getByRole("tab", { name: /Notifications Stream/i })).toBeDefined();

            const prefTab = screen.getByRole("tab", { name: /Preferences & Delivery/i });
            fireEvent.click(prefTab);

            await waitFor(() => {
                expect(screen.getByText(/Notification Channel Preferences/i)).toBeDefined();
                expect(screen.getByText(/Safety alerts cannot be suppressed/i)).toBeDefined();
            });
        });

        it("filters notifications by category in Stream tab", async () => {
            render(
                <NotificationsProvider>
                    <NotificationsPage />
                </NotificationsProvider>
            );

            await waitFor(() => {
                expect(screen.getByRole("group", { name: /Category filters/i })).toBeDefined();
            });

            const safetyBtn = screen.getByRole("button", { name: /Safety & Security/i });
            fireEvent.click(safetyBtn);

            await waitFor(() => {
                expect(safetyBtn.getAttribute("aria-pressed")).toBe("true");
                expect(screen.getByTestId("notif-item-seed-safety-1")).toBeDefined();
                expect(screen.queryByTestId("notif-item-seed-1")).toBeNull();
                expect(screen.queryByTestId("notif-item-seed-3")).toBeNull();
            });
        });
    });
});
