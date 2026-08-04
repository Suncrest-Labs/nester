from fastapi.testclient import TestClient

from app.main import app


def test_request_id_is_echoed_and_logged() -> None:
    client = TestClient(app)
    response = client.get("/health", headers={"X-Request-ID": "req-123"})

    assert response.status_code == 200
    assert response.headers["X-Request-ID"] == "req-123"
    assert response.json() == {"status": "ok"}
