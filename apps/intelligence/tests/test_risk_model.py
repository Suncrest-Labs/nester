from app.services import risk_model


def test_risk_adjusted_score_rewards_higher_apy():
    low = risk_model.risk_adjusted_score(apy=5.0, risk_score=0.3, liquidity_score=0.8)
    high = risk_model.risk_adjusted_score(apy=15.0, risk_score=0.3, liquidity_score=0.8)
    assert high > low


def test_risk_adjusted_score_penalizes_higher_risk():
    safer = risk_model.risk_adjusted_score(apy=10.0, risk_score=0.2, liquidity_score=0.8)
    riskier = risk_model.risk_adjusted_score(apy=10.0, risk_score=0.8, liquidity_score=0.8)
    assert safer > riskier


def test_risk_adjusted_score_never_divides_by_zero():
    # Zero risk and full liquidity would naively divide by zero.
    score = risk_model.risk_adjusted_score(apy=10.0, risk_score=0.0, liquidity_score=1.0)
    assert score == score  # not NaN/inf
    assert abs(score) < 1e6


def test_composite_risk_score_bounds():
    factors = risk_model.get_risk_factors("blend")
    score = risk_model.composite_risk_score(factors)
    assert 0.0 <= score <= 1.0


def test_unknown_protocol_falls_back_to_conservative_defaults():
    known = risk_model.get_risk_factors("blend")
    unknown = risk_model.get_risk_factors("some_new_protocol")
    assert unknown is not known
    assert unknown.audit_score <= known.audit_score


def test_score_protocol_matches_manual_computation():
    factors = risk_model.get_risk_factors("lobstr")
    risk = risk_model.composite_risk_score(factors)
    expected = risk_model.risk_adjusted_score(10.0, risk, factors.liquidity_score)
    assert risk_model.score_protocol("lobstr", 10.0) == expected
