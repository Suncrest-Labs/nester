package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// setupAdminDB opens a DB, creates the minimal schema, resets tables, and
// returns ready-to-use repositories plus a cleanup function.
func setupAdminDB(t *testing.T) (*AdminRepository, *UserRepository, func()) {
	t.Helper()
	db := openIntegrationDB(t)

	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            UUID        PRIMARY KEY,
			wallet_address TEXT       NOT NULL UNIQUE,
			display_name  TEXT        NOT NULL DEFAULT '',
			kyc_status    TEXT        NOT NULL DEFAULT 'pending',
			tier          VARCHAR(20) NOT NULL DEFAULT 'standard',
			last_login_at TIMESTAMPTZ,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS user_roles (
			user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role       VARCHAR(50) NOT NULL,
			granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			granted_by UUID        REFERENCES users(id),
			PRIMARY KEY (user_id, role)
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema setup: %v", err)
		}
	}

	cleanup := func() {
		if _, err := db.Exec(`TRUNCATE TABLE user_roles, users RESTART IDENTITY CASCADE`); err != nil {
			t.Logf("cleanup truncate: %v", err)
		}
	}
	cleanup() // start clean

	return NewAdminRepository(db), NewUserRepository(db), cleanup
}

// seedAdminUser inserts a bare user row and returns the new ID.
func seedAdminUser(t *testing.T, db *sql.DB, walletAddress string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO users (id, wallet_address, display_name) VALUES ($1, $2, $3)`,
		id, walletAddress, "Test User",
	); err != nil {
		t.Fatalf("seedAdminUser: %v", err)
	}
	return id
}

// directGrantRole bypasses the last-admin guard for test setup.
func directGrantRole(t *testing.T, db *sql.DB, userID uuid.UUID, role string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO user_roles (user_id, role) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, role,
	); err != nil {
		t.Fatalf("directGrantRole: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGetUserByWalletAddress_success(t *testing.T) {
	adminRepo, _, cleanup := setupAdminDB(t)
	defer cleanup()

	wallet := fmt.Sprintf("G%055d", 1)
	id := seedAdminUser(t, adminRepo.db, wallet)

	u, err := adminRepo.GetUserByWalletAddress(context.Background(), wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != id {
		t.Errorf("expected ID %v, got %v", id, u.ID)
	}
	if u.WalletAddress != wallet {
		t.Errorf("expected wallet %q, got %q", wallet, u.WalletAddress)
	}
}

func TestGetUserByWalletAddress_notFound(t *testing.T) {
	adminRepo, _, cleanup := setupAdminDB(t)
	defer cleanup()

	_, err := adminRepo.GetUserByWalletAddress(context.Background(), "GNONEXISTENT")
	if err == nil {
		t.Fatal("expected error for unknown wallet, got nil")
	}
}

func TestGrantAdminRole_success(t *testing.T) {
	adminRepo, userRepo, cleanup := setupAdminDB(t)
	defer cleanup()

	grantor := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 2))
	target := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 3))

	if err := adminRepo.GrantAdminRole(context.Background(), target, grantor); err != nil {
		t.Fatalf("GrantAdminRole() error = %v", err)
	}

	roles, err := userRepo.GetRoles(context.Background(), target)
	if err != nil {
		t.Fatalf("GetRoles() error = %v", err)
	}
	if len(roles) != 1 || roles[0] != "admin" {
		t.Errorf("expected [admin], got %v", roles)
	}
}

func TestGrantAdminRole_alreadyAdmin(t *testing.T) {
	adminRepo, userRepo, cleanup := setupAdminDB(t)
	defer cleanup()

	grantor := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 4))
	target := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 5))

	if err := adminRepo.GrantAdminRole(context.Background(), target, grantor); err != nil {
		t.Fatalf("first GrantAdminRole() error = %v", err)
	}
	// Second grant must be idempotent — no error.
	if err := adminRepo.GrantAdminRole(context.Background(), target, grantor); err != nil {
		t.Fatalf("second GrantAdminRole() (idempotent) error = %v", err)
	}

	roles, err := userRepo.GetRoles(context.Background(), target)
	if err != nil {
		t.Fatalf("GetRoles() error = %v", err)
	}
	if len(roles) != 1 {
		t.Errorf("expected exactly 1 admin role, got %v", roles)
	}
}

func TestRevokeAdminRole_success(t *testing.T) {
	adminRepo, userRepo, cleanup := setupAdminDB(t)
	defer cleanup()

	admin1 := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 6))
	admin2 := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 7))
	directGrantRole(t, adminRepo.db, admin1, "admin")
	directGrantRole(t, adminRepo.db, admin2, "admin")

	if err := adminRepo.RevokeAdminRole(context.Background(), admin2); err != nil {
		t.Fatalf("RevokeAdminRole() error = %v", err)
	}

	roles, err := userRepo.GetRoles(context.Background(), admin2)
	if err != nil {
		t.Fatalf("GetRoles() error = %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("expected no roles after revoke, got %v", roles)
	}
}

func TestRevokeAdminRole_lastAdmin(t *testing.T) {
	adminRepo, _, cleanup := setupAdminDB(t)
	defer cleanup()

	only := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 8))
	directGrantRole(t, adminRepo.db, only, "admin")

	if err := adminRepo.RevokeAdminRole(context.Background(), only); err == nil {
		t.Fatal("expected error when revoking last admin, got nil")
	}
}

func TestListAdminUsers(t *testing.T) {
	adminRepo, _, cleanup := setupAdminDB(t)
	defer cleanup()

	u1 := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 9))
	u2 := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 10))
	_ = seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 11)) // non-admin
	directGrantRole(t, adminRepo.db, u1, "admin")
	directGrantRole(t, adminRepo.db, u2, "admin")

	admins, err := adminRepo.ListAdminUsers(context.Background())
	if err != nil {
		t.Fatalf("ListAdminUsers() error = %v", err)
	}
	if len(admins) != 2 {
		t.Errorf("expected 2 admin users, got %d", len(admins))
	}
	ids := map[uuid.UUID]bool{u1: true, u2: true}
	for _, a := range admins {
		if !ids[a.ID] {
			t.Errorf("unexpected admin user ID %v", a.ID)
		}
	}
}

func TestGetRoles_returnsCorrectRoles(t *testing.T) {
	adminRepo, userRepo, cleanup := setupAdminDB(t)
	defer cleanup()

	u := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 12))
	directGrantRole(t, adminRepo.db, u, "admin")
	directGrantRole(t, adminRepo.db, u, "operator")

	roles, err := userRepo.GetRoles(context.Background(), u)
	if err != nil {
		t.Fatalf("GetRoles() error = %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %v", roles)
	}
	// ORDER BY role → alphabetical.
	if roles[0] != "admin" || roles[1] != "operator" {
		t.Errorf("expected [admin operator], got %v", roles)
	}
}

func TestGetRoles_userWithNoRoles(t *testing.T) {
	adminRepo, userRepo, cleanup := setupAdminDB(t)
	defer cleanup()

	u := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 13))

	roles, err := userRepo.GetRoles(context.Background(), u)
	if err != nil {
		t.Fatalf("GetRoles() error = %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("expected empty roles, got %v", roles)
	}
}

// TestGetRoles_passesUUIDNotString is a regression test for PR #270.
// GetRoles must pass the native uuid.UUID to pgx (not id.String()) so that
// pgx sends a binary UUID parameter without requiring an implicit server-side
// text→uuid cast. We verify the query returns the correct row when the
// parameter is a uuid.UUID value, and that a direct native-UUID query also
// matches — confirming no implicit cast is needed.
func TestGetRoles_passesUUIDNotString(t *testing.T) {
	adminRepo, userRepo, cleanup := setupAdminDB(t)
	defer cleanup()

	u := seedAdminUser(t, adminRepo.db, fmt.Sprintf("G%055d", 14))
	directGrantRole(t, adminRepo.db, u, "admin")

	// GetRoles now passes uuid.UUID directly (the fix).
	roles, err := userRepo.GetRoles(context.Background(), u)
	if err != nil {
		t.Fatalf("GetRoles() with uuid.UUID parameter error = %v", err)
	}
	if len(roles) != 1 || roles[0] != "admin" {
		t.Errorf("expected [admin], got %v", roles)
	}

	// Confirm the native UUID parameter matches the row without any cast.
	var count int
	if err := adminRepo.db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM user_roles WHERE user_id = $1 AND role = 'admin'`,
		u, // uuid.UUID — pgx sends as binary, no implicit cast
	).Scan(&count); err != nil {
		t.Fatalf("direct uuid.UUID query error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row with native UUID param, got %d", count)
	}
}
