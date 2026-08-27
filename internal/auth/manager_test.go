package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordsAreStoredAsSaltedBcryptHashes(t *testing.T) {
	const password = "warehouse-no-8"

	first, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}

	if first == password || second == password {
		t.Fatal("password was retained as plaintext")
	}
	if first == second {
		t.Fatal("bcrypt hashes did not use independent salts")
	}
	if !strings.HasPrefix(first, "$2") {
		t.Fatalf("password hash %q is not bcrypt", first)
	}
	cost, err := bcrypt.Cost([]byte(first))
	if err != nil {
		t.Fatal(err)
	}
	if cost != bcrypt.DefaultCost {
		t.Fatalf("bcrypt cost = %d, want %d", cost, bcrypt.DefaultCost)
	}
	if !passwordMatches(first, password) {
		t.Fatal("correct password did not match its hash")
	}
	if passwordMatches(first, "wrong-password") {
		t.Fatal("incorrect password matched the stored hash")
	}
}
