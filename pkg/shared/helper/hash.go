package helper

import (
	"golang.org/x/crypto/bcrypt"
)

// CheckPassword - Method to Check password hash
func CheckPassword(password string, hash []byte) bool {
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	return err == nil
}

// GeneratePasswordHash - Method to generate password hash
func GeneratePasswordHash(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}
