"use client";

// components/offramp/SuggestedBankChips.tsx
// Quick-pick chips shown when the user has entered a 10-digit account number.
// Tapping a chip selects that bank so the resolver can fire immediately.

import { motion } from "framer-motion";
import { cn } from "@/lib/utils";

export interface SuggestedBank {
  name: string;
  code: string;
  shortName: string;
}

export const POPULAR_NG_BANKS: SuggestedBank[] = [
  { name: "Guaranty Trust Bank", code: "058",    shortName: "GTBank"     },
  { name: "Access Bank",          code: "044",    shortName: "Access"     },
  { name: "Zenith Bank",          code: "057",    shortName: "Zenith"     },
  { name: "United Bank for Africa", code: "033",  shortName: "UBA"        },
  { name: "First Bank of Nigeria", code: "011",   shortName: "First Bank" },
  { name: "Kuda Bank",            code: "090267", shortName: "Kuda"       },
  { name: "Moniepoint MFB",       code: "50515",  shortName: "Moniepoint" },
  { name: "OPay",                 code: "999992", shortName: "OPay"       },
];

interface SuggestedBankChipsProps {
  selectedCode: string;
  onSelect: (code: string) => void;
}

export function SuggestedBankChips({ selectedCode, onSelect }: SuggestedBankChipsProps) {
  return (
    <motion.div
      initial={{ opacity: 0, y: -6 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -4 }}
      transition={{ duration: 0.2 }}
      className="space-y-1.5"
    >
      <p className="text-[11px] text-muted-foreground">Quick pick — select your bank:</p>
      <div className="flex flex-wrap gap-2">
        {POPULAR_NG_BANKS.map((bank, i) => {
          const isSelected = bank.code === selectedCode;
          return (
            <motion.button
              key={bank.code}
              type="button"
              initial={{ opacity: 0, scale: 0.9 }}
              animate={{ opacity: 1, scale: 1 }}
              transition={{ duration: 0.15, delay: i * 0.03 }}
              onClick={() => onSelect(bank.code)}
              className={cn(
                "px-3 py-1.5 rounded-full text-xs font-medium border transition-all",
                isSelected
                  ? "bg-foreground text-background border-foreground"
                  : "bg-white dark:bg-[#100F0F] text-foreground border-border hover:border-foreground/30 hover:bg-secondary/50"
              )}
            >
              {bank.shortName}
            </motion.button>
          );
        })}
      </div>
    </motion.div>
  );
}
