package service_test

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/bankaccount"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type memBankAccountRepo struct {
	byID map[uuid.UUID]storedAccount
}

type storedAccount struct {
	account   bankaccount.BankAccount
	encrypted []byte
	print     string
}

func newMemBankAccountRepo() *memBankAccountRepo {
	return &memBankAccountRepo{byID: make(map[uuid.UUID]storedAccount)}
}

func (m *memBankAccountRepo) Create(_ context.Context, account bankaccount.BankAccount, encrypted []byte, fingerprint string) (bankaccount.BankAccount, error) {
	for _, s := range m.byID {
		if s.account.UserID == account.UserID && s.print == fingerprint {
			return bankaccount.BankAccount{}, bankaccount.ErrDuplicateAccount
		}
	}
	account.CreatedAt = time.Now().UTC()
	m.byID[account.ID] = storedAccount{account: account, encrypted: encrypted, print: fingerprint}
	return account, nil
}

func (m *memBankAccountRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]bankaccount.BankAccount, error) {
	var out []bankaccount.BankAccount
	for _, s := range m.byID {
		if s.account.UserID == userID {
			out = append(out, s.account)
		}
	}
	return out, nil
}

func (m *memBankAccountRepo) GetByID(_ context.Context, id uuid.UUID) (bankaccount.BankAccount, []byte, error) {
	s, ok := m.byID[id]
	if !ok {
		return bankaccount.BankAccount{}, nil, bankaccount.ErrNotFound
	}
	return s.account, s.encrypted, nil
}

func (m *memBankAccountRepo) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := m.byID[id]; !ok {
		return bankaccount.ErrNotFound
	}
	delete(m.byID, id)
	return nil
}

func (m *memBankAccountRepo) ClearDefaultForCurrency(_ context.Context, userID uuid.UUID, currency string) error {
	for id, s := range m.byID {
		if s.account.UserID == userID && s.account.Currency == currency {
			s.account.IsDefault = false
			m.byID[id] = s
		}
	}
	return nil
}

func (m *memBankAccountRepo) SetDefault(_ context.Context, id uuid.UUID, userID uuid.UUID) error {
	s, ok := m.byID[id]
	if !ok || s.account.UserID != userID {
		return bankaccount.ErrNotFound
	}
	s.account.IsDefault = true
	m.byID[id] = s
	return nil
}

func testCipher(t *testing.T) *crypto.AccountCipher {
	t.Helper()
	c, err := crypto.NewAccountCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	return c
}

func TestBankAccountService_AddListRemove(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemBankAccountRepo()
	svc := service.NewBankAccountService(repo, testCipher(t), nil)

	created, err := svc.Add(ctx, userID, bankaccount.AddInput{
		BankName:      "GTBank",
		BankCode:      "058",
		AccountNumber: "0123456789",
		AccountName:   "JOHN DOE",
		Currency:      "NGN",
		Country:       "NG",
		SetAsDefault:  true,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !created.IsDefault {
		t.Error("expected default account")
	}

	list, err := svc.List(ctx, userID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}

	if err := svc.Remove(ctx, userID, created.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	list, _ = svc.List(ctx, userID)
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestBankAccountService_DefaultToggling(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemBankAccountRepo()
	svc := service.NewBankAccountService(repo, testCipher(t), nil)

	a1, _ := svc.Add(ctx, userID, bankaccount.AddInput{
		BankName: "GTBank", BankCode: "058", AccountNumber: "0123456789",
		AccountName: "A", Currency: "NGN", Country: "NG", SetAsDefault: true,
	})
	a2, _ := svc.Add(ctx, userID, bankaccount.AddInput{
		BankName: "Access", BankCode: "044", AccountNumber: "0987654321",
		AccountName: "B", Currency: "NGN", Country: "NG",
	})

	updated, err := svc.SetDefault(ctx, userID, a2.ID)
	if err != nil {
		t.Fatalf("set default: %v", err)
	}
	if !updated.IsDefault {
		t.Error("a2 should be default")
	}

	list, _ := svc.List(ctx, userID)
	var defaults int
	for _, item := range list {
		if item.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("expected exactly one default, got %d", defaults)
	}
	_ = a1
}

func TestBankAccountService_DuplicateRejected(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemBankAccountRepo()
	svc := service.NewBankAccountService(repo, testCipher(t), nil)

	input := bankaccount.AddInput{
		BankName: "GTBank", BankCode: "058", AccountNumber: "0123456789",
		AccountName: "A", Currency: "NGN", Country: "NG",
	}
	if _, err := svc.Add(ctx, userID, input); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := svc.Add(ctx, userID, input); err != bankaccount.ErrDuplicateAccount {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}
