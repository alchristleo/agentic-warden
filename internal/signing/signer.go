package signing

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Bundle signature formats. v1 signs the body; v2 signs a short statement
// naming the key and the body's SHA-256, which is what lets a KMS key sign
// a bundle of any size.
const (
	FormatV1 = "v1"
	FormatV2 = "v2"
)

const (
	bundleV2Prefix = "aw-bundle-v2"
	lineV1         = "aw-ed25519"
	lineV2         = "aw-ed25519-v2"
)

// Signer produces plain Ed25519 signatures. An implementation may be
// remote, so Sign takes a context and can fail.
type Signer interface {
	Public() ed25519.PublicKey
	Sign(ctx context.Context, msg []byte) ([]byte, error)
	// Formats lists the bundle formats this signer can produce, best first.
	Formats() []string
}

type seedSigner struct{ key ed25519.PrivateKey }

// NewSeedSigner wraps a key read by LoadSeed. A local key signs a message
// of any size, so it speaks both formats and prefers v2.
func NewSeedSigner(key ed25519.PrivateKey) Signer { return seedSigner{key: key} }

func (s seedSigner) Public() ed25519.PublicKey { return s.key.Public().(ed25519.PublicKey) }
func (s seedSigner) Formats() []string         { return []string{FormatV2, FormatV1} }
func (s seedSigner) Sign(_ context.Context, msg []byte) ([]byte, error) {
	return ed25519.Sign(s.key, msg), nil
}

// BundleStatement is the exact byte sequence a v2 signature covers.
func BundleStatement(keyID string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(bundleV2Prefix + "\n" + keyID + "\n" + hex.EncodeToString(sum[:]) + "\n")
}

// ParseFormats reads an X-AW-Signature-Formats header. Unknown names are
// ignored, and a header naming nothing known — or no header at all — means
// v1, which is what every aw-sync before v2 understood.
func ParseFormats(header string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(header, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if (name == FormatV1 || name == FormatV2) && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return []string{FormatV1}
	}
	return out
}

// Choose returns the first of supported, best first, that the client
// offered.
func Choose(offered, supported []string) (string, bool) {
	for _, s := range supported {
		for _, o := range offered {
			if s == o {
				return s, true
			}
		}
	}
	return "", false
}

// VerifyBundle checks sig over body in the given format. Any unknown
// format is false.
func VerifyBundle(format string, key ed25519.PublicKey, body []byte, sig string) bool {
	switch format {
	case FormatV1:
		return Verify(key, body, sig)
	case FormatV2:
		return Verify(key, BundleStatement(KeyID(key), body), sig)
	}
	return false
}

// SignatureLine is the one-line signature file aw-sync writes beside the
// bundle; the first token names the format.
func SignatureLine(format, keyID, sig string) string {
	token := lineV1
	if format == FormatV2 {
		token = lineV2
	}
	return token + " " + keyID + " " + sig + "\n"
}
