package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGetEffectivenessStats_QueryErrorIsReturnedNotSwallowed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewNudgeHistoryRepository(db)
	wantErr := errors.New("connection reset")
	mock.ExpectQuery(`SELECT count\(DISTINCT d\.id\), count\(DISTINCT o\.dispatch_id\)`).
		WillReturnError(wantErr)

	stats, err := repository.GetEffectivenessStats(context.Background(), "milestone", "active_saver")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if stats.HasData {
		t.Fatalf("expected HasData = false on error, got true (stats: %+v)", stats)
	}
	if stats.ConversionRate != 0 {
		t.Fatalf("expected zero-value ConversionRate on error, got %v", stats.ConversionRate)
	}
}

func TestGetEffectivenessStats_ColdStartHasNoData(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewNudgeHistoryRepository(db)
	rows := sqlmock.NewRows([]string{"count", "count"}).AddRow(0, 0)
	mock.ExpectQuery(`SELECT count\(DISTINCT d\.id\), count\(DISTINCT o\.dispatch_id\)`).
		WillReturnRows(rows)

	stats, err := repository.GetEffectivenessStats(context.Background(), "milestone", "active_saver")
	if err != nil {
		t.Fatalf("GetEffectivenessStats() error = %v, want nil", err)
	}
	if stats.HasData {
		t.Fatal("expected HasData = false for a cold-start nudge type (zero dispatches)")
	}
	if stats.ConversionRate != 0 {
		t.Fatalf("expected zero-value ConversionRate for cold start, got %v — a cold start must never read as a perfect conversion rate", stats.ConversionRate)
	}
}

func TestGetEffectivenessStats_MeasuredDataReturnsRealRate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewNudgeHistoryRepository(db)
	rows := sqlmock.NewRows([]string{"count", "count"}).AddRow(20, 5)
	mock.ExpectQuery(`SELECT count\(DISTINCT d\.id\), count\(DISTINCT o\.dispatch_id\)`).
		WillReturnRows(rows)

	stats, err := repository.GetEffectivenessStats(context.Background(), "milestone", "active_saver")
	if err != nil {
		t.Fatalf("GetEffectivenessStats() error = %v, want nil", err)
	}
	if !stats.HasData {
		t.Fatal("expected HasData = true when dispatches exist in the window")
	}
	if want := 0.25; stats.ConversionRate != want {
		t.Fatalf("ConversionRate = %v, want %v (5/20)", stats.ConversionRate, want)
	}
}

func TestGetEffectivenessStats_BindsNudgeTypeSegmentAndTimeWindow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repository := NewNudgeHistoryRepository(db)
	rows := sqlmock.NewRows([]string{"count", "count"}).AddRow(0, 0)
	mock.ExpectQuery(`SELECT count\(DISTINCT d\.id\), count\(DISTINCT o\.dispatch_id\)`).
		WithArgs("streak_milestone", "dormant", sqlmock.AnyArg()).
		WillReturnRows(rows)

	if _, err := repository.GetEffectivenessStats(context.Background(), "streak_milestone", "dormant"); err != nil {
		t.Fatalf("GetEffectivenessStats() error = %v, want nil", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
