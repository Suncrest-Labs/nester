Overview
The current /analyze endpoint takes free-form user input and returns an AI text response. A dedicated portfolio analysis endpoint uses Claude's tool use to produce a structured, machine-readable portfolio breakdown — enabling the DApp to render charts and cards from AI analysis rather than just free text.

Endpoint
POST /intelligence/portfolio/analyze
Authorization: Bearer <JWT>
Request body: none (user's vault positions fetched from context).

Claude Tool Use Schema
Define analysis tools that Claude calls to produce structured output:

tools = [
    {
        "name": "portfolio_breakdown",
        "description": "Return a structured portfolio analysis",
        "input_schema": {
            "properties": {
                "total_value_usdc": {"type": "number"},
                "yield_30d_usdc": {"type": "number"},
                "allocation_breakdown": [{"protocol": str, "weight": float, "apy": float}],
                "risk_level": {"enum": ["conservative", "moderate", "aggressive"]},
                "top_recommendation": {"type": "string"},
                "rebalance_suggested": {"type": "boolean"}
            }
        }
    }
]
Response
{
  "analysis": { ...structured fields... },
  "narrative": "Here is my analysis of your portfolio...",
  "confidence": "high",
  "generated_at": "2026-05-29T10:00:00Z"
}
Acceptance Criteria

POST /intelligence/portfolio/analyze endpoint implemented

Claude tool use schema produces all required structured fields

Response validated against Pydantic model before returning

Narrative explanation generated alongside structured data

DApp portfolio page renders AI analysis cards from structured response

Unit tests: mock Claude response → verify structured parsing

Rate limit: 5 requests/user/hour (analysis is expensive)
Phase
Phase 4 — AI Intelligence Layer

Activity
