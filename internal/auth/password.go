package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters. These follow the OWASP baseline (19 MiB, 2 iterations);
// bumping them later is safe because the cost is encoded in each stored hash.
const (
	argonMemory      uint32 = 19 * 1024
	argonIterations  uint32 = 2
	argonParallelism uint8  = 1
	argonSaltLength  int    = 16
	argonKeyLength   uint32 = 32
)

// Password length bounds. The upper bound keeps a huge input from turning a
// login into a denial-of-service via hashing cost.
const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

var (
	// ErrMismatchedPassword means the password did not match the stored hash.
	ErrMismatchedPassword = errors.New("auth: password does not match")
	// ErrInvalidHash means the stored hash was not a recognizable argon2id string.
	ErrInvalidHash = errors.New("auth: invalid password hash format")
)

// HashPassword returns an encoded argon2id hash in the standard PHC format:
// $argon2id$v=19$m=19456,t=2,p=1$<b64 salt>$<b64 hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword compares a plaintext password against an encoded hash. It
// returns ErrMismatchedPassword on a wrong password and ErrInvalidHash when the
// stored value cannot be decoded.
func VerifyPassword(password, encodedHash string) error {
	params, salt, key, err := decodeHash(encodedHash)
	if err != nil {
		return err
	}

	candidate := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, uint32(len(key)))

	if subtle.ConstantTimeCompare(key, candidate) != 1 {
		return ErrMismatchedPassword
	}
	return nil
}

type argonParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func decodeHash(encodedHash string) (argonParams, []byte, []byte, error) {
	var params argonParams

	parts := strings.Split(encodedHash, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return params, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return params, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return params, nil, nil, fmt.Errorf("%w: unsupported argon2 version %d", ErrInvalidHash, version)
	}

	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memory, &params.iterations, &params.parallelism); err != nil {
		return params, nil, nil, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return params, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return params, nil, nil, ErrInvalidHash
	}
	if len(key) == 0 {
		return params, nil, nil, ErrInvalidHash
	}

	return params, salt, key, nil
}

// dummyHash is verified against when an email has no account or has no local
// password. Doing the work anyway keeps login latency from revealing which
// emails exist.
var dummyHash string

func init() {
	h, err := HashPassword("roundly-timing-equalizer")
	if err != nil {
		panic(fmt.Sprintf("auth: cannot initialize dummy hash: %v", err))
	}
	dummyHash = h
}
