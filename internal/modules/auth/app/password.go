package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultArgon2Params = Argon2Params{
	Memory: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLength: 16, KeyLength: 32,
}

func HashPassword(password string) (string, error) {
	if len(password) < 12 {
		return "", errors.New("password must contain at least 12 characters")
	}
	salt := make([]byte, DefaultArgon2Params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, DefaultArgon2Params.Iterations, DefaultArgon2Params.Memory, DefaultArgon2Params.Parallelism, DefaultArgon2Params.KeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		DefaultArgon2Params.Memory,
		DefaultArgon2Params.Iterations,
		DefaultArgon2Params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyPassword(password, encoded string) (bool, error) {
	params, salt, expected, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func decodeHash(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return Argon2Params{}, nil, nil, errors.New("invalid Argon2id hash")
	}
	var memory, iterations uint32
	var parallelism uint8
	for _, field := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return Argon2Params{}, nil, nil, errors.New("invalid Argon2id parameters")
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return Argon2Params{}, nil, nil, errors.New("invalid Argon2id parameters")
		}
		switch key {
		case "m":
			memory = uint32(parsed)
		case "t":
			iterations = uint32(parsed)
		case "p":
			if parsed > 255 {
				return Argon2Params{}, nil, nil, errors.New("invalid Argon2id parallelism")
			}
			parallelism = uint8(parsed)
		default:
			return Argon2Params{}, nil, nil, errors.New("unknown Argon2id parameter")
		}
	}
	if memory < 8*1024 || memory > 1024*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 32 {
		return Argon2Params{}, nil, nil, errors.New("Argon2id parameters are outside safe bounds")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 {
		return Argon2Params{}, nil, nil, errors.New("invalid Argon2id salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 {
		return Argon2Params{}, nil, nil, errors.New("invalid Argon2id hash")
	}
	return Argon2Params{Memory: memory, Iterations: iterations, Parallelism: parallelism, SaltLength: uint32(len(salt)), KeyLength: uint32(len(expected))}, salt, expected, nil
}
