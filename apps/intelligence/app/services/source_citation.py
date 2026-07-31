"""Source attribution for on-chain and market data cited in Prometheus responses.

Tracks which protocol/API a TVL or APY figure came from and the timestamp it
was retrieved, so a response can cite provenance instead of presenting a
number as if it were always live and current.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone


def now_utc() -> datetime:
    return datetime.now(timezone.utc)


@dataclass(frozen=True)
class RetrievalSource:
    """One attributable data point: what it is, where it came from, and when."""

    label: str
    protocol: str
    as_of: datetime

    def citation(self) -> str:
        """Render as an inline citation, e.g. '(source: defillama, as of 2026-07-27 14:03 UTC)'."""
        stamp = self.as_of.astimezone(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
        return f"(source: {self.protocol}, as of {stamp})"


def parse_as_of(value: str | None) -> datetime:
    """Parse an ISO timestamp, falling back to the current time if missing/invalid."""
    if value:
        try:
            parsed = datetime.fromisoformat(value)
            return parsed if parsed.tzinfo else parsed.replace(tzinfo=timezone.utc)
        except ValueError:
            pass
    return now_utc()


def format_sources_block(sources: list[RetrievalSource]) -> str:
    """Render a de-duplicated '## Data Sources' block for prompt context.

    Keeps only the most recent as_of per (protocol, label) pair.
    """
    if not sources:
        return ""

    latest: dict[tuple[str, str], RetrievalSource] = {}
    for source in sources:
        key = (source.protocol, source.label)
        existing = latest.get(key)
        if existing is None or source.as_of > existing.as_of:
            latest[key] = source

    lines = [f"- {source.label}: {source.citation()}" for source in latest.values()]
    return "## Data Sources\n" + "\n".join(lines)
