package crypto

import (
	"database/sql/driver"
	"errors"
	"fmt"
)

// EncryptedString is a string that is transparently
// encrypted on write and decrypted on read via GORM.
type EncryptedString string

// Value is called by GORM/database driver on INSERT/UPDATE.
func (e EncryptedString) Value() (driver.Value, error) {
	if e == "" {
		return "", nil
	}
	encrypted, err := Encrypt(string(e))
	if err != nil {
		return nil, fmt.Errorf("EncryptedString.Value: %w", err)
	}
	return encrypted, nil
}

// Scan is called by GORM/database driver on SELECT.
func (e *EncryptedString) Scan(value any) error {
	if value == nil {
		*e = ""
		return nil
	}
	s, ok := value.(string)
	if !ok {
		return errors.New("EncryptedString.Scan: expected string")
	}
	if s == "" {
		*e = ""
		return nil
	}
	decrypted, err := Decrypt(s)
	if err != nil {
		return fmt.Errorf("EncryptedString.Scan: %w", err)
	}
	*e = EncryptedString(decrypted)
	return nil
}

// String lets you use it as a plain string easily.
func (e EncryptedString) String() string {
	return string(e)
}
