"""Tests for the multilingual language resolution, glossary, and formatting layer."""

from app.services import i18n


def test_resolve_language_prefers_explicit_preference_over_message_language():
    # French-looking message, but the stored preference is Swahili — the
    # preference must win (it must not "switch" based on one message).
    result = i18n.resolve_language("sw", "Combien coûte mon compte épargne ?")
    assert result == "sw"


def test_resolve_language_falls_back_to_detection_when_no_preference():
    result = i18n.resolve_language(None, "Combien pour mon épargne et le rendement?")
    assert result == "fr"


def test_resolve_language_defaults_to_english_when_undetectable():
    assert i18n.resolve_language(None, None) == i18n.DEFAULT_LANGUAGE
    assert i18n.resolve_language(None, "1234") == i18n.DEFAULT_LANGUAGE


def test_resolve_language_ignores_unsupported_preference():
    # An unsupported/garbage preference is not authoritative — falls back to
    # detection (or default) instead of erroring.
    assert i18n.resolve_language("klingon", None) == i18n.DEFAULT_LANGUAGE


def test_detect_language_recognizes_arabic_script():
    assert i18n.detect_language("كم أحتاج أن أدخر شهريا؟") == "ar"


def test_parse_language_preference_normalizes_region_subtags():
    assert i18n.parse_language_preference("fr-FR") == "fr"
    assert i18n.parse_language_preference("") is None
    assert i18n.parse_language_preference(None) is None


def test_glossary_covers_every_supported_language_with_no_gaps():
    english_keys = set(i18n.FINANCIAL_GLOSSARY["en"].keys())
    for lang in i18n.SUPPORTED_LANGUAGES:
        glossary = i18n.glossary_for(lang)
        assert set(glossary.keys()) == english_keys, f"glossary gap for {lang}"
        for term in glossary.values():
            assert term.strip(), f"empty glossary term in {lang}"


def test_language_directive_uses_established_term_not_literal_translation():
    directive = i18n.language_directive("fr")
    # "intérêts composés" is the established French term for compound
    # interest — a literal word-for-word translation would not use it.
    assert "intérêts composés" in directive
    assert "rendement" in directive


def test_language_directive_reiterates_guardrails_in_target_language():
    directive = i18n.language_directive("sw")
    # Grounding, refusal-on-absence, and non-advice framing must all still
    # be instructed even when the response language changes.
    assert "grounded" in directive.lower() or "provided context" in directive.lower()
    assert "unavailable" in directive.lower()


def test_language_directive_is_minimal_for_english():
    directive = i18n.language_directive("en")
    assert "Respond in English" in directive


def test_structured_output_language_note_empty_for_english():
    assert i18n.structured_output_language_note("en") == ""


def test_structured_output_language_note_preserves_schema_keys_in_english():
    note = i18n.structured_output_language_note("pt")
    assert "JSON keys" in note
    assert "English" in note


def test_format_amount_english_uses_comma_grouping_and_period_decimal():
    assert i18n.format_amount(1234.5, "USD", "en") == "$1,234.50"


def test_format_amount_french_uses_space_grouping_and_comma_decimal():
    result = i18n.format_amount(1234.5, "USD", "fr")
    assert result == "1 234,50 $"


def test_format_amount_crypto_ticker_is_suffixed_not_symbolized():
    assert i18n.format_amount(1000, "USDC", "en") == "1,000.00 USDC"


def test_format_amount_negative_preserves_sign():
    result = i18n.format_amount(-42.5, "USD", "en")
    assert result.startswith("-")


def test_format_percentage_arabic_uses_arabic_percent_sign():
    result = i18n.format_percentage(12.5, "ar")
    assert result.endswith("٪")


def test_format_percentage_default_two_decimals():
    assert i18n.format_percentage(8, "en") == "8.00%"


def test_format_date_localizes_month_name():
    result = i18n.format_date("2026-03-15T00:00:00Z", "fr")
    assert "mars" in result
    assert "2026" in result


def test_format_date_falls_back_to_raw_string_on_parse_failure():
    assert i18n.format_date("not-a-date", "en") == "not-a-date"


def test_build_portfolio_narrative_localizes_fixed_shape_text():
    narrative = i18n.build_portfolio_narrative(
        total_value="$1,000.00",
        vault_count=2,
        yield_earned="$10.00",
        risk_level="moderate",
        top_recommendation="Continuez ainsi.",
        language="fr",
    )
    assert "coffre(s)" in narrative
    assert "Continuez ainsi." in narrative
