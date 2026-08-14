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

// Argon2id parameters. Tuned per OWASP guidance and gentle enough for the
// constrained gateway hardware: 19 MiB memory, 2 iterations, 1 lane.
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// hashPassword returns a PHC-format argon2id string:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<b64 salt>$<b64 hash>
func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		b64(salt), b64(key)), nil
}

// verifyPassword checks a password against a stored PHC hash in constant time.
func verifyPassword(password, encoded string) (bool, error) {
	m, t, p, salt, key, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(key)))
	return subtle.ConstantTimeCompare(key, candidate) == 1, nil
}

func decodeHash(encoded string) (m, t uint32, p uint8, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, key]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, errors.New("invalid hash format")
	}
	var version int
	if _, err = fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return
	}
	if version != argon2.Version {
		return 0, 0, 0, nil, nil, errors.New("incompatible argon2 version")
	}
	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return
	}
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return
	}
	if key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return
	}
	return m, t, p, salt, key, nil
}

func b64(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }
