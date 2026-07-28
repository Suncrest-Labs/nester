"""AI-powered rebalancing suggestion engine.

Analyzes a vault's current allocation against live protocol APY data and the
risk model in `app.services.risk_model`, then asks Claude for a short,
human-readable rationale. Falls back to a deterministic rationale if Claude
is unavailable so the endpoint never hard-fails on a model outage.
"""

import json
import logging
from typing import Any

import anthropic
from stellar_sdk import Account, Network, StrKey, TransactionBuilder

from app.config import settings
from app.models.rebalance import (
    ProtocolAllocation,
    RebalanceAction,
    RebalanceExecuteRequest,
    RebalanceExecuteResponse,
    RebalanceSuggestRequest,
    RebalanceSuggestResponse,
    Urgency,
)
from app.services import risk_model
from app.services.defillama import get_client as get_defillama_client
from app.services.prometheus import ANALYZE_MAX_TOKENS, SYSTEM_PROMPT, get_client

logger = logging.getLogger(__name__)

# Manage-data values are capped at 64 bytes by the Stellar protocol.
_MANAGE_DATA_VALUE_MAX_BYTES = 64


async def _live_apy_by_protocol() -> dict[str, float]:
    """Best-effort live APY per protocol from DeFiLlama, keyed by lowercase name."""
    try:
        pools = await get_defillama_client().get_yield_pools("Stellar")
    except Exception:
        logger.exception("rebalance_engine: failed to fetch live APY data")
        return {}

    live: dict[str, float] = {}
    for pool in pools:
        project = str(pool.get("project", "")).strip().lower()
        apy = pool.get("apy")
        if project and isinstance(apy, (int, float)) and apy > 0:
            # Keep the highest observed APY per project across its pools.
            live[project] = max(live.get(project, 0.0), float(apy))
    return live


def _urgency_for_gain(gain_pct: float) -> Urgency:
    if gain_pct >= 10:
        return "high"
    if gain_pct >= 5:
        return "medium"
    return "low"


def _build_actions(
    allocations: list[ProtocolAllocation], optimal_protocol: str, optimal_apy: float
) -> list[RebalanceAction]:
    actions: list[RebalanceAction] = []
    optimal_lower = optimal_protocol.strip().lower()
    already_allocated = {a.protocol.strip().lower() for a in allocations}

    for alloc in allocations:
        is_optimal = alloc.protocol.strip().lower() == optimal_lower
        to_pct = 100.0 if is_optimal else 0.0
        if to_pct == alloc.percentage:
            action: Any = "hold"
        elif to_pct > alloc.percentage:
            action = "increase"
        else:
            action = "reduce"
        reason = (
            f"{alloc.protocol} is the highest risk-adjusted yield at {optimal_apy:.2f}% APY"
            if is_optimal
            else f"{alloc.protocol}'s risk-adjusted return trails {optimal_protocol}"
        )
        actions.append(
            RebalanceAction.model_validate(
                {
                    "protocol": alloc.protocol,
                    "action": action,
                    "from": alloc.percentage,
                    "to": to_pct,
                    "reason": reason,
                }
            )
        )

    if optimal_lower not in already_allocated:
        actions.append(
            RebalanceAction.model_validate(
                {
                    "protocol": optimal_protocol,
                    "action": "increase",
                    "from": 0.0,
                    "to": 100.0,
                    "reason": (
                        f"{optimal_protocol} offers the best risk-adjusted yield at "
                        f"{optimal_apy:.2f}% APY"
                    ),
                }
            )
        )

    return actions


async def _generate_rationale(
    current_apy: float, optimal_apy: float, optimal_protocol: str, actions: list[RebalanceAction]
) -> str:
    action_summary = ", ".join(f"{a.protocol}: {a.action} to {a.to_pct:.0f}%" for a in actions)
    prompt = (
        "You are Prometheus, Nester's yield-rebalancing analyst. "
        f"Current weighted APY is {current_apy:.2f}%, the optimal risk-adjusted allocation "
        f"yields {optimal_apy:.2f}% by favouring {optimal_protocol}. "
        f"Planned changes: {action_summary}. "
        "In 1-2 sentences, explain why this rebalance is worth doing (or not) for a "
        "Nester user in Nigeria/Ghana/Kenya. Plain language, no jargon, no em dashes."
    )
    try:
        client = get_client()
        response = await client.messages.create(
            model=settings.anthropic_model,
            max_tokens=ANALYZE_MAX_TOKENS,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": prompt}],
        )
        text = next(
            (b.text for b in response.content if isinstance(b, anthropic.types.TextBlock)), ""
        )
        return text.strip() or _fallback_rationale(current_apy, optimal_apy, optimal_protocol)
    except Exception:
        logger.exception("rebalance_engine: rationale generation failed")
        return _fallback_rationale(current_apy, optimal_apy, optimal_protocol)


def _fallback_rationale(current_apy: float, optimal_apy: float, optimal_protocol: str) -> str:
    gain = optimal_apy - current_apy
    return (
        f"Shifting toward {optimal_protocol} improves your weighted APY from "
        f"{current_apy:.2f}% to {optimal_apy:.2f}%, a gain of {gain:.2f} percentage points."
    )


class RebalanceEngine:
    """Analyzes vault allocations and produces AI rebalancing suggestions."""

    async def analyze(self, request: RebalanceSuggestRequest) -> RebalanceSuggestResponse:
        live_apy = await _live_apy_by_protocol()
        data_is_live = bool(live_apy)

        current_weighted_apy = sum(
            (a.percentage / 100.0) * a.apy for a in request.allocations
        )

        candidates = {a.protocol.strip().lower(): a.apy for a in request.allocations}
        for protocol in risk_model.PROTOCOL_RISK_FACTORS:
            candidates.setdefault(
                protocol, live_apy.get(protocol, risk_model.BASELINE_APY.get(protocol, 0.0))
            )
        for protocol, apy in live_apy.items():
            candidates[protocol] = apy

        scored = {
            protocol: risk_model.score_protocol(protocol, apy)
            for protocol, apy in candidates.items()
            if apy > 0
        }
        if not scored:
            optimal_protocol = request.allocations[0].protocol if request.allocations else "blend"
            optimal_apy = current_weighted_apy
        else:
            optimal_protocol = max(scored, key=lambda p: scored[p])
            optimal_apy = candidates[optimal_protocol]

        gain_pct = optimal_apy - current_weighted_apy
        should_rebalance = gain_pct > request.threshold_pct

        actions = (
            _build_actions(request.allocations, optimal_protocol, optimal_apy)
            if should_rebalance
            else []
        )
        rationale = (
            await _generate_rationale(
                current_weighted_apy, optimal_apy, optimal_protocol, actions
            )
            if should_rebalance
            else (
                "Your current allocation is already close to the optimal risk-adjusted "
                "yield. No change recommended."
            )
        )

        confidence = self._confidence(data_is_live, gain_pct)

        return RebalanceSuggestResponse(
            vault_id=request.vault_id,
            should_rebalance=should_rebalance,
            urgency=_urgency_for_gain(max(gain_pct, 0)),
            current_weighted_apy=round(current_weighted_apy, 4),
            optimal_weighted_apy=round(optimal_apy, 4),
            yield_improvement_usd=round(
                request.vault_balance_usd * max(gain_pct, 0) / 100.0, 2
            ),
            actions=actions,
            rationale=rationale,
            confidence=confidence,
        )

    @staticmethod
    def _confidence(data_is_live: bool, gain_pct: float) -> float:
        base = 0.85 if data_is_live else 0.45
        # Larger gains are easier to be confident about than marginal ones.
        certainty_bonus = min(abs(gain_pct) / 100.0, 0.1)
        return round(min(base + certainty_bonus, 0.99), 2)

    def build_unsigned_transaction(
        self, request: RebalanceExecuteRequest
    ) -> RebalanceExecuteResponse:
        """Build an unsigned Stellar transaction encoding the approved rebalance actions.

        Each action is recorded as a `manage_data` operation so the resulting
        transaction is inspectable on-chain audit tools before the user signs
        it. The source account's sequence number is not known to this
        service, so it is set to 0; the caller (wallet or relaying API) must
        refresh it from Horizon immediately before requesting a signature.
        """
        if not StrKey.is_valid_ed25519_public_key(request.source_account):
            raise ValueError("source_account must be a valid Stellar public key")

        network_passphrase = (
            Network.PUBLIC_NETWORK_PASSPHRASE
            if request.network == "public"
            else Network.TESTNET_NETWORK_PASSPHRASE
        )

        account = Account(account=request.source_account, sequence=0)
        builder = TransactionBuilder(
            source_account=account,
            network_passphrase=network_passphrase,
            base_fee=100,
        )

        for action in request.actions:
            data_name = f"rebalance_{action.protocol}"[:64]
            data_value = json.dumps({"to": action.to_pct, "action": action.action})[
                :_MANAGE_DATA_VALUE_MAX_BYTES
            ]
            builder.append_manage_data_op(data_name=data_name, data_value=data_value)

        builder.set_timeout(300)
        transaction = builder.build()

        return RebalanceExecuteResponse(
            vault_id=request.vault_id,
            unsigned_transaction_xdr=transaction.to_xdr(),
            network_passphrase=network_passphrase,
            source_account=request.source_account,
        )


rebalance_engine = RebalanceEngine()
