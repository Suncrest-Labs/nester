"""Pydantic models for AI explainability traces (#925).

An `ExplainabilityTrace` is a structured record of what informed an
AI-suggested action or answer: which retrieved data sections were used,
which tools were invoked (and with what outcome), and a short human-readable
summary of the reasoning. It is built alongside a chat response so a user (or
an auditor) can see why Prometheus said what it said, without inspecting raw
logs.
"""

from typing import Any

from pydantic import BaseModel, Field


class DocumentUsed(BaseModel):
    """One retrieved, user-scoped data section that informed the response."""

    source: str
    detail: str


class ToolInvocation(BaseModel):
    """One tool call made while producing the response."""

    tool_name: str
    arguments: dict[str, Any] = Field(default_factory=dict)
    status: str  # "executed" | "proposed" | "failed" | "rejected"


class ExplainabilityTrace(BaseModel):
    """Structured explanation of what informed an AI-suggested action or answer."""

    documents_used: list[DocumentUsed] = Field(default_factory=list)
    tools_invoked: list[ToolInvocation] = Field(default_factory=list)
    reasoning_summary: str = ""
