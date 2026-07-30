from typing import Dict

from pydantic import BaseModel


class NudgeCopyRequest(BaseModel):
    nudge_type: str
    segment: str
    facts: Dict[str, str]
    request_id: str = ""


class NudgeCopyResponse(BaseModel):
    title: str
    body: str
