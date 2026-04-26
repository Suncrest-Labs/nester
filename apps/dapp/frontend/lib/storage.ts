/**
 * Hardened localStorage wrapper.
 *
 * Goals:
 *  - Never throw when localStorage is unavailable (private browsing, full
 *    quota, server-side rendering). Failures fall back to an in-memory map
 *    so callers can keep working in the same tab.
 *  - Surface storage events for cross-tab synchronization.
 *  - Warn (once) when storage usage approaches the browser's ~5MB quota.
 *  - Recover from corrupted JSON by removing the key rather than crashing
 *    the whole provider that reads it.
 *
 * The default export `safeStorage` is a singleton; callers shouldn't
 * instantiate their own.
 */

type Listener<T> = (value: T | null) => void;

const memoryStore: Map<string, string> = new Map();
const listeners: Map<string, Set<Listener<unknown>>> = new Map();

let quotaWarningEmitted = false;
const STORAGE_QUOTA_WARN_RATIO = 0.8;
// Browsers typically allocate ~5MB; we approximate so we can warn early.
const APPROX_LOCALSTORAGE_QUOTA_BYTES = 5 * 1024 * 1024;

function isBrowser() {
    return typeof window !== "undefined";
}

function readNative(key: string): string | null {
    if (!isBrowser()) return memoryStore.get(key) ?? null;
    try {
        return window.localStorage.getItem(key);
    } catch {
        return memoryStore.get(key) ?? null;
    }
}

function writeNative(key: string, value: string): boolean {
    if (!isBrowser()) {
        memoryStore.set(key, value);
        return true;
    }
    try {
        window.localStorage.setItem(key, value);
        memoryStore.delete(key);
        return true;
    } catch {
        memoryStore.set(key, value);
        return false;
    }
}

function removeNative(key: string) {
    memoryStore.delete(key);
    if (!isBrowser()) return;
    try {
        window.localStorage.removeItem(key);
    } catch {
        // ignore — storage may be disabled or full
    }
}

/**
 * Coarse usage estimate. We sum the lengths of every key+value pair as a
 * proxy for byte usage; UTF-16 strings are roughly 2 bytes/char, so we
 * multiply at the end. This is intentionally conservative — under-reporting
 * usage is worse than over-reporting because we'd miss a quota warning.
 */
function estimateUsageBytes(): number {
    if (!isBrowser()) return 0;
    try {
        let total = 0;
        for (let i = 0; i < window.localStorage.length; i++) {
            const key = window.localStorage.key(i);
            if (!key) continue;
            const value = window.localStorage.getItem(key) ?? "";
            total += (key.length + value.length) * 2;
        }
        return total;
    } catch {
        return 0;
    }
}

function maybeWarnAboutQuota() {
    if (quotaWarningEmitted) return;
    const usage = estimateUsageBytes();
    if (usage / APPROX_LOCALSTORAGE_QUOTA_BYTES >= STORAGE_QUOTA_WARN_RATIO) {
        quotaWarningEmitted = true;
        // Single console.warn — we don't want to spam every write.
        if (typeof console !== "undefined") {
            // eslint-disable-next-line no-console
            console.warn(
                `[nester] localStorage usage at ~${Math.round(
                    (usage / APPROX_LOCALSTORAGE_QUOTA_BYTES) * 100
                )}% of approximate quota — older notifications will be pruned aggressively.`
            );
        }
    }
}

export const safeStorage = {
    /** Returns the parsed JSON value at `key`, or `fallback` on miss/parse error. */
    get<T>(key: string, fallback: T): T {
        const raw = readNative(key);
        if (raw === null || raw === undefined) return fallback;
        try {
            return JSON.parse(raw) as T;
        } catch {
            // Corrupted data — wipe it so we don't keep failing.
            removeNative(key);
            return fallback;
        }
    },

    /** Returns the raw string at `key`, or `null`. */
    getRaw(key: string): string | null {
        return readNative(key);
    },

    /**
     * Writes `value` as JSON. Returns true on durable write to localStorage,
     * false when we fell back to in-memory (i.e. private mode, quota hit).
     */
    set<T>(key: string, value: T): boolean {
        let raw: string;
        try {
            raw = JSON.stringify(value);
        } catch {
            return false;
        }
        const ok = writeNative(key, raw);
        if (ok) maybeWarnAboutQuota();
        return ok;
    },

    /** Removes the key from both backing stores. */
    remove(key: string) {
        removeNative(key);
    },

    /**
     * Subscribe to cross-tab updates for a single key. The callback receives
     * the parsed value when another tab writes, or `null` on remove. Returns
     * an unsubscribe function.
     *
     * The native `storage` event only fires in *other* tabs; same-tab updates
     * still need to be applied via React state. That's intentional.
     */
    subscribe<T>(key: string, callback: Listener<T>): () => void {
        if (!isBrowser()) return () => {};

        let bucket = listeners.get(key) as Set<Listener<unknown>> | undefined;
        if (!bucket) {
            bucket = new Set<Listener<unknown>>();
            listeners.set(key, bucket);
        }
        bucket.add(callback as Listener<unknown>);

        const handler = (event: StorageEvent) => {
            if (event.key !== key) return;
            // Same key changed in another tab — try parsing and dispatch.
            const peers = listeners.get(key);
            if (!peers) return;
            let parsed: T | null = null;
            if (event.newValue !== null) {
                try {
                    parsed = JSON.parse(event.newValue) as T;
                } catch {
                    parsed = null;
                }
            }
            for (const cb of peers) {
                try {
                    cb(parsed);
                } catch {
                    // A single subscriber blowing up shouldn't stop the others.
                }
            }
        };

        window.addEventListener("storage", handler);

        return () => {
            window.removeEventListener("storage", handler);
            const peers = listeners.get(key);
            if (peers) {
                peers.delete(callback as Listener<unknown>);
                if (peers.size === 0) listeners.delete(key);
            }
        };
    },
};

export type SafeStorage = typeof safeStorage;
