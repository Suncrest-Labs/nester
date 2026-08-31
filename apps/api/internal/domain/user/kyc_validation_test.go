package user

import (
	"errors"
	"testing"
	"time"
)

func TestParseDateOfBirth_AcceptsAValidPastDate(t *testing.T) {
	got, err := ParseDateOfBirth("1990-05-14")
	if err != nil {
		t.Fatalf("ParseDateOfBirth() error = %v", err)
	}
	want := time.Date(1990, 5, 14, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseDateOfBirth_RejectsEmptyString(t *testing.T) {
	_, err := ParseDateOfBirth("")
	if !errors.Is(err, ErrInvalidDateOfBirth) {
		t.Fatalf("error = %v, want ErrInvalidDateOfBirth", err)
	}
}

func TestParseDateOfBirth_RejectsMalformedDate(t *testing.T) {
	cases := []string{"not-a-date", "1990/05/14", "14-05-1990", "1990-13-01", "1990-05-32"}
	for _, c := range cases {
		if _, err := ParseDateOfBirth(c); !errors.Is(err, ErrInvalidDateOfBirth) {
			t.Fatalf("ParseDateOfBirth(%q) error = %v, want ErrInvalidDateOfBirth", c, err)
		}
	}
}

func TestParseDateOfBirth_RejectsAFutureDate(t *testing.T) {
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	if _, err := ParseDateOfBirth(future); !errors.Is(err, ErrInvalidDateOfBirth) {
		t.Fatalf("ParseDateOfBirth(future) error = %v, want ErrInvalidDateOfBirth", err)
	}
}

func TestIsValidCountryCode_AcceptsRealCodes(t *testing.T) {
	for _, code := range []string{"US", "GB", "NG", "DE", "JP", "BR"} {
		if !IsValidCountryCode(code) {
			t.Fatalf("IsValidCountryCode(%q) = false, want true", code)
		}
	}
}

func TestIsValidCountryCode_RejectsUnknownOrMalformedCodes(t *testing.T) {
	cases := []string{"", "XX", "ZZ", "USA", "us", "12", "  "}
	for _, c := range cases {
		if IsValidCountryCode(c) {
			t.Fatalf("IsValidCountryCode(%q) = true, want false", c)
		}
	}
}

func TestIsValidCountryCode_IsCaseSensitiveToTheCanonicalUppercaseForm(t *testing.T) {
	if IsValidCountryCode("us") {
		t.Fatal("expected lowercase 'us' to be rejected — callers must normalize to uppercase before calling")
	}
	if !IsValidCountryCode("US") {
		t.Fatal("expected uppercase 'US' to be accepted")
	}
}
