"""Tests for the DefiLlama staleness guard (#931).

Confirms that once a live fetch has succeeded and populated the cache, a
subsequent DefiLlama outage (non-200 or a raised exception) after the normal
short-TTL cache entry has expired falls back to the last-known-good data
instead of an empty list — so an AI response that references yield data
degrades to slightly-stale numbers instead of having none at all.
"""

from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from app.services.defillama import DeFiLlamaClient, _mem_cache


@pytest.fixture(autouse=True)
def clear_mem_cache():
    _mem_cache.clear()
    yield
    _mem_cache.clear()


@pytest.fixture
def client():
    return DeFiLlamaClient(
        base_url="https://api.llama.fi",
        yields_url="https://yields.llama.fi",
    )


_POOLS_RESPONSE = {
    "data": [
        {
            "pool": "abc-123",
            "project": "blend",
            "symbol": "USDC",
            "apy": 9.5,
            "apyBase": 8.0,
            "apyReward": 1.5,
            "tvlUsd": 2_000_000,
            "apyPct7d": 0.3,
            "il7d": None,
            "chain": "Stellar",
        },
    ]
}


def _make_mock_response(status: int, json_data: object) -> MagicMock:
    mock_resp = AsyncMock()
    mock_resp.status = status
    mock_resp.json = AsyncMock(return_value=json_data)
    mock_resp.__aenter__ = AsyncMock(return_value=mock_resp)
    mock_resp.__aexit__ = AsyncMock(return_value=False)
    return mock_resp


def _make_session(mock_resp: MagicMock) -> MagicMock:
    session = AsyncMock()
    session.get = MagicMock(return_value=mock_resp)
    session.__aenter__ = AsyncMock(return_value=session)
    session.__aexit__ = AsyncMock(return_value=False)
    return session


def _make_failing_session(exc: Exception) -> MagicMock:
    session = AsyncMock()
    session.get = MagicMock(side_effect=exc)
    session.__aenter__ = AsyncMock(return_value=session)
    session.__aexit__ = AsyncMock(return_value=False)
    return session


async def _prime_cache_and_expire_short_ttl(client: DeFiLlamaClient) -> None:
    """Populate both the normal cache and the stale fallback via one
    successful fetch, then simulate the short-TTL entry expiring (an
    outage happening *after* the normal cache would already be a miss)
    without waiting out the real 15-minute TTL."""
    mock_resp = _make_mock_response(200, _POOLS_RESPONSE)
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_yield_pools(chain="Stellar")
    assert result  # sanity: the priming fetch actually returned data

    # Expire only the short-TTL entry, leaving the long-TTL stale copy intact
    # — mirrors real behavior (different TTLs), without sleeping in a test.
    key = "defillama:pools:stellar"
    del _mem_cache[key]
    assert key + ":stale" in _mem_cache


@pytest.mark.asyncio
async def test_outage_after_ttl_expiry_serves_stale_data_on_non_200(client):
    await _prime_cache_and_expire_short_ttl(client)

    mock_resp = _make_mock_response(500, {})
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_yield_pools(chain="Stellar")

    assert result, "expected stale fallback data, got empty list"
    assert result[0]["project"] == "blend"


@pytest.mark.asyncio
async def test_outage_after_ttl_expiry_serves_stale_data_on_exception(client):
    await _prime_cache_and_expire_short_ttl(client)

    with patch("aiohttp.ClientSession", return_value=_make_failing_session(Exception("network down"))):
        result = await client.get_yield_pools(chain="Stellar")

    assert result, "expected stale fallback data, got empty list"
    assert result[0]["project"] == "blend"


@pytest.mark.asyncio
async def test_outage_with_no_prior_successful_fetch_still_returns_empty(client):
    # No priming fetch — nothing to fall back to, so behavior must be
    # unchanged from before the staleness guard: an empty list, not an error.
    mock_resp = _make_mock_response(500, {})
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_yield_pools(chain="Stellar")

    assert result == []


@pytest.mark.asyncio
async def test_stale_fallback_is_not_used_when_a_fresh_cache_entry_exists(client):
    # Prime the cache and do NOT expire it — the fresh, non-stale path
    # should serve normally and never even attempt a live fetch, let alone
    # need the fallback.
    mock_resp = _make_mock_response(200, _POOLS_RESPONSE)
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)) as mock_session:
        await client.get_yield_pools(chain="Stellar")
        result = await client.get_yield_pools(chain="Stellar")

    assert mock_session.call_count == 1
    assert result[0]["project"] == "blend"
