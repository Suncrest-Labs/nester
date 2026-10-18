import React from "react";
import { describe, it, expect } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { NetworkProvider, useNetwork } from "@/context/NetworkProvider";
import { NETWORKS } from "@/lib/networks";
import {
    DeployedContractsSection,
    VERIFY_DOC_URL,
    buildStellarExpertContractUrl,
} from "@/components/network/DeployedContractsSection";

function NetworkHarness() {
    const { setNetwork } = useNetwork();
    return (
        <div>
            <button
                type="button"
                data-testid="switch-to-mainnet"
                onClick={() => setNetwork("mainnet")}
            >
                Switch to Mainnet
            </button>
            <button
                type="button"
                data-testid="switch-to-testnet"
                onClick={() => setNetwork("testnet")}
            >
                Switch to Testnet
            </button>
            <DeployedContractsSection />
        </div>
    );
}

describe("DeployedContractsSection", () => {
    it("renders Testnet contract addresses, stellar.expert testnet links, WASM hashes, and VERIFY.md link", () => {
        render(
            <NetworkProvider>
                <NetworkHarness />
            </NetworkProvider>,
        );

        expect(screen.getByTestId("deployed-contracts-section").getAttribute("data-network")).toBe(
            "testnet",
        );
        expect(screen.getByTestId("contract-id-vault").textContent).toBe(
            NETWORKS.testnet.contracts.vault,
        );
        expect(
            screen.getByTestId("contract-explorer-link-vault").getAttribute("href"),
        ).toBe(
            buildStellarExpertContractUrl("testnet", NETWORKS.testnet.contracts.vault),
        );
        expect(screen.getByTestId("verify-doc-link").getAttribute("href")).toBe(
            VERIFY_DOC_URL,
        );
        expect(screen.getByTestId("contract-wasm-vault").textContent).toContain("sha256:");
    });

    it("updates contract addresses and stellar.expert public links when switching to Mainnet", async () => {
        render(
            <NetworkProvider>
                <NetworkHarness />
            </NetworkProvider>,
        );

        fireEvent.click(screen.getByTestId("switch-to-mainnet"));

        await waitFor(() => {
            expect(
                screen.getByTestId("deployed-contracts-section").getAttribute("data-network"),
            ).toBe("mainnet");
        });
        expect(screen.getByTestId("contract-id-vault").textContent).toBe(
            NETWORKS.mainnet.contracts.vault,
        );
        expect(
            screen.getByTestId("contract-explorer-link-vault").getAttribute("href"),
        ).toBe(
            buildStellarExpertContractUrl("mainnet", NETWORKS.mainnet.contracts.vault),
        );
    });
});
