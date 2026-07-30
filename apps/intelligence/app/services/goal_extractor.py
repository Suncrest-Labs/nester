"""Natural Language Goal Extraction Service."""

import logging
import re
from datetime import datetime
from typing import Optional

import pytz
from pydantic import BaseModel, Field

from app.services.claude import get_client, get_model_id

logger = logging.getLogger(__name__)


class ExtractedGoal(BaseModel):
    name: str = Field(description="Short descriptive name for the goal")
    target_amount: float = Field(description="Target amount in USDC")
    deadline: str = Field(description="ISO 8601 date string (YYYY-MM-DD)")
    category: str = Field(
        description=(
            "Goal category: savings, vacation, emergency, education, car, home, investment, other"
        )
    )
    initial_deposit: Optional[float] = Field(default=0, description="Optional initial deposit")
    is_recurring: bool = Field(default=False, description="Whether this is a recurring plan")
    recurring_amount: Optional[float] = Field(default=None, description="Monthly recurring amount")


class AmbiguityResponse(BaseModel):
    is_ambiguous: bool = True
    message: str
    missing_fields: list[str]


class GoalExtractionResult(BaseModel):
    success: bool
    extracted: Optional[ExtractedGoal] = None
    ambiguity: Optional[AmbiguityResponse] = None
    error: Optional[str] = None


class GoalExtractor:
    CATEGORIES = [
        "savings",
        "vacation",
        "emergency",
        "education",
        "car",
        "home",
        "investment",
        "other",
    ]

    def __init__(self):
        self.client = get_client()
        self.model = get_model_id()

    def extract(self, user_message: str, user_timezone: str = "UTC") -> GoalExtractionResult:
        if self._check_injection(user_message):
            return GoalExtractionResult(success=False, error="Invalid input detected.")

        try:
            response = self.client.messages.create(
                model=self.model,
                max_tokens=1024,
                messages=[{"role": "user", "content": self._build_extraction_prompt(user_message)}],
                tools=[{"name": "extract_goal", "input_schema": ExtractedGoal.model_json_schema()}],
                tool_choice={"type": "tool", "name": "extract_goal"},
            )

            for content in response.content:
                if content.type == "tool_use" and content.name == "extract_goal":
                    extracted = ExtractedGoal(**content.input)
                    return self._validate_and_resolve(extracted, user_timezone)

            return GoalExtractionResult(success=False, error="Failed to extract goal.")

        except Exception as e:
            logger.error(f"Extraction error: {e}")
            return GoalExtractionResult(success=False, error="Unable to process request.")

    def _check_injection(self, message: str) -> bool:
        patterns = [
            r"ignore (?:the|all) (?:previous|above) (?:instructions?|prompt)",
            r"(?:system|developer|assistant).*(?:prompt|instruction)",
            r"you are (?:now|not)",
            r"pretend (?:you|to be)",
            r"role[- ]?play",
            r"act as",
            r"forget (?:all|previous|above)",
            r"disregard",
            r"do not (?:follow|obey|listen to)",
            r"you must (?:now|not)",
            r"new rules?",
            r"override",
        ]
        lower_msg = message.lower()
        for pattern in patterns:
            if re.search(pattern, lower_msg):
                return True
        return False

    def _build_extraction_prompt(self, message: str) -> str:
        categories = ", ".join(self.CATEGORIES)
        return f"""Extract a structured savings goal from the user's message.
Current date: {datetime.now().strftime("%Y-%m-%d")}
User message: "{message}"
Extract: name, target_amount (USDC), deadline (YYYY-MM-DD), category [{categories}],
initial_deposit, is_recurring, recurring_amount.
Rules: Do NOT guess missing fields. Return ONLY the structured extraction."""

    def _validate_and_resolve(
        self, extracted: ExtractedGoal, user_timezone: str = "UTC"
    ) -> GoalExtractionResult:
        if extracted.target_amount <= 0:
            return GoalExtractionResult(
                success=False,
                ambiguity=AmbiguityResponse(
                    is_ambiguous=True,
                    message="Please specify a target amount.",
                    missing_fields=["target_amount"],
                ),
            )

        try:
            # Use user's timezone
            try:
                tz = pytz.timezone(user_timezone)
            except pytz.UnknownTimeZoneError:
                tz = pytz.UTC

            now = datetime.now(tz)
            deadline_date = datetime.fromisoformat(extracted.deadline).replace(tzinfo=tz)

            if deadline_date < now:
                return GoalExtractionResult(
                    success=False,
                    ambiguity=AmbiguityResponse(
                        is_ambiguous=True,
                        message=f"The date {extracted.deadline} is in the past.",
                        missing_fields=["deadline"],
                    ),
                )
        except ValueError:
            return GoalExtractionResult(
                success=False,
                ambiguity=AmbiguityResponse(
                    is_ambiguous=True,
                    message=f"Could not understand the date '{extracted.deadline}'.",
                    missing_fields=["deadline"],
                ),
            )

        if extracted.category not in self.CATEGORIES:
            return GoalExtractionResult(
                success=False,
                ambiguity=AmbiguityResponse(
                    is_ambiguous=True,
                    message=f"Unknown category '{extracted.category}'.",
                    missing_fields=["category"],
                ),
            )

        if not extracted.name or len(extracted.name) < 2:
            return GoalExtractionResult(
                success=False,
                ambiguity=AmbiguityResponse(
                    is_ambiguous=True,
                    message="Please provide a name for your goal.",
                    missing_fields=["name"],
                ),
            )

        if extracted.initial_deposit is not None and extracted.initial_deposit < 0:
            return GoalExtractionResult(success=False, error="Initial deposit cannot be negative.")

        if extracted.is_recurring and extracted.recurring_amount is None:
            return GoalExtractionResult(
                success=False,
                ambiguity=AmbiguityResponse(
                    is_ambiguous=True,
                    message="How much per month?",
                    missing_fields=["recurring_amount"],
                ),
            )

        if (
            extracted.is_recurring
            and extracted.recurring_amount is not None
            and extracted.recurring_amount <= 0
        ):
            return GoalExtractionResult(success=False, error="Recurring amount must be positive.")

        return GoalExtractionResult(success=True, extracted=extracted)
