"""JWT authentication dependency for all protected intelligence endpoints."""

from typing import Any, Optional

import jwt
from fastapi import Depends, HTTPException, Request, status
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from app.config import settings

# auto_error=False so missing/invalid auth returns None instead of 403,
# letting the dev-mode bypass below handle it gracefully.
_bearer = HTTPBearer(auto_error=False)


def verify_jwt(
    request: Request,
    credentials: Optional[HTTPAuthorizationCredentials] = Depends(_bearer),
) -> dict[str, Any]:
    """Validate a Bearer JWT and return its claims.

    Dev-mode bypass: when INTELLIGENCE_JWT_SECRET is not configured the
    endpoint is open and the user ID is taken from the `userId` query param.
    This lets EventSource connections work without a custom auth header.

    In production (secret configured) a valid signed Bearer token is required.
    """
    if not settings.jwt_secret:
        user_id = request.query_params.get("userId", "anonymous")
        return {"sub": user_id}

    if credentials is None:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Authorization header required",
            headers={"WWW-Authenticate": "Bearer"},
        )

    try:
        claims: dict[str, Any] = jwt.decode(
            credentials.credentials,
            settings.jwt_secret,
            algorithms=["HS256"],
        )
    except jwt.ExpiredSignatureError:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Token has expired",
            headers={"WWW-Authenticate": "Bearer"},
        )
    except jwt.InvalidTokenError:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid token",
            headers={"WWW-Authenticate": "Bearer"},
        )
    return claims
