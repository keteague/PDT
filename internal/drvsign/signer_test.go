package drvsign

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

func makeCert(t *testing.T, cn, org string, serial int64, skid []byte) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: cn, Organization: []string{org}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		SubjectKeyId: skid,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// buildSignedData assembles a minimal PKCS#7 SignedData - just enough
// structure for SignerCertificate (no real signature), with the given
// certificates and one SignerInfo whose sid is sidDER.
func buildSignedData(t *testing.T, certs []*x509.Certificate, sidDER []byte) []byte {
	t.Helper()
	mustMarshal := func(v any, params string) []byte {
		b, err := asn1.MarshalWithParams(v, params)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var certBytes []byte
	for _, c := range certs {
		certBytes = append(certBytes, c.Raw...)
	}
	version := mustMarshal(1, "")
	digestAlgs := mustMarshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true}, "")
	encap := mustMarshal(struct{ OID asn1.ObjectIdentifier }{asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 10, 1}}, "")
	certSet := mustMarshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: certBytes}, "")
	signerInfo := mustMarshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSequence, IsCompound: true, Bytes: append(mustMarshal(1, ""), sidDER...)}, "")
	signerInfos := mustMarshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: signerInfo}, "")
	var sdBody []byte
	for _, part := range [][]byte{version, digestAlgs, encap, certSet, signerInfos} {
		sdBody = append(sdBody, part...)
	}
	sd := mustMarshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSequence, IsCompound: true, Bytes: sdBody}, "")
	// [0] EXPLICIT wrapper built by hand: asn1.Marshal ignores the explicit
	// tag for a RawValue that already carries FullBytes.
	return mustMarshal(struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue
	}{oidSignedData, asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: sd}}, "")
}

func issuerAndSerial(t *testing.T, c *x509.Certificate) []byte {
	t.Helper()
	b, err := asn1.Marshal(struct {
		Issuer asn1.RawValue
		Serial *big.Int
	}{asn1.RawValue{FullBytes: c.RawIssuer}, c.SerialNumber})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The signer is picked by issuer+serial out of several embedded certs, not
// just "the first one".
func TestSignerCertificate_ByIssuerAndSerial(t *testing.T) {
	chain := makeCert(t, "Some Timestamp CA", "TS Inc.", 7, nil)
	signer := makeCert(t, "KYOCERA Document Solutions Inc.", "KYOCERA Document Solutions Inc.", 42, nil)
	der := buildSignedData(t, []*x509.Certificate{chain, signer}, issuerAndSerial(t, signer))

	got, err := SignerCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if Thumbprint(got) != Thumbprint(signer) {
		t.Errorf("got %s, want the Kyocera signer", got.Subject)
	}
	if PublisherName(got) != "KYOCERA Document Solutions Inc." || IsMicrosoftSigned(got) {
		t.Errorf("name %q / microsoft %v", PublisherName(got), IsMicrosoftSigned(got))
	}
}

func TestSignerCertificate_BySubjectKeyID(t *testing.T) {
	skid := []byte{1, 2, 3, 4, 5}
	signer := makeCert(t, "Publisher", "Pub Inc.", 3, skid)
	other := makeCert(t, "Other", "Other Inc.", 4, []byte{9, 9})
	sid, err := asn1.Marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, Bytes: skid})
	if err != nil {
		t.Fatal(err)
	}
	got, err := SignerCertificate(buildSignedData(t, []*x509.Certificate{other, signer}, sid))
	if err != nil || Thumbprint(got) != Thumbprint(signer) {
		t.Fatalf("got %v, %v; want the SKID match", got, err)
	}
}

func TestSignerCertificate_SignerNotIncluded(t *testing.T) {
	signer := makeCert(t, "Absent", "Absent Inc.", 5, nil)
	other := makeCert(t, "Present", "Present Inc.", 6, nil)
	_, err := SignerCertificate(buildSignedData(t, []*x509.Certificate{other}, issuerAndSerial(t, signer)))
	if err == nil || !strings.Contains(err.Error(), "isn't included") {
		t.Errorf("expected the signer-not-included error, got %v", err)
	}
}

func TestSignerCertificate_NotPKCS7(t *testing.T) {
	if _, err := SignerCertificate([]byte("[Version]\r\nSignature=\"$Windows NT$\"")); err == nil {
		t.Error("expected an error for a non-PKCS#7 file")
	}
}

func TestIsMicrosoftSigned(t *testing.T) {
	ms := makeCert(t, "Microsoft Windows Hardware Compatibility Publisher", "Microsoft Corporation", 1, nil)
	if !IsMicrosoftSigned(ms) {
		t.Error("expected Microsoft's driver-signing certificate to be recognized")
	}
}

// TestSignerCertificate_RealCatalog checks a real driver catalog when
// PDT_TEST_CAT points at one (e.g. Kyocera's kyomita.cat); expected
// publisher in PDT_TEST_CAT_SIGNER. Skipped otherwise - vendor catalogs
// aren't committed to the repo.
func TestSignerCertificate_RealCatalog(t *testing.T) {
	path := os.Getenv("PDT_TEST_CAT")
	if path == "" {
		t.Skip("PDT_TEST_CAT not set")
	}
	der, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := SignerCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("signer %q, issuer %q, thumbprint %s", PublisherName(c), c.Issuer.CommonName, Thumbprint(c))
	if want := os.Getenv("PDT_TEST_CAT_SIGNER"); want != "" && !strings.EqualFold(PublisherName(c), want) {
		t.Errorf("signer %q, want %q", PublisherName(c), want)
	}
	if want := os.Getenv("PDT_TEST_CAT_THUMBPRINT"); want != "" && Thumbprint(c) != strings.ToUpper(want) {
		t.Errorf("thumbprint %s, want %s", Thumbprint(c), want)
	}
}
