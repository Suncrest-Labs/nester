"""Structured retrieval layer for Prometheus AI grounding (#852).

Splits retrieval by data type rather than using embeddings for structured data:

- Positions, goals, transactions -> structured, user-scoped API queries
- Yield landscape -> vault/market service calls

A lightweight query router decides which sources to pull so only the minimal
context needed to answer is retrieved. Every fetch is scoped to the
authenticated user_id passed by the caller (sourced from the JWT subject) — the
query text never selects the user, so a prompt-injected "show me another user's
data" cannot widen scope.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from enum import Enum
from typing import Any, Protocol


class Intent(str, Enum):
    """A data need inferred from the user's message."""

    POSITIONS = "positions"
    YIELD_LANDSCAPE = "yield_landscape"
    GOALS = "goals"
    TRANSACTIONS = "transactions"
    GENERAL = "general"


# Keyword rules per intent. Structured routing — deterministic and auditable,
# unlike an embedding classifier whose decisions cannot be explained.
_INTENT_KEYWORDS: dict[Intent, tuple[str, ...]] = {
    Intent.GOALS: (
        "goal", "goals", "target", "save for", "saving for", "deadline",
        "car", "house", "home", "vacation", "wedding", "emergency fund", "tuition",
    ),
    Intent.YIELD_LANDSCAPE: (
        "best vault", "which vault", "recommend", "recommendation", "highest apy",
        "best apy", "best yield", "opportunit", "where should i", "compare vault",
        "top vault", "move my money", "switch vault",
    ),
    Intent.TRANSACTIONS: (
        "deposit", "deposited", "withdraw", "withdrawal", "transaction",
        "last", "recent", "history", "activity", "transfer",
    ),
    Intent.POSITIONS: (
        "my vault", "my vaults", "my portfolio", "my balance", "my position",
        "how much", "holdings", "net worth", "total value", "my yield", "my apy",
    ),
}


def route_query(message: str) -> set[Intent]:
    """Return the set of data intents a message needs.

    Matching is on the message text only. When nothing matches, POSITIONS is
    included as the safe default (most questions are about the user's own money)
    plus GENERAL so the caller always has the user's baseline context.
    """
    text = message.lower()
    intents: set[Intent] = set()
    for intent, keywords in _INTENT_KEYWORDS.items():
        if any(kw in text for kw in keywords):
            intents.add(intent)
    if not intents:
        intents.add(Intent.POSITIONS)
    intents.add(Intent.GENERAL)
    return intents


# Match currency/number tokens like "1,234.56", "$45k", "12.5%", "0.08".
_NUMBER_RE = re.compile(r"\d[\d,]*\.?\d*")


def normalize_number(token: str) -> str:
    """Normalize a numeric token for comparison: strip thousands separators and
    trailing zeros so "1,200.00", "1200", and "1200.0" compare equal.
    """
    cleaned = token.replace(",", "").strip()
    try:
        d = float(cleaned)
    except ValueError:
        return cleaned
    if d == int(d):
        return str(int(d))
    return ("%f" % d).rstrip("0").rstrip(".")


def extract_numbers(text: str) -> set[str]:
    """Extract the set of normalized numbers appearing in text."""
    return {normalize_number(m) for m in _NUMBER_RE.findall(text)}


@dataclass
class Citation:
    """A provenance pointer for a retrieved fact, surfaced to the user."""

    source: str
    detail: str


@dataclass
class RetrievedContext:
    """The minimal, user-scoped context assembled for one query."""

    sections: list[str] = field(default_factory=list)
    facts: set[str] = field(default_factory=set)
    citations: list[Citation] = field(default_factory=list)

    @property
    def has_data(self) -> bool:
        return bool(self.sections)

    def _record_numbers(self, *values: Any) -> None:
        for v in values:
            self.facts.update(extract_numbers(str(v)))

    def to_prompt_block(self) -> str:
        """Render the retrieved context for injection into the system prompt."""
        if not self.sections:
            return "## Retrieved Context\n(No user data was retrieved for this query.)"
        cite_lines = [f"- {c.source}: {c.detail}" for c in self.citations]
        cites = "\n".join(cite_lines) if cite_lines else "- (none)"
        return (
            "## Retrieved Context (user-scoped; the ONLY source of facts)\n"
            + "\n\n".join(self.sections)
            + "\n\n## Sources\n"
            + cites
        )


class DataSource(Protocol):
    """User-scoped data access for retrieval. All user methods take the
    authenticated user_id; implementations must never widen scope beyond it."""

    async def user_vaults(self, user_id: str) -> list[dict[str, Any]]: ...
    async def savings_goals(self, user_id: str) -> list[dict[str, Any]]: ...
    async def recent_transactions(self, user_id: str) -> list[dict[str, Any]]: ...
    async def available_vaults(self) -> list[dict[str, Any]]: ...
    async def market_rates(self) -> list[dict[str, Any]]: ...


class RetrievalService:
    """Assembles minimal, grounded, user-scoped context per query."""

    def __init__(self, source: DataSource, max_transactions: int = 5) -> None:
        self._source = source
        self._max_transactions = max_transactions

    async def retrieve(self, user_id: str, message: str) -> RetrievedContext:
        """Retrieve only the context the routed intents require, always scoped to
        user_id regardless of message content."""
        intents = route_query(message)
        ctx = RetrievedContext()

        if Intent.POSITIONS in intents or Intent.GENERAL in intents:
            await self._add_positions(user_id, ctx)
        if Intent.GOALS in intents:
            await self._add_goals(user_id, ctx)
        if Intent.TRANSACTIONS in intents:
            await self._add_transactions(user_id, ctx)
        if Intent.YIELD_LANDSCAPE in intents:
            await self._add_yield_landscape(ctx)

        return ctx

    async def _add_positions(self, user_id: str, ctx: RetrievedContext) -> None:
        vaults = await self._source.user_vaults(user_id)
        if not vaults:
            return
        lines = []
        for v in vaults:
            name = v.get("name", "Vault")
            bal = v.get("balance_usd", 0)
            apy = v.get("apy", 0)
            yield_earned = v.get("yield_earned", 0)
            lines.append(
                f"- {name}: ${bal} balance, {apy}% APY, ${yield_earned} yield earned"
            )
            ctx._record_numbers(bal, apy, yield_earned)
        ctx.sections.append("### Your Vaults\n" + "\n".join(lines))
        ctx.citations.append(
            Citation("positions", f"{len(vaults)} active vault(s) on your account")
        )

    async def _add_goals(self, user_id: str, ctx: RetrievedContext) -> None:
        goals = await self._source.savings_goals(user_id)
        if not goals:
            return
        lines = []
        for g in goals:
            name = g.get("name") or g.get("description") or "Goal"
            target = g.get("target_amount", 0)
            current = g.get("current_amount", 0)
            progress = g.get("progress_pct", 0)
            lines.append(
                f"- {name}: {current}/{target} saved ({progress}% complete)"
            )
            ctx._record_numbers(target, current, progress)
        ctx.sections.append("### Your Savings Goals\n" + "\n".join(lines))
        ctx.citations.append(Citation("goals", f"{len(goals)} savings goal(s)"))

    async def _add_transactions(self, user_id: str, ctx: RetrievedContext) -> None:
        txs = await self._source.recent_transactions(user_id)
        if not txs:
            return
        recent = txs[: self._max_transactions]
        lines = []
        for t in recent:
            kind = t.get("type", "transaction")
            amount = t.get("amount", 0)
            currency = t.get("currency", "")
            when = t.get("created_at", "")
            lines.append(f"- {kind}: {amount} {currency} on {when}")
            ctx._record_numbers(amount)
        ctx.sections.append("### Your Recent Transactions\n" + "\n".join(lines))
        ctx.citations.append(
            Citation("transactions", f"{len(recent)} most recent transaction(s)")
        )

    async def _add_yield_landscape(self, ctx: RetrievedContext) -> None:
        vaults = await self._source.available_vaults()
        if vaults:
            lines = []
            for v in vaults[:8]:
                name = v.get("name", "Vault")
                apy = v.get("apy", 0)
                tier = v.get("risk_tier", "unknown")
                lines.append(f"- {name}: {apy}% APY, {tier} risk")
                ctx._record_numbers(apy)
            ctx.sections.append("### Available Vaults\n" + "\n".join(lines))
            ctx.citations.append(
                Citation("yield_landscape", f"{len(vaults)} available vault(s)")
            )
        rates = await self._source.market_rates()
        if rates:
            lines = []
            for r in rates[:6]:
                protocol = str(r.get("protocol", "unknown")).upper()
                apy = r.get("apy", 0)
                lines.append(f"- {protocol}: {apy} APY")
                ctx._record_numbers(apy)
            ctx.sections.append("### Market Rates\n" + "\n".join(lines))
            ctx.citations.append(Citation("market_rates", "live protocol rates"))
