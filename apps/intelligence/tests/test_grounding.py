"""Tests for grounding rules and numeric validation (#852)."""

from app.services.grounding import (
    GROUNDING_RULES,
    REFUSAL_MARKER,
    build_grounded_system_prompt,
    is_refusal,
    unsupported_numbers,
    validate_grounding,
)
from app.services.retrieval import RetrievedContext


def _ctx_with_facts(*numbers: str) -> RetrievedContext:
    ctx = RetrievedContext()
    ctx.sections.append("### Your Vaults\n- Growth: $1200 balance, 8.5% APY")
    ctx.facts.update(numbers)
    return ctx


class TestGroundedPrompt:
    def test_prompt_includes_rules_and_context(self):
        ctx = _ctx_with_facts("1200", "8.5")
        prompt = build_grounded_system_prompt("BASE PERSONA", ctx)
        assert "BASE PERSONA" in prompt
        assert "Grounding rules" in prompt
        assert "Retrieved Context" in prompt
        # The injection-defense clause must be present.
        assert "untrusted text" in GROUNDING_RULES


class TestValidation:
    def test_supported_numbers_pass(self):
        ctx = _ctx_with_facts("1200", "8.5")
        ok, unsupported = validate_grounding(
            "You have $1,200 earning 8.5% APY.", ctx
        )
        assert ok
        assert unsupported == []

    def test_invented_number_is_flagged(self):
        ctx = _ctx_with_facts("1200", "8.5")
        ok, unsupported = validate_grounding(
            "You have $1,200 earning 8.5% APY, projected to reach $9,999 next year.",
            ctx,
        )
        assert not ok
        assert "9999" in unsupported

    def test_refusal_is_always_grounded(self):
        ctx = _ctx_with_facts()  # no facts
        answer = f"{REFUSAL_MARKER}. I don't see any transactions on your account."
        ok, unsupported = validate_grounding(answer, ctx)
        assert ok
        assert unsupported == []

    def test_is_refusal_detection(self):
        assert is_refusal(f"Sorry, {REFUSAL_MARKER}.")
        assert not is_refusal("You have $1,200 in your vault.")

    def test_unsupported_numbers_lists_only_ungrounded(self):
        ctx = _ctx_with_facts("100")
        # 100 is supported; 250 is not.
        result = unsupported_numbers("You saved 100 toward a 250 target.", ctx)
        assert result == ["250"]
