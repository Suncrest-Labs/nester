"""Tests for the Prometheus explainability trace (#925)."""

from app.models.explainability import DocumentUsed, ExplainabilityTrace, ToolInvocation
from app.services.prometheus import build_explainability_trace
from app.services.retrieval import Citation, RetrievedContext


def test_trace_with_no_documents_or_tools() -> None:
    ctx = RetrievedContext()
    trace = build_explainability_trace(ctx, [])

    assert isinstance(trace, ExplainabilityTrace)
    assert trace.documents_used == []
    assert trace.tools_invoked == []
    assert "no user-scoped data" in trace.reasoning_summary.lower()


def test_trace_includes_retrieval_citations_as_documents_used() -> None:
    ctx = RetrievedContext(
        sections=["### Your Vaults\n- Balanced: $100 balance"],
        citations=[Citation(source="positions", detail="1 active vault(s) on your account")],
    )
    trace = build_explainability_trace(ctx, [])

    assert len(trace.documents_used) == 1
    doc = trace.documents_used[0]
    assert isinstance(doc, DocumentUsed)
    assert doc.source == "positions"
    assert doc.detail == "1 active vault(s) on your account"
    assert "positions" in trace.reasoning_summary


def test_trace_includes_tool_invocations() -> None:
    ctx = RetrievedContext()
    invocations = [
        ToolInvocation(tool_name="get_vault_apy", arguments={"vault_id": "v1"}, status="executed"),
    ]
    trace = build_explainability_trace(ctx, invocations)

    assert trace.tools_invoked == invocations
    assert "get_vault_apy" in trace.reasoning_summary
    assert "executed" in trace.reasoning_summary


def test_trace_combines_documents_and_tools_in_summary() -> None:
    ctx = RetrievedContext(
        sections=["### Your Savings Goals\n- Car: 100/1000 saved (10% complete)"],
        citations=[Citation(source="goals", detail="1 savings goal(s)")],
    )
    invocations = [
        ToolInvocation(tool_name="propose_rebalance", arguments={}, status="proposed"),
    ]
    trace = build_explainability_trace(ctx, invocations)

    assert len(trace.documents_used) == 1
    assert len(trace.tools_invoked) == 1
    assert "goals" in trace.reasoning_summary
    assert "propose_rebalance" in trace.reasoning_summary


def test_explainability_trace_serializes_to_json() -> None:
    trace = ExplainabilityTrace(
        documents_used=[DocumentUsed(source="positions", detail="1 vault")],
        tools_invoked=[ToolInvocation(tool_name="t", arguments={"a": 1}, status="executed")],
        reasoning_summary="test",
    )
    payload = trace.model_dump_json()
    assert "positions" in payload
    assert "t" in payload
