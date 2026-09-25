// Package awskms signs with an Ed25519 key held in AWS KMS, which never
// leaves KMS. Each call is logged by CloudTrail. KMS signs a raw message of
// at most 4096 bytes, which is why bundles are signed in format v2: a short
// statement over the body's hash.
package awskms

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/smithy-go"

	"github.com/acme/agent-wrapper/internal/signing"
)

// MaxMessage is KMS's limit on a RAW message for Ed25519 signing.
const MaxMessage = 4096

// API is the part of the KMS client this package calls, so tests use a fake.
type API interface {
	Sign(ctx context.Context, in *kms.SignInput, opts ...func(*kms.Options)) (*kms.SignOutput, error)
	GetPublicKey(ctx context.Context, in *kms.GetPublicKeyInput, opts ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error)
}

// Signer implements signing.Signer with a KMS key.
type Signer struct {
	api API
	arn string
	pub ed25519.PublicKey
}

// Load parses keyARN, builds a KMS client for its region from the default
// credential chain (on ECS, the task role), and checks the key.
func Load(ctx context.Context, keyARN string) (*Signer, error) {
	parsed, err := awsarn.Parse(keyARN)
	if err != nil || parsed.Service != "kms" || !strings.HasPrefix(parsed.Resource, "key/") {
		return nil, fmt.Errorf("awskms: %q is not a KMS key ARN (arn:aws:kms:<region>:<account>:key/<id>)", keyARN)
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(parsed.Region))
	if err != nil {
		return nil, fmt.Errorf("awskms: loading AWS configuration for %s: %w", keyARN, err)
	}
	return New(ctx, kms.NewFromConfig(cfg), keyARN)
}

// New checks that keyARN is an Ed25519 signing key and caches its public
// key. A wrong key is a refusal to start, never a signer that fails later.
func New(ctx context.Context, api API, keyARN string) (*Signer, error) {
	out, err := api.GetPublicKey(ctx, &kms.GetPublicKeyInput{KeyId: aws.String(keyARN)})
	if err != nil {
		return nil, fmt.Errorf("awskms: reading the public key of %s: %s", keyARN, describe(err))
	}
	if out.KeySpec != types.KeySpecEccNistEdwards25519 || out.KeyUsage != types.KeyUsageTypeSignVerify {
		return nil, fmt.Errorf("awskms: %s is a %s key for %s; it must be %s for %s",
			keyARN, out.KeySpec, out.KeyUsage, types.KeySpecEccNistEdwards25519, types.KeyUsageTypeSignVerify)
	}
	parsed, err := x509.ParsePKIXPublicKey(out.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("awskms: parsing the public key of %s: %w", keyARN, err)
	}
	pub, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("awskms: the public key of %s is %T, not Ed25519", keyARN, parsed)
	}
	return &Signer{api: api, arn: keyARN, pub: pub}, nil
}

func (s *Signer) Public() ed25519.PublicKey { return s.pub }

// Formats is v2 only: KMS cannot sign a whole bundle body.
func (s *Signer) Formats() []string { return []string{signing.FormatV2} }

// Sign asks KMS for a plain Ed25519 signature over msg.
func (s *Signer) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	if len(msg) > MaxMessage {
		return nil, fmt.Errorf("awskms: a %d-byte message exceeds KMS's %d-byte limit", len(msg), MaxMessage)
	}
	out, err := s.api.Sign(ctx, &kms.SignInput{
		KeyId:            aws.String(s.arn),
		Message:          msg,
		MessageType:      types.MessageTypeRaw,
		SigningAlgorithm: types.SigningAlgorithmSpecEd25519Sha512,
	})
	if err != nil {
		return nil, fmt.Errorf("awskms: signing with %s: %s", s.arn, describe(err))
	}
	if len(out.Signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("awskms: %s returned a %d-byte signature, not %d", s.arn, len(out.Signature), ed25519.SignatureSize)
	}
	return out.Signature, nil
}

// describe names the AWS error code when there is one, since that is what
// an operator searches for.
func describe(err error) string {
	var api smithy.APIError
	if errors.As(err, &api) {
		return api.ErrorCode() + ": " + api.ErrorMessage()
	}
	return err.Error()
}
