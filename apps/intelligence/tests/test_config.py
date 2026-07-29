import os
from unittest.mock import patch

import pytest
from pydantic import ValidationError

from app.config import Settings


def test_settings_allows_empty_jwt_in_development() -> None:
    with patch.dict(os.environ, {}, clear=False):
        os.environ.pop("ENVIRONMENT", None)
        settings = Settings()
        assert settings.jwt_secret == ""


def test_settings_allows_empty_jwt_when_development_explicit() -> None:
    with patch.dict(os.environ, {"ENVIRONMENT": "development"}, clear=False):
        settings = Settings()
        assert settings.jwt_secret == ""


def test_settings_rejects_empty_jwt_in_production() -> None:
    with patch.dict(os.environ, {"ENVIRONMENT": "production"}, clear=False):
        with pytest.raises(ValidationError, match="INTELLIGENCE_JWT_SECRET must be set"):
            Settings()


def test_settings_rejects_empty_jwt_in_staging() -> None:
    with patch.dict(os.environ, {"ENVIRONMENT": "staging"}, clear=False):
        with pytest.raises(ValidationError, match="INTELLIGENCE_JWT_SECRET must be set"):
            Settings()


def test_settings_accepts_jwt_in_production() -> None:
    with patch.dict(
        os.environ,
        {
            "ENVIRONMENT": "production",
            "INTELLIGENCE_JWT_SECRET": (
                "test-secret-key-32chars-long!!!!"
            ),
        },
        clear=False,
    ):
        settings = Settings()
        assert settings.jwt_secret == "test-secret-key-32chars-long!!!!"
