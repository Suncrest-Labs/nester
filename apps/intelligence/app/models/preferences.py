"""Pydantic models for per-user AI response tone/style preferences (#927).

Prometheus lets a user choose how it talks to them: how long answers are
(concise vs detailed) and how formal the voice is (casual vs formal). These
are threaded into system-prompt construction in `app.services.claude` so the
same underlying facts get reworded to match the user's taste, without
changing the grounding/scope rules in the base system prompt.
"""

from typing import Literal

from pydantic import BaseModel

ResponseLength = Literal["concise", "detailed"]
ResponseTone = Literal["casual", "formal"]


class ResponsePreferences(BaseModel):
    """A user's saved preference for how Prometheus should style its replies.

    Defaults match Prometheus's existing baseline voice (warm/casual, and
    reasoned/detailed answers per the base SYSTEM_PROMPT), so a user with no
    preference on file sees no behavior change.
    """

    response_length: ResponseLength = "detailed"
    response_tone: ResponseTone = "casual"
