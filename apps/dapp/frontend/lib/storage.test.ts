import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { safeStorage } from "./storage";

// Issue #1233: safeStorage must never throw when the underlying accessor
// does, and set/get/remove must round-trip correctly through both the
// native store and the in-memory fallback.

describe("safeStorage", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("round-trips a JSON value through set/get", () => {
    expect(safeStorage.set("k1", { a: 1 })).toBe(true);
    expect(safeStorage.get("k1", null)).toEqual({ a: 1 });
  });

  it("returns the fallback for a missing key", () => {
    expect(safeStorage.get("missing", "fallback")).toBe("fallback");
  });

  it("wipes and falls back on corrupted (non-JSON) data rather than throwing", () => {
    window.localStorage.setItem("corrupt", "{not valid json");
    expect(safeStorage.get("corrupt", "fallback")).toBe("fallback");
    expect(window.localStorage.getItem("corrupt")).toBeNull();
  });

  it("falls back to the in-memory map when localStorage.getItem throws", () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockImplementation(() => {
      throw new Error("SecurityError: storage disabled");
    });
    // set() itself uses the mocked getItem indirectly via writeNative's
    // read-then-write path in some browsers, but the direct contract under
    // test is: a throwing getItem must not propagate out of get().
    expect(() => safeStorage.get("any-key", "fallback")).not.toThrow();
    expect(safeStorage.get("any-key", "fallback")).toBe("fallback");
  });

  it("falls back to the in-memory map when localStorage.setItem throws (e.g. quota exceeded)", () => {
    vi.spyOn(window.localStorage.__proto__, "setItem").mockImplementation(() => {
      throw new DOMException("QuotaExceededError");
    });
    const ok = safeStorage.set("quota-key", "value");
    expect(ok).toBe(false); // false signals "fell back to in-memory"
    expect(safeStorage.get("quota-key", null)).toBe("value"); // still readable
  });

  it("remove clears both the native store and any in-memory fallback", () => {
    safeStorage.set("to-remove", "x");
    safeStorage.remove("to-remove");
    expect(safeStorage.get("to-remove", null)).toBeNull();
  });

  describe("removeByPrefix", () => {
    it("removes only keys matching the prefix", () => {
      safeStorage.set("cache:a:1", "x");
      safeStorage.set("cache:a:2", "y");
      safeStorage.set("cache:b:1", "z");
      safeStorage.set("unrelated", "w");

      safeStorage.removeByPrefix("cache:a:");

      expect(safeStorage.get("cache:a:1", null)).toBeNull();
      expect(safeStorage.get("cache:a:2", null)).toBeNull();
      expect(safeStorage.get("cache:b:1", null)).toBe("z");
      expect(safeStorage.get("unrelated", null)).toBe("w");
    });

    it("does not throw when localStorage enumeration itself is unavailable", () => {
      vi.spyOn(window.localStorage.__proto__, "length", "get").mockImplementation(() => {
        throw new Error("SecurityError");
      });
      expect(() => safeStorage.removeByPrefix("cache:")).not.toThrow();
    });

    it("is a no-op (not an error) when nothing matches the prefix", () => {
      safeStorage.set("other", "x");
      expect(() => safeStorage.removeByPrefix("nomatch:")).not.toThrow();
      expect(safeStorage.get("other", null)).toBe("x");
    });
  });

  describe("getRaw", () => {
    it("returns the raw, un-parsed string", () => {
      window.localStorage.setItem("raw-key", "plain-string-value");
      expect(safeStorage.getRaw("raw-key")).toBe("plain-string-value");
    });

    it("returns null for a missing key", () => {
      expect(safeStorage.getRaw("missing")).toBeNull();
    });
  });
});
