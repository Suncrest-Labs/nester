import { z } from "zod";

export const StellarAddressSchema = z.string().regex(/^G[A-Z2-7]{55}$/, "Invalid Stellar address format");

export const AssetBalanceSchema = z.object({
  asset_type: z.string(),
  asset_code: z.string().optional(),
  asset_issuer: z.string().optional(),
  balance: z.string(),
});

export const ValidateBalanceRequestSchema = z.object({
  body: z.object({
    account_id: StellarAddressSchema,
    payments: z.array(
      z.object({
        asset_code: z.string().min(1),
        asset_issuer: z.string().optional(),
        amount: z.string().refine((val) => Number(val) > 0, "Amount must be positive"),
      }),
    ),
  }),
});

export type AssetBalance = z.infer<typeof AssetBalanceSchema>;

export type ValidateBalanceRequest = z.infer<typeof ValidateBalanceRequestSchema>;

export interface BalancesMap {
  [assetKey: string]: number;
}

export interface PaymentValidationResult {
  asset_key: string;
  required: number;
  available: number;
  sufficient: boolean;
}

export interface BalanceValidationResponse {
  account_id: string;
  all_sufficient: boolean;
  results: PaymentValidationResult[];
}
