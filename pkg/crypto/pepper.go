package crypto

import (
	"crypto/rand"
	"fmt"
	"os"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/atomicfile"
)

const (
	// pepperFileName is the name of the file storing the pepper.
	pepperFileName = "pepper.key"
	// pepperDir is the directory where the pepper is stored. It is deliberately
	// not data/tokens: the pepper protects the key-encryption key derived from
	// the master passphrase, and keeping it out of the same tree as the
	// credentials it ultimately protects lets the two be placed on separate
	// volumes (e.g. distinct Docker volumes) in deployment.
	pepperDir = "data/pepper"
	// pepperSize is 32 bytes, matching the AES-256 key it helps protect.
	pepperSize = 32
)

// loadOrCreatePepper loads the persisted pepper or generates and persists a
// new one. The pepper is a secret, unlike a salt: it never appears alongside
// the ciphertext it helps protect.
func loadOrCreatePepper() ([]byte, error) {
	path := pepperFileName
	// The path is this process's own pepper file under the gitignored data/
	// tree, never a caller-supplied value.
	// #nosec G304
	if data, err := os.ReadFile(pepperDir + "/" + path); err == nil {
		if len(data) != pepperSize {
			return nil, fmt.Errorf("pepper file has %d bytes, want %d", len(data), pepperSize)
		}
		return data, nil
	}

	pepper := make([]byte, pepperSize)
	if _, err := rand.Read(pepper); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyGeneration, err)
	}

	if err := atomicfile.Write(pepperDir+"/"+path, pepper, 0600); err != nil {
		return nil, fmt.Errorf("failed to write pepper file: %w", err)
	}

	return pepper, nil
}
