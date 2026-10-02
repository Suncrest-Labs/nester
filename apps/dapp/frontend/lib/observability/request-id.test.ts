import { describe, it, expect, beforeEach } from "vitest";
import {
  getLastRequestId,
  setLastRequestId,
  resetLastRequestId,
} from "@/lib/observability/request-id";

describe("request-id tracker", () => {
  beforeEach(() => {
    resetLastRequestId();
  });

  it("is undefined before any request-id has been seen", () => {
    expect(getLastRequestId()).toBeUndefined();
  });

  it("stores and returns the last set request id", () => {
    setLastRequestId("req-abc123");
    expect(getLastRequestId()).toBe("req-abc123");
  });

  it("overwrites the stored id on a subsequent call", () => {
    setLastRequestId("req-first");
    setLastRequestId("req-second");
    expect(getLastRequestId()).toBe("req-second");
  });

  it("ignores null (e.g. a response with no X-Request-ID header)", () => {
    setLastRequestId("req-kept");
    setLastRequestId(null);
    expect(getLastRequestId()).toBe("req-kept");
  });

  it("ignores undefined", () => {
    setLastRequestId("req-kept");
    setLastRequestId(undefined);
    expect(getLastRequestId()).toBe("req-kept");
  });

  it("ignores an empty string", () => {
    setLastRequestId("req-kept");
    setLastRequestId("");
    expect(getLastRequestId()).toBe("req-kept");
  });
});
