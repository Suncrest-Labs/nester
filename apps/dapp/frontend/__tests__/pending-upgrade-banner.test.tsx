import React from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  PendingUpgradeBanner,
  formatTimelockCountdown,
} from "../components/pending-upgrade-banner";

describe("PendingUpgradeBanner (#1549)", () => {
  it("renders nothing when there are no pending upgrades", () => {
    const { container } = render(<PendingUpgradeBanner initialUpgrades={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it("displays the banner during a rehearsed proposal with ETA and WASM hash link", () => {
    const wasmHash =
      "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855";
    render(
      <PendingUpgradeBanner
        initialUpgrades={[
          {
            contract_address:
              "CCWAMN2476B3R236R3T7G7M3MMK23R2J4S3I6W3M2A4J6S3I6W3M2A4J",
            wasm_hash: wasmHash,
            eta: 1792483200,
            eta_iso: "2026-10-20T08:00:00Z",
            remaining_seconds: 172800,
            status: "pending",
            release_url: `https://github.com/Suncrest-Labs/nester/releases?q=${wasmHash}`,
          },
        ]}
      />,
    );

    expect(screen.getByTestId("pending-upgrade-banner")).toBeDefined();
    expect(screen.getByTestId("upgrade-eta").textContent).toBe(
      "2026-10-20T08:00:00Z",
    );
    expect(screen.getByTestId("upgrade-countdown").textContent).toBe(
      "2d 0h remaining",
    );
    expect(screen.getByTestId("upgrade-wasm-hash").textContent).toBe(
      "e3b0c442...7852b855",
    );
    expect(
      screen.getByTestId("upgrade-release-link").getAttribute("href"),
    ).toBe(`https://github.com/Suncrest-Labs/nester/releases?q=${wasmHash}`);
  });

  it("formats timelock countdown across days, hours, and matured states", () => {
    expect(formatTimelockCountdown(172800)).toBe("2d 0h remaining");
    expect(formatTimelockCountdown(7500)).toBe("2h 5m remaining");
    expect(formatTimelockCountdown(0)).toBe("Ready for execution");
  });
});
