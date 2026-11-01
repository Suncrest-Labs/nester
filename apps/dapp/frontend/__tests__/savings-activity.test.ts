import { describe, it, expect, vi, afterEach } from "vitest";
import fixture from "./fixtures/horizon-savings-operations.json";
import {
  ACTIVITY_PAGE_SIZE,
  fetchSavingsActivity,
  parseSavingsOperations,
  type HorizonOperation,
} from "@/lib/stellar/activity";

const WALLET = "GDOCSKN3NOFTH3ZUFS4LZNUYNZT7I3BTSNVPK5QQIV6R4VZ34CBUUF3E";
const CORE = "CAWDB2TAO5MKNULROETJ3BRAT2DNTLI3S7BOHOVM2EDUQUY5FN6DUJ4Q";
const records = fixture._embedded.records as unknown as HorizonOperation[];

afterEach(() => vi.unstubAllGlobals());

describe("parseSavingsOperations", () => {
  it("keeps only the wallet's deposit and redeem calls into the savings core", () => {
    const items = parseSavingsOperations(records, WALLET, CORE);

    expect(items).toEqual([
      {
        id: "23628169873494017",
        kind: "withdraw",
        amount: "40",
        hash: records.find((r) => r.id === "23628169873494017")!.transaction_hash,
        createdAt: "2026-11-01T10:54:10Z",
      },
      {
        id: "23628144103698433",
        kind: "deposit",
        amount: "100",
        hash: records.find((r) => r.id === "23628144103698433")!.transaction_hash,
        createdAt: "2026-11-01T10:53:40Z",
      },
    ]);
  });

  it("ignores calls into a different contract", () => {
    expect(
      parseSavingsOperations(records, WALLET, "CCNFZP235C52SJUHX3JBMC3UJJATUSKJJESDBXK5OPQRZUWSXXUZIPU7")
    ).toEqual([]);
  });

  it("skips failed transactions", () => {
    const failed = records.map((r) => ({ ...r, transaction_successful: false }));
    expect(parseSavingsOperations(failed, WALLET, CORE)).toEqual([]);
  });

  it("only counts transfers that touch the wallet", () => {
    expect(parseSavingsOperations(records, "GOTHERWALLET", CORE)).toEqual([]);
  });
});

describe("fetchSavingsActivity", () => {
  it("requests the wallet's operations and returns no cursor on a short page", async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response(JSON.stringify(fixture))));
    vi.stubGlobal("fetch", fetchMock);

    const page = await fetchSavingsActivity(WALLET, CORE, "123");

    const url = new URL(String((fetchMock.mock.calls[0] as unknown[])[0]));
    expect(url.pathname).toBe(`/accounts/${WALLET}/operations`);
    expect(url.searchParams.get("order")).toBe("desc");
    expect(url.searchParams.get("cursor")).toBe("123");
    expect(url.searchParams.get("join")).toBe("transactions");
    expect(page.items).toHaveLength(2);
    expect(page.scanned).toBe(records.length);
    expect(page.nextCursor).toBeNull();
  });

  it("returns the last paging token when the page is full", async () => {
    const full = Array.from({ length: ACTIVITY_PAGE_SIZE }, (_, i) => ({
      ...records[0],
      id: String(i),
      paging_token: `pt-${i}`,
    }));
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response(JSON.stringify({ _embedded: { records: full } }))))
    );

    const page = await fetchSavingsActivity(WALLET, CORE);
    expect(page.nextCursor).toBe(`pt-${ACTIVITY_PAGE_SIZE - 1}`);
  });

  it("treats an unfunded account as empty and throws on other errors", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("{}", { status: 404 }))));
    await expect(fetchSavingsActivity(WALLET, CORE)).resolves.toEqual({
      items: [],
      scanned: 0,
      nextCursor: null,
    });

    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("{}", { status: 500 }))));
    await expect(fetchSavingsActivity(WALLET, CORE)).rejects.toThrow("Horizon returned 500");
  });
});
