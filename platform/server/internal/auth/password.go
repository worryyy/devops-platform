package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

func HashPassword(plain string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hashed), nil
}

func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
