import logging
import re
import time
import uuid
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request, Response
from slowapi import Limiter, _rate_limit_exceeded_handler
from slowapi.errors import RateLimitExceeded
from slowapi.util import get_remote_address

from app.routers import (
    analyze,
    chat,
    coaching,
    deterioration,
    feedback,
    health,
    nudges,
    optimize,
    rebalance,
    recommendations,
    savings,
    tool_actions,
    trusted,
    ws_chat,
)
