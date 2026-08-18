package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type PasswordParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

func DefaultPasswordParams() PasswordParams {
	return PasswordParams{Memory: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLength: 16, KeyLength: 32}
}

func HashPassword(password string, params PasswordParams) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", fmt.Errorf("password must contain between 12 and 1024 bytes")
	}
	if err := validatePasswordParams(params); err != nil {
		return "", err
	}
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", params.Memory, params.Iterations, params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) (bool, error) {
	params, salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func NeedsPasswordRehash(encoded string, desired PasswordParams) bool {
	current, _, _, err := parsePasswordHash(encoded)
	return err != nil || current.Memory != desired.Memory || current.Iterations != desired.Iterations || current.Parallelism != desired.Parallelism || current.KeyLength != desired.KeyLength
}

func validatePasswordParams(p PasswordParams) error {
	if p.Memory < 8*1024 || p.Memory > 256*1024 || p.Iterations < 1 || p.Iterations > 10 || p.Parallelism < 1 || p.Parallelism > 16 || p.SaltLength < 16 || p.KeyLength < 24 {
		return fmt.Errorf("unsafe Argon2id parameters")
	}
	return nil
}

func parsePasswordHash(encoded string) (PasswordParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return PasswordParams{}, nil, nil, fmt.Errorf("invalid Argon2id hash")
	}
	var p PasswordParams
	for _, field := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(field, "=", 2)
		if len(pair) != 2 {
			return p, nil, nil, fmt.Errorf("invalid Argon2id parameters")
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return p, nil, nil, fmt.Errorf("invalid Argon2id parameter")
		}
		switch pair[0] {
		case "m":
			p.Memory = uint32(value)
		case "t":
			p.Iterations = uint32(value)
		case "p":
			p.Parallelism = uint8(value)
		default:
			return p, nil, nil, fmt.Errorf("unknown Argon2id parameter")
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, fmt.Errorf("invalid Argon2id salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, fmt.Errorf("invalid Argon2id key")
	}
	p.SaltLength, p.KeyLength = uint32(len(salt)), uint32(len(key))
	if err := validatePasswordParams(p); err != nil {
		return p, nil, nil, err
	}
	return p, salt, key, nil
}
