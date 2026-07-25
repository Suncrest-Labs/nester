from app.routers import natural_language_goal

app.include_router(natural_language_goal.router, prefix="/api/v1", tags=["goals"])