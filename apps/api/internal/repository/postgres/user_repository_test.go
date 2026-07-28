package postgres

import (
	"testing"
)

func TestPackUnpackKYCCiphertexts_RoundTrip(t *testing.T) {
	idNum := []byte("id-number-ciphertext-here")
	frontKey := []byte("front-key-ciphertext-here")
	backKey := []byte("back-key-ciphertext-here")

	packed := packKYCCiphertexts(idNum, frontKey, backKey)
	gotIDNum, gotFrontKey, gotBackKey := UnpackKYCCiphertexts(packed)

	if string(gotIDNum) != string(idNum) {
		t.Fatalf("id_number: want %q, got %q", idNum, gotIDNum)
	}
	if string(gotFrontKey) != string(frontKey) {
		t.Fatalf("front_key: want %q, got %q", frontKey, gotFrontKey)
	}
	if string(gotBackKey) != string(backKey) {
		t.Fatalf("back_key: want %q, got %q", backKey, gotBackKey)
	}
}

func TestPackUnpackKYCCiphertexts_NilBackKey(t *testing.T) {
	idNum := []byte("id-number")
	frontKey := []byte("front-key")

	packed := packKYCCiphertexts(idNum, frontKey, nil)
	gotIDNum, gotFrontKey, gotBackKey := UnpackKYCCiphertexts(packed)

	if string(gotIDNum) != string(idNum) {
		t.Fatalf("id_number: want %q, got %q", idNum, gotIDNum)
	}
	if string(gotFrontKey) != string(frontKey) {
		t.Fatalf("front_key: want %q, got %q", frontKey, gotFrontKey)
	}
	if gotBackKey != nil {
		t.Fatalf("back_key: want nil, got %q", gotBackKey)
	}
}

func TestPackUnpackKYCCiphertexts_Empty(t *testing.T) {
	gotIDNum, gotFrontKey, gotBackKey := UnpackKYCCiphertexts(nil)
	if gotIDNum != nil || gotFrontKey != nil || gotBackKey != nil {
		t.Fatal("all should be nil for nil input")
	}

	gotIDNum, gotFrontKey, gotBackKey = UnpackKYCCiphertexts([]byte{})
	if gotIDNum != nil || gotFrontKey != nil || gotBackKey != nil {
		t.Fatal("all should be nil for empty input")
	}
}
