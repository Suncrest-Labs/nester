"""Unit tests for DeFiLlamaClient with mocked HTTP responses."""
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


_PROTOCOLS_RESPONSE = [
    {
        "name": "Stellar Pool",
        "slug": "stellar-pool",
        "tvl": 5_000_000,
        "chains": ["Stellar"],
        "category": "DEX",
    },
    {
        "name": "Ethereum Pool",
        "slug": "eth-pool",
        "tvl": 10_000_000,
        "chains": ["Ethereum"],
        "category": "Lending",
    },
]

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
        {
            "pool": "def-456",
            "project": "aave",
            "symbol": "USDC",
            "apy": 6.2,
            "apyBase": 6.2,
            "apyReward": None,
            "tvlUsd": 50_000_000,
            "apyPct7d": -0.1,
            "il7d": None,
            "chain": "Ethereum",
        },
    ]
}

_HISTORY_RESPONSE = {
    "data": [
        {"timestamp": "2024-01-01T00:00:00Z", "apy": 9.1, "tvlUsd": 1_900_000},
        {"timestamp": "2024-01-02T00:00:00Z", "apy": 9.5, "tvlUsd": 2_000_000},
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


@pytest.mark.asyncio
async def test_get_stellar_protocols_filters_by_chain(client):
    mock_resp = _make_mock_response(200, _PROTOCOLS_RESPONSE)
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_stellar_protocols()

    assert len(result) == 1
    assert result[0]["name"] == "Stellar Pool"
    assert result[0]["chain"] == "Stellar"


@pytest.mark.asyncio
async def test_get_stellar_protocols_returns_empty_on_error(client):
    session = AsyncMock()
    session.get = MagicMock(side_effect=Exception("network error"))
    session.__aenter__ = AsyncMock(return_value=session)
    session.__aexit__ = AsyncMock(return_value=False)
    with patch("aiohttp.ClientSession", return_value=session):
        result = await client.get_stellar_protocols()
    assert result == []


@pytest.mark.asyncio
async def test_get_yield_pools_filters_by_chain(client):
    mock_resp = _make_mock_response(200, _POOLS_RESPONSE)
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_yield_pools(chain="Stellar")

    assert len(result) == 1
    assert result[0]["project"] == "blend"
    assert result[0]["chain"] == "Stellar"


@pytest.mark.asyncio
async def test_get_yield_pools_caches_result(client):
    mock_resp = _make_mock_response(200, _POOLS_RESPONSE)
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)) as mock_session:
        await client.get_yield_pools(chain="Stellar")
        await client.get_yield_pools(chain="Stellar")
    # ClientSession only constructed once due to caching
    assert mock_session.call_count == 1


@pytest.mark.asyncio
async def test_get_yield_pools_returns_empty_on_non_200(client):
    mock_resp = _make_mock_response(500, {})
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_yield_pools(chain="Stellar")
    assert result == []


@pytest.mark.asyncio
async def test_get_pool_history_returns_snapshots(client):
    mock_resp = _make_mock_response(200, _HISTORY_RESPONSE)
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_pool_history("abc-123")

    assert len(result) == 2
    assert result[0]["apy"] == 9.1
    assert result[1]["tvlUsd"] == 2_000_000


@pytest.mark.asyncio
async def test_get_pool_history_returns_empty_on_error(client):
    mock_resp = _make_mock_response(404, {})
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_pool_history("nonexistent")
    assert result == []
