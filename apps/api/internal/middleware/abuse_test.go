package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type abuseRecorder struct{ events []AbuseEvent }

func (r *abuseRecorder) ObserveAbuse(event AbuseEvent) { r.events = append(r.events, event) }

func testProtector() (*AbuseProtector, *time.Time, *abuseRecorder) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	observer := &abuseRecorder{}
	p := NewAbuseProtector(AbuseConfig{
		Window: time.Minute, EscalationTTL: 30 * time.Second,
		GlobalFailures: 4, EnumerationProbes: 3, BotVelocity: 3,
	}, observer)
	p.now = func() time.Time { return now }
	return p, &now, observer
}

func TestDistributedCredentialStuffingTriggersAggregateChallenge(t *testing.T) {
	p, _, observer := testProtector()
	for _, fingerprint := range []string{"ip-a", "ip-b", "ip-c"} {
		if got := p.RecordFailedAuth("/auth/login", fingerprint); got != AbuseAllow {
			t.Fatalf("early action = %s", got)
		}
	}
	if got := p.RecordFailedAuth("/auth/login", "ip-d"); got != AbuseChallenge {
		t.Fatalf("aggregate action = %s, want challenge", got)
	}
	if observer.events[len(observer.events)-1].Kind != "credential_stuffing" {
		t.Fatal("aggregate abuse was not observable")
	}
}

func TestEnumerationUsesUniformResponseAndTriggersChallenge(t *testing.T) {
	p, _, _ := testProtector()
	for _, resource := range []string{"one@example.com", "two@example.com"} {
		if got := p.RecordProbe("/account/check", resource, "scanner"); got != AbuseAllow {
			t.Fatalf("early probe = %s", got)
		}
	}
	if got := p.RecordProbe("/account/check", "three@example.com", "scanner"); got != AbuseChallenge {
		t.Fatalf("probe action = %s", got)
	}
	a, b := httptest.NewRecorder(), httptest.NewRecorder()
	WriteUniformLookupResponse(a)
	WriteUniformLookupResponse(b)
	if a.Code != b.Code || a.Body.String() != b.Body.String() {
		t.Fatal("exists/not-exists lookup responses differ")
	}
}

func TestBotVelocityChallengesWhileNormalSignupPasses(t *testing.T) {
	p, _, _ := testProtector()
	if p.RecordSensitiveFlow("/signup", "human-a") != AbuseAllow {
		t.Fatal("normal signup challenged")
	}
	p.RecordSensitiveFlow("/signup", "bot-shared")
	p.RecordSensitiveFlow("/signup", "bot-shared")
	if p.RecordSensitiveFlow("/signup", "bot-shared") != AbuseChallenge {
		t.Fatal("bot pattern did not trigger challenge")
	}
}

func TestAdaptiveEscalationRelaxesAndChallengeIsRecoverable(t *testing.T) {
	p, now, _ := testProtector()
	for range 4 {
		p.RecordFailedAuth("/auth/login", "distributed")
	}
	if p.RecordSensitiveFlow("/auth/login", "legitimate") != AbuseChallenge {
		t.Fatal("endpoint did not tighten under attack")
	}
	*now = now.Add(31 * time.Second)
	if p.RecordSensitiveFlow("/auth/login", "legitimate") != AbuseAllow {
		t.Fatal("endpoint did not relax after escalation TTL")
	}

	handler := ChallengeMiddleware(func(*http.Request) AbuseAction { return AbuseChallenge })(ok200)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/signup", nil))
	if rec.Code != http.StatusPreconditionRequired || rec.Header().Get("X-Abuse-Action") != "challenge" {
		t.Fatal("graduated response did not expose a recoverable challenge")
	}
}

func TestEscalationSurvivesShorterDetectionWindow(t *testing.T) {
	p, now, _ := testProtector()
	p.cfg.Window = time.Second
	p.cfg.EscalationTTL = time.Minute
	for range 4 {
		p.RecordFailedAuth("/auth/login", "distributed")
	}
	*now = now.Add(2 * time.Second)
	if p.RecordSensitiveFlow("/auth/login", "legitimate") != AbuseChallenge {
		t.Fatal("window rotation erased an unexpired escalation")
	}
}
