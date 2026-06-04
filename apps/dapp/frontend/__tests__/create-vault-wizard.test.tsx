import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { CreateVaultWizard } from "@/components/vault/CreateVaultWizard";

vi.mock("@/lib/stellar/vault-factory", () => ({
  VaultFactory: {
    deployVault: vi.fn().mockResolvedValue({ success: true, vaultId: "test-vault" }),
  },
}));

describe("CreateVaultWizard", () => {
  it("renders step 1 with vault basics heading", () => {
    render(<CreateVaultWizard />);
    expect(screen.getByRole("heading", { name: /vault basics/i })).toBeInTheDocument();
  });

  it("requires vault name before proceeding", () => {
    render(<CreateVaultWizard />);
    const nextBtn = screen.getByRole("button", { name: /next/i });
    expect(nextBtn).toBeDisabled();
  });

  it("allows selecting a vault type", () => {
    render(<CreateVaultWizard />);
    fireEvent.click(screen.getByText(/stable yield/i));
    expect(screen.getByText(/stable yield/i)).toBeInTheDocument();
  });
});
