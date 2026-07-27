"""Shared deterministic financial math helpers.

Pure functions with no I/O and no LLM/randomness involvement anywhere. Used by
both `SavingsService` (goal deposit planning, #issue predates this file) and
`recommendation_engine` (candidate generation, #847) so the amortization math
lives in exactly one place and every caller gets the identical, auditable
computation.
"""

from __future__ import annotations

import math
from typing import Optional


def required_monthly_deposit(future_value: float, monthly_rate: float, months: int) -> float:
    """Return the fixed monthly deposit needed to reach `future_value` in
    `months` months at `monthly_rate` (a periodic rate, e.g. annual_apy / 12).

    P = FV * (r / ((1 + r)^n - 1))  when r > 0
    P = FV / n                       when r == 0 (no yield)
    """
    if months <= 0:
        return future_value
    if monthly_rate > 0:
        return future_value * (monthly_rate / ((1 + monthly_rate) ** months - 1))
    return future_value / months


def months_to_reach_target(
    future_value: float, monthly_rate: float, monthly_deposit: float
) -> Optional[float]:
    """Return the number of months needed to reach `future_value` by
    depositing `monthly_deposit` every month at `monthly_rate`.

    Returns None when the goal is unreachable at this deposit rate (deposit is
    zero or negative, or the amortization ratio is non-positive).

    Inverts the future-value-of-annuity formula:
      FV = P * (((1 + r)^n - 1) / r)   =>   n = log(1 + FV*r/P) / log(1 + r)
    """
    if future_value <= 0:
        return 0.0
    if monthly_deposit <= 0:
        return None
    if monthly_rate > 0:
        ratio = 1 + (future_value * monthly_rate / monthly_deposit)
        if ratio <= 0:
            return None
        return math.log(ratio) / math.log(1 + monthly_rate)
    return future_value / monthly_deposit
