package postgres

import (
	"testing"
)

func TestPackUnpackKYCCiphertexts_RoundTrip(t *testing.T) {
	idNum := []byte("id-number-ciphertext-here")
	frontKey := []byte("front-key-ciphertext-here")
	backKey := []byte("back-key-ciphertext-here")

	packed, err := packKYCCiphertexts(idNum, frontKey, backKey)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
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

	packed, err := packKYCCiphertexts(idNum, frontKey, nil)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
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

func TestPackKYCCiphertexts_RejectsOversizedSegment(t *testing.T) {
	// The 16-bit length framing cannot represent a segment of 64 KiB or more.
	// Truncating one would corrupt the packed blob and only surface later as an
	// unexplained decryption failure during rotation, so packing must refuse it
	// (nester#1035, G115).
	oversized := make([]byte, maxKYCSegmentLen+1)

	for name, args := range map[string][3][]byte{
		"id_number":        {oversized, []byte("front"), []byte("back")},
		"front_object_key": {[]byte("id"), oversized, []byte("back")},
		"back_object_key":  {[]byte("id"), []byte("front"), oversized},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := packKYCCiphertexts(args[0], args[1], args[2]); err == nil {
				t.Fatalf("an oversized %s segment was packed instead of rejected", name)
			}
		})
	}
}

func TestPackKYCCiphertexts_AcceptsMaximumSegment(t *testing.T) {
	// The boundary itself must still pack: the limit is inclusive.
	atLimit := make([]byte, maxKYCSegmentLen)

	packed, err := packKYCCiphertexts(atLimit, []byte("front"), []byte("back"))
	if err != nil {
		t.Fatalf("a segment at exactly the limit was rejected: %v", err)
	}
	gotIDNum, _, _ := UnpackKYCCiphertexts(packed)
	if len(gotIDNum) != maxKYCSegmentLen {
		t.Fatalf("round trip lost data: want %d bytes, got %d", maxKYCSegmentLen, len(gotIDNum))
	}
}
