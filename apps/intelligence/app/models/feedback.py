"""Pydantic models for conversation rating and feedback capture (#926)."""

from pydantic import BaseModel, Field


class FeedbackRequest(BaseModel):
    """Request body for submitting conversation feedback.

    Attributes:
        rating: "thumbs_up" or "thumbs_down"
        comment: Optional free-text explanation for the rating.
        conversation_id: Identifier linking feedback to a specific
            conversation turn (e.g. the assistant message ID or a
            composite turn key).
    """

    rating: str = Field(
        ...,
        pattern=r"^(thumbs_up|thumbs_down)$",
        description="thumbs_up or thumbs_down",
    )
    comment: str = Field(
        default="",
        max_length=2000,
        description="Optional free-text feedback comment",
    )
    conversation_id: str = Field(
        default="",
        max_length=256,
        description="Identifier linking feedback to a specific conversation turn",
    )


class FeedbackEntry(BaseModel):
    """A stored feedback entry returned by the API."""

    id: str = Field(..., description="Unique feedback entry identifier")
    rating: str = Field(..., description="thumbs_up or thumbs_down")
    comment: str = Field(default="", description="Optional free-text comment")
    conversation_id: str = Field(default="", description="Linked conversation turn ID")
    user_id: str = Field(..., description="User who submitted the feedback")
    created_at: str = Field(
        ..., description="ISO-8601 timestamp when feedback was submitted"
    )


class FeedbackResponse(BaseModel):
    """Response returned after successfully submitting feedback."""

    ok: bool = Field(default=True)
    feedback_id: str = Field(..., description="ID of the stored feedback entry")


class FeedbackListResponse(BaseModel):
    """Response containing a user's feedback history."""

    feedback_entries: list[FeedbackEntry] = Field(
        default_factory=list,
        description="List of feedback entries, newest first",
    )
    total: int = Field(default=0, description="Total number of feedback entries")
