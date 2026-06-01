from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from app.services.vault_context import VaultContextFetcher


class TestVaultContextFetcher:
    @pytest.fixture
    def fetcher(self):
        return VaultContextFetcher(
            api_base_url="https://api.test.com",
            service_api_key="test-key"
        )

    @pytest.mark.asyncio
    async def test_fetch_user_vaults_success(self, fetcher):
        mock_response_data = {
            "vaults": [
                {
                    "name": "Test Vault 1",
                    "total_balance_usd": 1000.0,
                    "average_apy": 0.08,
                    "yield_earned_usd": 50.0,
                    "lock_period_days": 30,
                    "allocations": [
                        {"protocol": "Aave", "amount_usd": 600.0, "apy": 0.08},
                        {"protocol": "Blend", "amount_usd": 400.0, "apy": 0.12}
                    ],
                    "id": "vault-1"
                }
            ]
        }

        with patch('aiohttp.ClientSession') as mock_session:
            mock_response = AsyncMock()
            mock_response.status = 200
            mock_response.json = AsyncMock(return_value=mock_response_data)

            mock_get_cm = AsyncMock()
            mock_get_cm.__aenter__.return_value = mock_response
            mock_session_instance = MagicMock()
            mock_session_instance.get.return_value = mock_get_cm
            mock_session.return_value.__aenter__.return_value = mock_session_instance

            result = await fetcher.fetch_user_vaults("test-user-id")

            assert len(result) == 1
            vault = result[0]
            assert vault["name"] == "Test Vault 1"
            assert vault["balance_usd"] == 1000.0
            assert vault["apy"] == 0.08
            assert vault["yield_earned"] == 50.0
            assert vault["lock_period_days"] == 30
            assert vault["id"] == "vault-1"
            # Check allocation breakdown
            assert vault["allocation_breakdown"]["Aave"] == 60.0  # 600/1000 * 100
            assert vault["allocation_breakdown"]["Blend"] == 40.0  # 400/1000 * 100

    @pytest.mark.asyncio
    async def test_fetch_user_vaults_http_error(self, fetcher):
        with patch('aiohttp.ClientSession') as mock_session:
            mock_response = AsyncMock()
            mock_response.status = 500

            mock_get_cm = AsyncMock()
            mock_get_cm.__aenter__.return_value = mock_response
            mock_session_instance = MagicMock()
            mock_session_instance.get.return_value = mock_get_cm
            mock_session.return_value.__aenter__.return_value = mock_session_instance

            result = await fetcher.fetch_user_vaults("test-user-id")

            assert result == []

    @pytest.mark.asyncio
    async def test_fetch_market_rates_success(self, fetcher):
        mock_response_data = {
            "data": [
                {
                    "project": "Aave",
                    "symbol": "aUSDC",
                    "apy": 0.065,
                    "tvlUsd": 1000000,
                    "chain": "Ethereum"
                },
                {
                    "project": "Blend",
                    "symbol": "blendUSDC",
                    "apy": 0.09,
                    "tvlUsd": 500000,
                    "chain": "Stellar"
                },
                {
                    "project": "Compound",
                    "symbol": "cUSDC",
                    "apy": 0.058,
                    "tvlUsd": 800000,
                    "chain": "Ethereum"
                },
                {
                    "project": "SomeOther",
                    "symbol": "other",
                    "apy": 0.03,
                    "tvlUsd": 100000,
                    "chain": "Polygon"
                }
            ]
        }

        with patch('aiohttp.ClientSession') as mock_session:
            mock_response = AsyncMock()
            mock_response.status = 200
            mock_response.json = AsyncMock(return_value=mock_response_data)

            mock_get_cm = AsyncMock()
            mock_get_cm.__aenter__.return_value = mock_response
            mock_session_instance = MagicMock()
            mock_session_instance.get.return_value = mock_get_cm
            mock_session.return_value.__aenter__.return_value = mock_session_instance

            result = await fetcher.fetch_market_rates()

            # Should only return Aave, Blend, Compound
            assert len(result) == 3
            assert result[0]["protocol"] == "aave"
            assert result[1]["protocol"] == "blend"
            assert result[2]["protocol"] == "compound"

    @pytest.mark.asyncio
    async def test_fetch_market_rates_http_error_fallback(self, fetcher):
        with patch('aiohttp.ClientSession') as mock_session:
            mock_response = AsyncMock()
            mock_response.status = 500

            mock_get_cm = AsyncMock()
            mock_get_cm.__aenter__.return_value = mock_response
            mock_session_instance = MagicMock()
            mock_session_instance.get.return_value = mock_get_cm
            mock_session.return_value.__aenter__.return_value = mock_session_instance

            result = await fetcher.fetch_market_rates()

            # Should return fallback rates
            assert len(result) == 3
            assert result[0]["protocol"] == "aave"
            assert result[0]["apy"] == 0.065
            assert result[1]["protocol"] == "blend"
            assert result[1]["apy"] == 0.09
            assert result[2]["protocol"] == "compound"
            assert result[2]["apy"] == 0.058

    @pytest.mark.asyncio
    async def test_fetch_market_rates_exception_fallback(self, fetcher):
        with patch('aiohttp.ClientSession') as mock_session:
            mock_session.side_effect = Exception("Network error")

            result = await fetcher.fetch_market_rates()

            # Should return fallback rates
            assert len(result) == 3
            assert result[0]["protocol"] == "aave"
            assert result[0]["apy"] == 0.065
            assert result[1]["protocol"] == "blend"
            assert result[1]["apy"] == 0.09
            assert result[2]["protocol"] == "compound"
            assert result[2]["apy"] == 0.058

    @pytest.mark.asyncio
    async def test_fetch_vault_risk_success(self, fetcher):
        mock_response_data = {
            "overall": 54.0,
            "tier": "medium",
            "concentration_risk": 0.61,
            "protocol_risk": 0.28,
            "yield_volatility": 0.15,
            "liquidity_risk": 0.09,
            "computed_at": "2025-03-15T10:00:00Z"
        }

        with patch('aiohttp.ClientSession') as mock_session:
            mock_response = AsyncMock()
            mock_response.status = 200
            mock_response.json = AsyncMock(return_value=mock_response_data)

            mock_get_cm = AsyncMock()
            mock_get_cm.__aenter__.return_value = mock_response
            mock_session_instance = MagicMock()
            mock_session_instance.get.return_value = mock_get_cm
            mock_session.return_value.__aenter__.return_value = mock_session_instance

            result = await fetcher.fetch_vault_risk("test-vault-id")

            assert result["overall"] == 54.0
            assert result["tier"] == "medium"
            assert result["concentration_risk"] == 0.61
            assert result["protocol_risk"] == 0.28
            assert result["yield_volatility"] == 0.15
            assert result["liquidity_risk"] == 0.09

    @pytest.mark.asyncio
    async def test_fetch_vault_risk_http_error(self, fetcher):
        with patch('aiohttp.ClientSession') as mock_session:
            mock_response = AsyncMock()
            mock_response.status = 500

            mock_get_cm = AsyncMock()
            mock_get_cm.__aenter__.return_value = mock_response
            mock_session_instance = MagicMock()
            mock_session_instance.get.return_value = mock_get_cm
            mock_session.return_value.__aenter__.return_value = mock_session_instance

            result = await fetcher.fetch_vault_risk("test-vault-id")

            assert result == {}

    def test_build_context_block_with_data(self, fetcher):
        vaults = [
            {
                "name": "Test Vault",
                "balance_usd": 1000.0,
                "apy": 8.5,
                "yield_earned": 50.0,
                "allocation_breakdown": {"Aave": 60.0, "Blend": 40.0},
                "lock_period_days": 30,
                "id": "vault-1"
            }
        ]

        market_rates = [
            {"protocol": "aave", "apy": 0.065},
            {"protocol": "blend", "apy": 0.09},
            {"protocol": "compound", "apy": 0.058}
        ]

        result = fetcher.build_context_block(vaults, market_rates)

        assert "## User Portfolio" in result
        expected = (
            "- Test Vault: $1,000.00 balance, 8.50% APY, "
            "Allocation: [Aave: 60.0%, Blend: 40.0%]"
        )
        assert expected in result
        assert "## Current Market Rates (Live)" in result
        assert "- AAVE: 6.50% APY" in result
        assert "- BLEND: 9.00% APY" in result
        assert "- COMPOUND: 5.80% APY" in result

    def test_build_context_block_empty_vaults(self, fetcher):
        vaults = []
        market_rates = [{"protocol": "aave", "apy": 0.065}]

        result = fetcher.build_context_block(vaults, market_rates)

        assert "The user has no active vaults." in result
        assert "## Current Market Rates (Live)" in result

    def test_build_context_block_empty_market_rates(self, fetcher):
        vaults = [{
            "name": "Test Vault",
            "balance_usd": 1000.0,
            "apy": 8.5,
            "yield_earned": 50.0,
            "allocation_breakdown": {"Aave": 100.0},
            "lock_period_days": 30,
            "id": "vault-1"
        }]
        market_rates = []

        result = fetcher.build_context_block(vaults, market_rates)

        assert "## User Portfolio" in result
        assert "Market data unavailable." in result

    def test_build_risk_profile_block_with_data(self, fetcher):
        vaults = [
            {
                "name": "Test Vault",
                "balance_usd": 1000.0,
                "apy": 8.5,
                "yield_earned": 50.0,
                "allocation_breakdown": {"Aave": 60.0, "Blend": 40.0},
                "lock_period_days": 30,
                "id": "vault-1"
            }
        ]

        risk_data = {
            "vault-1": {
                "overall": 54.0,
                "tier": "medium",
                "concentration_risk": 0.61,
                "protocol_risk": 0.28,
                "yield_volatility": 0.15,
                "liquidity_risk": 0.09
            }
        }

        result = fetcher.build_risk_profile_block(vaults, risk_data)

        assert "## Risk Profile" in result
        assert "- Test Vault: medium risk (score 54/100)." in result
        assert "Primary driver:" in result
        assert "Recommendation:" in result

    def test_build_risk_profile_block_empty_vaults(self, fetcher):
        vaults = []
        risk_data = {}

        result = fetcher.build_risk_profile_block(vaults, risk_data)

        assert "## Risk Profile" in result
        assert "No vaults to assess risk." in result

    def test_build_risk_profile_block_missing_risk_data(self, fetcher):
        vaults = [
            {
                "name": "Test Vault",
                "balance_usd": 1000.0,
                "apy": 8.5,
                "yield_earned": 50.0,
                "allocation_breakdown": {"Aave": 60.0, "Blend": 40.0},
                "lock_period_days": 30,
                "id": "vault-1"
            }
        ]

        risk_data = {}  # Empty risk data

        result = fetcher.build_risk_profile_block(vaults, risk_data)

        assert "## Risk Profile" in result
        assert "- Test Vault: Risk data unavailable" in result

    def test_generate_risk_recommendation_low_tier(self, fetcher):
        recommendation = fetcher._generate_risk_recommendation("low", "concentration_risk", {})
        assert "well-balanced" in recommendation
        assert "maintaining current allocation" in recommendation

    def test_generate_risk_recommendation_medium_tier_concentration(self, fetcher):
        recommendation = fetcher._generate_risk_recommendation("medium", "concentration_risk", {})
        assert "diversifying across additional protocols" in recommendation

    def test_generate_risk_recommendation_medium_tier_protocol(self, fetcher):
        recommendation = fetcher._generate_risk_recommendation("medium", "protocol_risk", {})
        assert "shifting allocation toward lower-risk protocols" in recommendation

    def test_generate_risk_recommendation_medium_tier_yield_volatility(self, fetcher):
        recommendation = fetcher._generate_risk_recommendation("medium", "yield_volatility", {})
        assert "allocating to more stable vaults" in recommendation

    def test_generate_risk_recommendation_medium_tier_liquidity(self, fetcher):
        recommendation = fetcher._generate_risk_recommendation("medium", "liquidity_risk", {})
        assert "vault size may be large relative to protocol market size" in recommendation

    def test_generate_risk_recommendation_high_tier_concentration(self, fetcher):
        recommendation = fetcher._generate_risk_recommendation("high", "concentration_risk", {})
        assert "Strongly consider diversifying" in recommendation

    def test_generate_risk_recommendation_high_tier_protocol(self, fetcher):
        recommendation = fetcher._generate_risk_recommendation("high", "protocol_risk", {})
        assert "consider reallocating to lower-risk protocols" in recommendation

    def test_generate_risk_recommendation_high_tier_yield_volatility(self, fetcher):
        recommendation = fetcher._generate_risk_recommendation("high", "yield_volatility", {})
        assert "consider moving to more stable yield strategies" in recommendation

    def test_generate_risk_recommendation_high_tier_liquidity(self, fetcher):
        recommendation = fetcher._generate_risk_recommendation("high", "liquidity_risk", {})
        assert "consider reducing position size" in recommendation
