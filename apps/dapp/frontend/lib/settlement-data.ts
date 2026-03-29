export const BANKS = [
    { name: "Kuda Bank", code: "kuda" },
    { name: "Moniepoint", code: "moniepoint" },
    { name: "Access Bank", code: "access" },
    { name: "GTBank", code: "gtbank" },
    { name: "First Bank", code: "firstbank" },
    { name: "UBA", code: "uba" },
    { name: "Zenith Bank", code: "zenith" },
    { name: "Opay", code: "opay" },
];

export interface LPNode {
    id: string;
    name: string;
    bank: string;
    baseRateOffset: number;
    fee: number;
    avgSettleTime: number;
    reliability: number;
}

export const LP_NODES: LPNode[] = [
    { id: "n1", name: "LiquidityPrime", bank: "kuda", baseRateOffset: 2.15, fee: 0.4, avgSettleTime: 8, reliability: 99.2 },
    { id: "n2", name: "FastSettle NG", bank: "zenith", baseRateOffset: 0.80, fee: 0.5, avgSettleTime: 12, reliability: 97.8 },
    { id: "n3", name: "NairaNode", bank: "gtbank", baseRateOffset: 1.45, fee: 0.45, avgSettleTime: 15, reliability: 98.5 },
    { id: "n4", name: "CedarPay", bank: "access", baseRateOffset: -0.30, fee: 0.35, avgSettleTime: 20, reliability: 96.1 },
    { id: "n5", name: "StellarBridge", bank: "moniepoint", baseRateOffset: 1.90, fee: 0.5, avgSettleTime: 5, reliability: 99.5 },
    { id: "n6", name: "KudaConnect", bank: "kuda", baseRateOffset: 0.55, fee: 0.3, avgSettleTime: 3, reliability: 98.9 },
    { id: "n7", name: "PayRoute", bank: "firstbank", baseRateOffset: -0.90, fee: 0.6, avgSettleTime: 25, reliability: 94.2 },
    { id: "n8", name: "SwiftNode", bank: "opay", baseRateOffset: 1.10, fee: 0.45, avgSettleTime: 10, reliability: 97.0 },
];
