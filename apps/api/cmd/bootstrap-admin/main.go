// bootstrap-admin grants the admin role to a user identified by wallet address.
// Run once to seed the first administrator after deploying a fresh database.
//
// Usage:
//
//	go run ./cmd/bootstrap-admin --wallet=G... [--dsn=postgres://...]
//
// The DSN defaults to the DATABASE_DSN environment variable.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap-admin:", err)
		os.Exit(1)
	}
}

func run() error {
	wallet := flag.String("wallet", "", "Stellar wallet address to grant admin role (required)")
	dsn := flag.String("dsn", "", "PostgreSQL DSN (defaults to DATABASE_DSN env var)")
	flag.Parse()

	if *wallet == "" {
		flag.Usage()
		return fmt.Errorf("--wallet is required")
	}

	_ = godotenv.Load()
	if *dsn == "" {
		*dsn = os.Getenv("DATABASE_DSN")
	}
	if *dsn == "" {
		return fmt.Errorf("DATABASE_DSN environment variable or --dsn flag is required")
	}

	db, err := sql.Open("pgx", *dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	var userID string
	err = db.QueryRow(`SELECT id FROM users WHERE wallet_address = $1`, *wallet).Scan(&userID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no user found with wallet address %q", *wallet)
	}
	if err != nil {
		return fmt.Errorf("query user: %w", err)
	}

	_, err = db.Exec(
		`INSERT INTO user_roles (user_id, role) VALUES ($1, 'admin') ON CONFLICT DO NOTHING`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("insert role: %w", err)
	}

	fmt.Printf("granted admin role to user %s (wallet %s)\n", userID, *wallet)
	return nil
}
