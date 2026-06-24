import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import SavingsChart, { type SavingsChartPoint } from "@/components/analytics/SavingsChart";

const points: SavingsChartPoint[] = [
    { date: "Jun 15", apy: "8.23", value: 8.23 },
    { date: "Jun 16", apy: "9.10", value: 9.1 },
];

describe("SavingsChart", () => {
    it("shows a skeleton pulse while loading", () => {
        render(<SavingsChart data={[]} isLoading />);
        expect(screen.getByLabelText(/loading chart data/i)).toBeInTheDocument();
    });

    it("shows the descriptive empty state when there is no data", () => {
        render(<SavingsChart data={[]} />);
        expect(
            screen.getByText(/chart data will appear after your vault has been active/i)
        ).toBeInTheDocument();
    });

    it("renders the chart when data is present", () => {
        render(<SavingsChart data={points} />);
        expect(
            screen.getByRole("img", { name: /vault apy performance over time/i })
        ).toBeInTheDocument();
        expect(screen.queryByText(/chart data will appear/i)).not.toBeInTheDocument();
    });
});
