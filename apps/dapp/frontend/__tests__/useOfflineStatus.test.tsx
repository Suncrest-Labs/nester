import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useOfflineStatus } from "@/hooks/useOfflineStatus";

function setOnline(value: boolean) {
    Object.defineProperty(window.navigator, "onLine", {
        configurable: true,
        value,
    });
}

describe("useOfflineStatus", () => {
    beforeEach(() => {
        setOnline(true);
    });

    afterEach(() => {
        setOnline(true);
        vi.useRealTimers();
    });

    it("reflects navigator.onLine as false on mount when offline", () => {
        setOnline(false);
        const { result } = renderHook(() => useOfflineStatus());
        expect(result.current.isOffline).toBe(true);
    });

    it("reflects navigator.onLine as true on mount when online", () => {
        setOnline(true);
        const { result } = renderHook(() => useOfflineStatus());
        expect(result.current.isOffline).toBe(false);
        expect(result.current.lastSynced).not.toBeNull();
    });

    it("flips to offline when the window 'offline' event fires", () => {
        const { result } = renderHook(() => useOfflineStatus());
        expect(result.current.isOffline).toBe(false);

        act(() => {
            setOnline(false);
            window.dispatchEvent(new Event("offline"));
        });

        expect(result.current.isOffline).toBe(true);
    });

    it("flips back to online and updates lastSynced when the 'online' event fires", () => {
        setOnline(false);
        const { result } = renderHook(() => useOfflineStatus());
        expect(result.current.isOffline).toBe(true);

        act(() => {
            setOnline(true);
            window.dispatchEvent(new Event("online"));
        });

        expect(result.current.isOffline).toBe(false);
        expect(result.current.lastSynced).not.toBeNull();
    });

    it("removes its event listeners on unmount", () => {
        const addSpy = vi.spyOn(window, "addEventListener");
        const removeSpy = vi.spyOn(window, "removeEventListener");

        const { unmount } = renderHook(() => useOfflineStatus());
        expect(addSpy).toHaveBeenCalledWith("offline", expect.any(Function));
        expect(addSpy).toHaveBeenCalledWith("online", expect.any(Function));

        unmount();

        expect(removeSpy).toHaveBeenCalledWith("offline", expect.any(Function));
        expect(removeSpy).toHaveBeenCalledWith("online", expect.any(Function));
    });
});
