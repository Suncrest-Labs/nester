"""Tests for per-user AI response tone/style preferences (#927)."""

from app.models.preferences import ResponsePreferences
from app.services.claude import apply_tone_preferences, build_tone_instructions


def test_build_tone_instructions_none_is_empty() -> None:
    assert build_tone_instructions(None) == ""


def test_apply_tone_preferences_none_is_noop() -> None:
    base = "BASE PROMPT"
    assert apply_tone_preferences(base, None) == base


def test_default_preferences_produce_detailed_casual_instructions() -> None:
    prefs = ResponsePreferences()
    assert prefs.response_length == "detailed"
    assert prefs.response_tone == "casual"

    block = build_tone_instructions(prefs)
    assert "thorough" in block.lower()
    assert "casual" in block.lower()


def test_concise_preference_differs_from_detailed() -> None:
    concise = build_tone_instructions(
        ResponsePreferences(response_length="concise", response_tone="casual")
    )
    detailed = build_tone_instructions(
        ResponsePreferences(response_length="detailed", response_tone="casual")
    )
    assert concise != detailed
    assert "short" in concise.lower()
    assert "thorough" in detailed.lower()


def test_formal_preference_differs_from_casual() -> None:
    formal = build_tone_instructions(
        ResponsePreferences(response_length="concise", response_tone="formal")
    )
    casual = build_tone_instructions(
        ResponsePreferences(response_length="concise", response_tone="casual")
    )
    assert formal != casual
    assert "professional" in formal.lower()
    assert "warm" in casual.lower()


def test_apply_tone_preferences_appends_to_base_prompt() -> None:
    base = "You are Prometheus."
    prefs = ResponsePreferences(response_length="concise", response_tone="formal")
    result = apply_tone_preferences(base, prefs)
    assert result.startswith(base)
    assert "concise" not in result.lower() or "short" in result.lower()
    assert "professional" in result.lower()


def test_all_four_combinations_are_distinct() -> None:
    combos = [
        ResponsePreferences(response_length=length, response_tone=tone)
        for length in ("concise", "detailed")
        for tone in ("casual", "formal")
    ]
    blocks = {build_tone_instructions(p) for p in combos}
    assert len(blocks) == 4
