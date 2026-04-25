"use client";

// components/offramp/BankCombobox.tsx
// Searchable bank dropdown. Fetches the bank list once on mount from
// GET /api/v1/banks?country=NG and filters client-side as the user types.

import { useEffect, useRef, useState } from "react";
import { ChevronDown, Loader2, Search } from "lucide-react";
import { cn } from "@/lib/utils";
import { fetchBankList, type Bank } from "@/lib/api/bank";

interface BankComboboxProps {
  value: string;           // selected bank code
  onChange: (code: string) => void;
  error?: string;
  country?: string;
  disabled?: boolean;
}

export function BankCombobox({
  value,
  onChange,
  error,
  country = "NG",
  disabled = false,
}: BankComboboxProps) {
  const [banks, setBanks] = useState<Bank[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState(false);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const containerRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  // Fetch bank list once on mount.
  useEffect(() => {
    let cancelled = false;
    fetchBankList(country)
      .then((list) => {
        if (!cancelled) {
          setBanks(list);
          setLoading(false);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setFetchError(true);
          setLoading(false);
        }
      });
    return () => { cancelled = true; };
  }, [country]);

  // Close on outside click.
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
        setQuery("");
      }
    }
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, []);

  // Focus search input when dropdown opens.
  useEffect(() => {
    if (open) setTimeout(() => searchRef.current?.focus(), 50);
  }, [open]);

  const selectedBank = banks.find((b) => b.code === value) ?? null;

  // Filter by name — case-insensitive, matches anywhere in the name.
  const filtered = query.trim()
    ? banks.filter((b) => b.name.toLowerCase().includes(query.toLowerCase()))
    : banks;

  return (
    <div ref={containerRef} className="relative">
      {/* Trigger button */}
      <button
        type="button"
        disabled={disabled || loading}
        onClick={() => setOpen((o) => !o)}
        className={cn(
          "w-full flex items-center justify-between px-4 py-3 rounded-xl border bg-white transition-colors min-h-[52px]",
          error
            ? "border-red-500"
            : open
            ? "border-foreground/30"
            : "border-border hover:border-foreground/20",
          (disabled || loading) && "opacity-60 cursor-not-allowed"
        )}
      >
        <span
          className={cn(
            "text-sm truncate",
            selectedBank ? "font-medium text-foreground" : "text-muted-foreground"
          )}
        >
          {loading
            ? "Loading banks…"
            : fetchError
            ? "Could not load banks"
            : selectedBank
            ? selectedBank.name
            : "Choose your bank"}
        </span>
        {loading ? (
          <Loader2 className="h-4 w-4 text-muted-foreground animate-spin shrink-0" />
        ) : (
          <ChevronDown
            className={cn(
              "h-4 w-4 text-muted-foreground shrink-0 transition-transform",
              open && "rotate-180"
            )}
          />
        )}
      </button>

      {/* Validation error */}
      {error && (
        <span className="text-xs text-red-500 font-medium mt-1 block">{error}</span>
      )}

      {/* Dropdown panel */}
      {open && !loading && (
        <div className="absolute left-0 right-0 top-full mt-1 rounded-xl border border-border bg-white shadow-lg z-30 flex flex-col max-h-64">
          {/* Search input */}
          <div className="px-3 py-2 border-b border-border flex items-center gap-2">
            <Search className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            <input
              ref={searchRef}
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search banks…"
              className="flex-1 text-sm bg-transparent outline-none placeholder:text-muted-foreground/60"
            />
          </div>

          {/* Bank list */}
          <div className="overflow-y-auto">
            {filtered.length === 0 ? (
              <p className="px-4 py-3 text-sm text-muted-foreground text-center">
                No banks match &ldquo;{query}&rdquo;
              </p>
            ) : (
              filtered.map((bank) => (
                <button
                  key={bank.code}
                  type="button"
                  onClick={() => {
                    onChange(bank.code);
                    setOpen(false);
                    setQuery("");
                  }}
                  className={cn(
                    "w-full text-left px-4 py-3 text-sm transition-colors min-h-[48px]",
                    bank.code === value
                      ? "bg-secondary font-medium text-foreground"
                      : "hover:bg-secondary/50 text-foreground"
                  )}
                >
                  {bank.name}
                </button>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}