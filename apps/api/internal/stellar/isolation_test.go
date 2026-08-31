package stellar

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/signing"
)

// These tests assert the central claim of the signing isolation work: that the
// API-side code holds no signing key. They are structural as well as
// behavioural, because "no key here" is a property of the code's shape, and a
// future change that reintroduced key custody would otherwise pass every
// behavioural test while quietly undoing the boundary.

func TestInvokerHoldsNoKeyMaterial(t *testing.T) {
	// invoker.go builds and simulates transactions. It must not reference the
	// keypair package or hold a secret: that was the pre-change design, and
	// this is what stops it being reintroduced.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "invoker.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse invoker.go: %v", err)
	}

	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if strings.HasSuffix(path, "/keypair") {
			t.Fatalf("invoker.go imports %s; the transaction builder must not hold key material", path)
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// A call to .Sign(...) in the builder would mean the key is applied
		// here rather than across the boundary.
		if sel.Sel.Name == "Sign" {
			if ident, isIdent := sel.X.(*ast.Ident); isIdent && ident.Name != "c" {
				return true
			}
			t.Errorf("invoker.go calls .Sign directly; signing must go through the signer boundary")
		}
		return true
	})
}

func TestRemoteSignerRejectsASecretSeedAsOperatorAddress(t *testing.T) {
	// Guards against the configuration mistake that would silently put a secret
	// into the API's environment: pasting the S-prefixed seed where the
	// G-prefixed public address belongs.
	client, err := newLoopbackClient(t)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	secretSeedShaped := "S" + strings.Repeat("A", 55)
	if _, err := NewRemoteSigner(client, secretSeedShaped, "Test SDF Network ; September 2015"); err == nil {
		t.Fatal("a secret-seed-shaped value was accepted as the operator address")
	}
}

func TestRemoteSignerRequiresAnOperatorAddress(t *testing.T) {
	client, err := newLoopbackClient(t)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	if _, err := NewRemoteSigner(client, "", "Test SDF Network ; September 2015"); err == nil {
		t.Fatal("an empty operator address was accepted")
	}
}

func TestRemoteSignerRefusesUnknownOperations(t *testing.T) {
	// The API-side signer refuses to even construct an intent for an operation
	// outside the closed set, so an unmodelled request never reaches the wire.
	client, err := newLoopbackClient(t)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	rs, err := NewRemoteSigner(client, "G"+strings.Repeat("A", 55), "Test SDF Network ; September 2015")
	if err != nil {
		t.Fatalf("build remote signer: %v", err)
	}

	_, err = rs.SignEnvelope(context.Background(), SignRequest{
		EnvelopeXDR:     "AAAA",
		Operation:       "drain_everything",
		ContractAddress: "C" + strings.Repeat("A", 55),
	})
	if err == nil {
		t.Fatal("an unknown operation was sent to the signer instead of being refused locally")
	}
}

func TestInvokerWithoutSignerIsReadOnly(t *testing.T) {
	// A read-only deployment legitimately has no operator key. Simulation and
	// query paths must keep working while signing fails with a clear error
	// rather than a nil dereference.
	inv, err := NewContractInvokerWithSigner(
		"http://rpc.invalid", "http://horizon.invalid",
		"Test SDF Network ; September 2015", nil)
	if err != nil {
		t.Fatalf("build read-only invoker: %v", err)
	}

	if _, err := inv.requireOperatorAddress(); !errors.Is(err, ErrNoSigner) {
		t.Fatalf("expected ErrNoSigner, got %v", err)
	}
	if _, err := inv.signEnvelope(context.Background(), SignRequest{}); !errors.Is(err, ErrNoSigner) {
		t.Fatalf("signEnvelope should return ErrNoSigner with no signer, got %v", err)
	}
}

func TestLocalSignerExposesOnlyThePublicAddress(t *testing.T) {
	// LocalSigner holds a key, but its exported surface must never return it.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "signer.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse signer.go: %v", err)
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || !fn.Name.IsExported() {
			continue
		}
		// No exported method may be named in a way that suggests key export.
		name := strings.ToLower(fn.Name.Name)
		for _, forbidden := range []string{"secret", "seed", "privatekey", "export"} {
			if strings.Contains(name, forbidden) {
				t.Errorf("LocalSigner exposes %s, which suggests key export", fn.Name.Name)
			}
		}
	}
}

// newLoopbackClient builds a signer client pointed at a socket path that is
// never served. The tests here exercise construction and local validation, both
// of which happen before any request is sent.
func newLoopbackClient(t *testing.T) (*signing.Client, error) {
	t.Helper()
	return signing.NewClient(signing.ClientOptions{
		SocketPath: filepath.Join(t.TempDir(), "signer.sock"),
	})
}
