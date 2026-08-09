package auth

import "testing"

func TestArgon2HasherCreatesSaltedVerifiableHashes(t *testing.T) {
	hasher := NewArgon2Hasher()

	first, err := hasher.Hash("correct horse battery")
	if err != nil {
		t.Fatalf("first Hash() error = %v", err)
	}
	second, err := hasher.Hash("correct horse battery")
	if err != nil {
		t.Fatalf("second Hash() error = %v", err)
	}
	if first == second {
		t.Fatal("two hashes for the same password must use different salts")
	}

	match, err := hasher.Verify("correct horse battery", first)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !match {
		t.Fatal("Verify() = false, want true")
	}
	match, err = hasher.Verify("wrong horse battery", first)
	if err != nil {
		t.Fatalf("Verify() wrong password error = %v", err)
	}
	if match {
		t.Fatal("Verify() wrong password = true, want false")
	}
}
