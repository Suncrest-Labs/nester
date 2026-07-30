import { describe, it, expect } from "vitest";
import { z } from "zod/v4";
import { validateAmount, validateBankAccount } from "@/lib/validation";
import { BANKS } from "@/lib/settlement-data";

const formSchema = z.object({
  amount: validateAmount({
    min: 1,
    balance: 5000,
    maxDecimals: 6,
    minMessage: "Minimum amount is 1 USDC",
    balanceMessage: "Amount exceeds your balance",
  }),
  accountNumber: validateBankAccount(),
  bankCode: z.string({ message: "Please select a bank" }).min(1, "Please select a bank"),
});

describe("Offramp form validation", () => {
  it("requires bank selection", () => {
    const result = formSchema.safeParse({
      amount: "100",
      accountNumber: "0123456789",
      bankCode: "",
    });
    expect(result.success).toBe(false);
  });

  it("validates account number format", () => {
    const result = formSchema.safeParse({
      amount: "100",
      accountNumber: "123",
      bankCode: BANKS[0].code,
    });
    expect(result.success).toBe(false);
  });

  it("guards submit when amount exceeds balance", () => {
    const result = formSchema.safeParse({
      amount: "99999",
      accountNumber: "0123456789",
      bankCode: BANKS[0].code,
    });
    expect(result.success).toBe(false);
  });

  it("accepts valid offramp form values", () => {
    const result = formSchema.safeParse({
      amount: "500",
      accountNumber: "0123456789",
      bankCode: BANKS[0].code,
    });
    expect(result.success).toBe(true);
  });
});
