package password

import (
	"log"

	"golang.org/x/crypto/bcrypt"
)

func Hash(raw string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error Hashing Password")
		return "", err
	}
	return string(hash), nil
}

func Verify(hash string, raw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(raw))
}
