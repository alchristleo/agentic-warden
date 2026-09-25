package awskms

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/smithy-go"

	"github.com/acme/agent-wrapper/internal/signing"
)

const arn = "arn:aws:kms:eu-west-1:111122223333:key/1234abcd-12ab-34cd-56ef-1234567890ab"

type fakeKMS struct {
	priv     ed25519.PrivateKey
	spec     types.KeySpec
	usage    types.KeyUsageType
	der      []byte
	getErr   error
	signErr  error
	shortSig bool
	lastSign *kms.SignInput
	signs    int
}

func newFake(t *testing.T) *fakeKMS {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeKMS{priv: priv, spec: types.KeySpecEccNistEdwards25519, usage: types.KeyUsageTypeSignVerify, der: der}
}

func (f *fakeKMS) GetPublicKey(_ context.Context, in *kms.GetPublicKeyInput, _ ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &kms.GetPublicKeyOutput{KeyId: in.KeyId, KeySpec: f.spec, KeyUsage: f.usage, PublicKey: f.der}, nil
}

func (f *fakeKMS) Sign(_ context.Context, in *kms.SignInput, _ ...func(*kms.Options)) (*kms.SignOutput, error) {
	f.signs++
	f.lastSign = in
	if f.signErr != nil {
		return nil, f.signErr
	}
	sig := ed25519.Sign(f.priv, in.Message)
	if f.shortSig {
		sig = sig[:63]
	}
	return &kms.SignOutput{Signature: sig}, nil
}

func TestNewAndSignProduceAVerifiableV2Signature(t *testing.T) {
	f := newFake(t)
	s, err := New(context.Background(), f, arn)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Formats(); len(got) != 1 || got[0] != signing.FormatV2 {
		t.Fatalf("formats %v", got)
	}
	body := []byte(strings.Repeat("x", 100<<10))
	raw, err := s.Sign(context.Background(), signing.BundleStatement(signing.KeyID(s.Public()), body))
	if err != nil {
		t.Fatal(err)
	}
	if f.lastSign.SigningAlgorithm != types.SigningAlgorithmSpecEd25519Sha512 || f.lastSign.MessageType != types.MessageTypeRaw || *f.lastSign.KeyId != arn {
		t.Fatalf("sign input %+v", f.lastSign)
	}
	if !signing.VerifyBundle(signing.FormatV2, s.Public(), body, base64.StdEncoding.EncodeToString(raw)) {
		t.Fatal("does not verify")
	}
}

func TestNewRejectsTheWrongKey(t *testing.T) {
	cases := map[string]func(f *fakeKMS){
		"spec":  func(f *fakeKMS) { f.spec = types.KeySpecEccNistP256 },
		"usage": func(f *fakeKMS) { f.usage = types.KeyUsageTypeEncryptDecrypt },
		"der":   func(f *fakeKMS) { f.der = []byte("not der") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			mutate(f)
			_, err := New(context.Background(), f, arn)
			if err == nil || !strings.Contains(err.Error(), arn) {
				t.Fatalf("err = %v", err)
			}
			if name != "der" && !strings.Contains(err.Error(), string(f.spec)) {
				t.Fatalf("err %v does not name the spec %s", err, f.spec)
			}
		})
	}
}

func TestNewNamesTheAWSErrorCode(t *testing.T) {
	f := newFake(t)
	f.getErr = &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "not allowed"}
	_, err := New(context.Background(), f, arn)
	if err == nil || !strings.Contains(err.Error(), "AccessDeniedException") || !strings.Contains(err.Error(), arn) {
		t.Fatalf("err = %v", err)
	}
}

func TestSignRefusesOversizeBeforeCallingKMS(t *testing.T) {
	f := newFake(t)
	s, _ := New(context.Background(), f, arn)
	if _, err := s.Sign(context.Background(), make([]byte, MaxMessage+1)); err == nil {
		t.Fatal("want error")
	}
	if f.signs != 0 {
		t.Fatalf("KMS called %d times", f.signs)
	}
	if _, err := s.Sign(context.Background(), make([]byte, MaxMessage)); err != nil {
		t.Fatalf("exactly 4096 bytes: %v", err)
	}
}

func TestSignFailuresAreErrors(t *testing.T) {
	f := newFake(t)
	s, _ := New(context.Background(), f, arn)
	f.shortSig = true
	if _, err := s.Sign(context.Background(), []byte("m")); err == nil {
		t.Fatal("63-byte signature accepted")
	}
	f.shortSig = false
	f.signErr = &smithy.GenericAPIError{Code: "ThrottlingException"}
	if _, err := s.Sign(context.Background(), []byte("m")); err == nil || !strings.Contains(err.Error(), "ThrottlingException") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsMalformedARNs(t *testing.T) {
	for _, bad := range []string{"", "not-an-arn", "arn:aws:s3:::bucket", "arn:aws:kms:eu-west-1:1111:alias"} {
		if _, err := Load(context.Background(), bad); err == nil {
			t.Errorf("Load(%q) succeeded", bad)
		}
	}
}
