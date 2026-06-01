"""Unit tests for CoinGeckoClient with mocked HTTP responses."""
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from app.services.coingecko import CoinGeckoClient, _mem_cache


@pytest.fixture(autouse=True)
def clear_mem_cache():
    _mem_cache.clear()
    yield
    _mem_cache.clear()


@pytest.fixture
def client():
    return CoinGeckoClient()


_PRICE_RESPONSE = {
    "usd-coin": {
        "usd": 1.0002,
        "usd_24h_change": 0.01,
        "usd_market_cap": 42_000_000_000.0,
    },
    "stellar": {
        "usd": 0.12,
        "usd_24h_change": -3.5,
        "usd_market_cap": 3_500_000_000.0,
    },
}

_DEFI_GLOBAL_RESPONSE = {
    "data": {
        "defi_market_cap": "85000000000",
        "defi_dominance": "4.2",
        "trading_volume_24h": "6000000000",
    }
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
async def test_get_prices_returns_price_data(client):
    mock_resp = _make_mock_response(200, _PRICE_RESPONSE)
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_prices(["usd-coin", "stellar"])

    assert "usd-coin" in result
    assert abs(result["usd-coin"].usd - 1.0002) < 1e-6
    assert result["usd-coin"].usd_24h_change == pytest.approx(0.01)
    assert "stellar" in result
    assert result["stellar"].usd_24h_change == pytest.approx(-3.5)


@pytest.mark.asyncio
async def test_get_prices_returns_empty_on_429(client):
    mock_resp = _make_mock_response(429, {})
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_prices(["usd-coin"])
    assert result == {}


@pytest.mark.asyncio
async def test_get_prices_returns_empty_on_network_error(client):
    session = AsyncMock()
    session.get = MagicMock(side_effect=Exception("connection refused"))
    session.__aenter__ = AsyncMock(return_value=session)
    session.__aexit__ = AsyncMock(return_value=False)
    with patch("aiohttp.ClientSession", return_value=session):
        result = await client.get_prices(["usd-coin"])
    assert result == {}


@pytest.mark.asyncio
async def test_get_prices_caches_result(client):
    mock_resp = _make_mock_response(200, _PRICE_RESPONSE)
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)) as mock_session:
        await client.get_prices(["usd-coin", "stellar"])
        await client.get_prices(["usd-coin", "stellar"])
    assert mock_session.call_count == 1


@pytest.mark.asyncio
async def test_get_market_sentiment_bull(client):
    # vol_ratio = 6B / 85B ≈ 0.071 > 0.05 → bull
    mock_resp = _make_mock_response(200, _DEFI_GLOBAL_RESPONSE)
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_market_sentiment()

    assert result.signal == "bull"
    assert result.defi_market_cap_usd == pytest.approx(85_000_000_000, rel=1e-3)
    assert result.defi_dominance_pct == pytest.approx(4.2, rel=1e-3)


@pytest.mark.asyncio
async def test_get_market_sentiment_bear():
    client = CoinGeckoClient()
    bear_response = {
        "data": {
            "defi_market_cap": "100000000000",
            "defi_dominance": "3.0",
            "trading_volume_24h": "1000000000",  # vol_ratio = 0.01 < 0.02 → bear
        }
    }
    mock_resp = _make_mock_response(200, bear_response)
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_market_sentiment()
    assert result.signal == "bear"


@pytest.mark.asyncio
async def test_get_market_sentiment_returns_neutral_on_429(client):
    mock_resp = _make_mock_response(429, {})
    with patch("aiohttp.ClientSession", return_value=_make_session(mock_resp)):
        result = await client.get_market_sentiment()
    assert result.signal == "neutral"
    assert result.defi_market_cap_usd == 0.0


@pytest.mark.asyncio
async def test_get_market_sentiment_returns_neutral_on_error(client):
    session = AsyncMock()
    session.get = MagicMock(side_effect=Exception("timeout"))
    session.__aenter__ = AsyncMock(return_value=session)
    session.__aexit__ = AsyncMock(return_value=False)
    with patch("aiohttp.ClientSession", return_value=session):
        result = await client.get_market_sentiment()
    assert result.signal == "neutral"
