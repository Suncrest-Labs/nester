"""
Natural Language Goal Extraction Service

Turns user messages like "I want to save 500k for a car by next March"
into structured goal fields using Claude's structured output capability.
"""

import re
from datetime import datetime
from typing import Optional
from pydantic import BaseModel, Field

from app.services.claude import get_client, get_model_id


class ExtractedGoal(BaseModel):
    """Structured goal extracted from natural language"""
    name: str = Field(description="Short descriptive name for the goal")
    target_amount: float = Field(description="Target amount in USDC")
    deadline: str = Field(description="ISO 8601 date string (YYYY-MM-DD)")
    category: str = Field(
        description="Goal category: savings, vacation, emergency, education, car, home, investment, other"
    )
    initial_deposit: Optional[float] = Field(
        default=0, description="Optional initial deposit amount"
    )
    is_recurring: bool = Field(
        default=False, description="Whether this is a recurring savings plan"
    )
    recurring_amount: Optional[float] = Field(
        default=None, description="Monthly recurring amount if applicable"
    )

    class Config:
        json_schema_extra = {
            "example": {
                "name": "Car Savings",
                "target_amount": 500000,
                "deadline": "2026-03-31",
                "category": "car",
                "initial_deposit": 10000,
                "is_recurring": False,
                "recurring_amount": None
            }
        }


class AmbiguityResponse(BaseModel):
    """Response when the extraction has ambiguities"""
    is_ambiguous: bool = True
    message: str
    missing_fields: list[str]


class GoalExtractionResult(BaseModel):
    """Complete extraction result"""
    success: bool
    extracted: Optional[ExtractedGoal] = None
    ambiguity: Optional[AmbiguityResponse] = None
    error: Optional[str] = None


class GoalExtractor:
    """Extracts structured savings goals from natural language using Claude"""

    CATEGORIES = [
        "savings", "vacation", "emergency", "education",
        "car", "home", "investment", "other"
    ]

    def __init__(self):
        self.client = get_client()
        self.model = get_model_id()

    def extract(self, user_message: str, user_timezone: str = "UTC") -> GoalExtractionResult:
        """
        Extract structured goal from a natural language message

        Args:
            user_message: The user's natural language description
            user_timezone: User's timezone for date resolution

        Returns:
            GoalExtractionResult with either extracted goal or ambiguity
        """
        # First, check for prompt injection
        injection_check = self._check_injection(user_message)
        if injection_check:
            return GoalExtractionResult(
                success=False,
                error="Invalid input detected. Please provide a valid goal description."
            )

        # Build the extraction prompt
        prompt = self._build_extraction_prompt(user_message, user_timezone)

        try:
            response = self.client.messages.create(
                model=self.model,
                max_tokens=1024,
                messages=[
                    {"role": "user", "content": prompt}
                ],
                tools=[{
                    "name": "extract_goal",
                    "description": "Extract structured savings goal from natural language",
                    "input_schema": ExtractedGoal.model_json_schema()
                }],
                tool_choice={"type": "tool", "name": "extract_goal"}
            )

            # Parse the tool call response
            for content in response.content:
                if content.type == "tool_use" and content.name == "extract_goal":
                    extracted = ExtractedGoal(**content.input)
                    return self._validate_and_resolve(extracted, user_timezone)

            return GoalExtractionResult(
                success=False,
                error="Failed to extract goal. Please try rephrasing."
            )

        except Exception as e:
            return GoalExtractionResult(
                success=False,
                error=f"Extraction error: {str(e)}"
            )

    def _check_injection(self, message: str) -> bool:
        """Check for prompt injection attempts"""
        injection_patterns = [
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

        # Check for base64 encoded content
        base64_pattern = r'[A-Za-z0-9+/]{30,}={0,2}'
        if re.search(base64_pattern, message):
            return True

        # Check for injection keywords
        lower_msg = message.lower()
        for pattern in injection_patterns:
            if re.search(pattern, lower_msg):
                return True

        return False

    def _build_extraction_prompt(self, message: str, timezone: str) -> str:
        """Build the extraction prompt"""
        categories_str = ", ".join(self.CATEGORIES)
        return f"""Extract a structured savings goal from the user's message.

Current date and timezone: {datetime.now().strftime('%Y-%m-%d')} ({timezone})

User message: "{message}"

Extract the following fields:
- name: A short, descriptive name for the goal
- target_amount: The target amount in USDC (always positive number)
- deadline: The target date in YYYY-MM-DD format (must be a future date)
- category: One of [{categories_str}]
- initial_deposit: Optional initial deposit amount (default 0)
- is_recurring: Whether this is a recurring savings plan
- recurring_amount: Monthly recurring amount if applicable

IMPORTANT RULES:
1. If ANY field is missing or ambiguous, DO NOT guess. Set is_ambiguous=true and explain what's missing.
2. For deadlines:
   - "next March" = March of the next year if current month >= March
   - "in 6 months" = current date + 6 months
   - "by year end" = December 31 of current year
   - Always validate the date is in the future
3. For amounts:
   - "500k" = 500,000
   - "500,000" = 500,000
   - "half a million" = 500,000
   - Always return a positive number
4. Category defaults to "savings" if not specified
5. Do NOT invent information the user didn't provide

Return ONLY the structured extraction."""

    def _validate_and_resolve(
        self, extracted: ExtractedGoal, timezone: str
    ) -> GoalExtractionResult:
        """Validate and resolve dates and amounts"""
        # Validate amount
        if extracted.target_amount <= 0:
            return GoalExtractionResult(
                success=False,
                error="Target amount must be a positive number"
            )

        # Validate and resolve date
        try:
            deadline_date = datetime.fromisoformat(extracted.deadline)
            now = datetime.now()

            # Check if date is in the past
            if deadline_date < now:
                return GoalExtractionResult(
                    success=False,
                    ambiguity=AmbiguityResponse(
                        is_ambiguous=True,
                        message=(
                            f"The date {extracted.deadline} is in the past. "
                            "Did you mean next year?"
                        ),
                        missing_fields=["deadline"]
                    )
                )
        except ValueError:
            return GoalExtractionResult(
                success=False,
                ambiguity=AmbiguityResponse(
                    is_ambiguous=True,
                    message=(
                        f"Could not understand the date '{extracted.deadline}'. "
                        "Please specify a valid date."
                    ),
                    missing_fields=["deadline"]
                )
            )

        # Validate category
        if extracted.category not in self.CATEGORIES:
            extracted.category = "savings"

        # Check for missing fields that should be confirmed
        missing_fields = []
        if not extracted.name or len(extracted.name) < 2:
            missing_fields.append("name")

        if missing_fields:
            return GoalExtractionResult(
                success=False,
                ambiguity=AmbiguityResponse(
                    is_ambiguous=True,
                    message=f"Please provide: {', '.join(missing_fields)}",
                    missing_fields=missing_fields
                )
            )

        return GoalExtractionResult(
            success=True,
            extracted=extracted
        )
