// Package drvsign reads who signed a driver package: the publisher
// certificate inside its signed catalog (.cat) file.
//
// Why PDT needs it (Ken, 2026-11-04): since 2026-01-15 Microsoft no longer
// countersigns Type 3 print drivers, so a package is signed by its
// manufacturer alone. Installing one from a publisher whose certificate
// isn't in the machine's Trusted Publishers store makes Windows stop and
// ask ("Would you like to install this device software?") - which breaks an
// unattended deploy. Putting the publisher's certificate in that store first
// is exactly what that dialog's "Always trust software from ..." checkbox
// does, and makes the install silent.
package drvsign

import (
	"bytes"
	"crypto/sha1"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

var oidSignedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}

// SignerCertificate returns the certificate that signed a PKCS#7 SignedData
// blob - a driver catalog (.cat) is one. It's the certificate the (first)
// SignerInfo names, by issuer and serial number or by subject key
// identifier, found among the certificates embedded in the blob. Parsed
// with encoding/asn1 rather than crypt32 so it's platform-neutral and
// testable anywhere; this only identifies the signer and does NOT verify
// the signature - callers must do that (Windows: WinVerifyTrust) before
// trusting the result.
func SignerCertificate(der []byte) (*x509.Certificate, error) {
	var ci struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return nil, fmt.Errorf("not a PKCS#7 file: %w", err)
	}
	if !ci.ContentType.Equal(oidSignedData) {
		return nil, fmt.Errorf("not PKCS#7 signed data (content type %v)", ci.ContentType)
	}

	// SignedData ::= SEQUENCE { version, digestAlgorithms SET,
	//   encapContentInfo, certificates [0] IMPLICIT OPTIONAL,
	//   crls [1] IMPLICIT OPTIONAL, signerInfos SET }
	var sd asn1.RawValue
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, fmt.Errorf("reading signed data: %w", err)
	}
	rest := sd.Bytes
	var version int
	var err error
	if rest, err = asn1.Unmarshal(rest, &version); err != nil {
		return nil, fmt.Errorf("reading signed data version: %w", err)
	}
	var skip asn1.RawValue
	for i := 0; i < 2; i++ { // digestAlgorithms, encapContentInfo
		if rest, err = asn1.Unmarshal(rest, &skip); err != nil {
			return nil, fmt.Errorf("reading signed data: %w", err)
		}
	}
	var certs []*x509.Certificate
	var signerInfos asn1.RawValue
	for len(rest) > 0 {
		var el asn1.RawValue
		if rest, err = asn1.Unmarshal(rest, &el); err != nil {
			return nil, fmt.Errorf("reading signed data: %w", err)
		}
		switch {
		case el.Class == asn1.ClassContextSpecific && el.Tag == 0:
			certs = parseCertificateSet(el.Bytes)
		case el.Class == asn1.ClassUniversal && el.Tag == asn1.TagSet:
			signerInfos = el
		}
	}
	if len(signerInfos.Bytes) == 0 {
		return nil, errors.New("not signed: no signer information")
	}

	// SignerInfo ::= SEQUENCE { version, sid, ... } - only the first is
	// the primary signature.
	var si asn1.RawValue
	if _, err := asn1.Unmarshal(signerInfos.Bytes, &si); err != nil {
		return nil, fmt.Errorf("reading signer: %w", err)
	}
	siRest := si.Bytes
	var siVersion int
	if siRest, err = asn1.Unmarshal(siRest, &siVersion); err != nil {
		return nil, fmt.Errorf("reading signer: %w", err)
	}
	var sid asn1.RawValue
	if _, err := asn1.Unmarshal(siRest, &sid); err != nil {
		return nil, fmt.Errorf("reading signer: %w", err)
	}

	if sid.Class == asn1.ClassContextSpecific && sid.Tag == 0 { // [0] subjectKeyIdentifier
		for _, c := range certs {
			if len(c.SubjectKeyId) > 0 && bytes.Equal(c.SubjectKeyId, sid.Bytes) {
				return c, nil
			}
		}
		return nil, errors.New("the signing certificate isn't included in the file")
	}
	var ias struct {
		Issuer asn1.RawValue
		Serial *big.Int
	}
	if _, err := asn1.Unmarshal(sid.FullBytes, &ias); err != nil {
		return nil, fmt.Errorf("reading signer identity: %w", err)
	}
	for _, c := range certs {
		if bytes.Equal(c.RawIssuer, ias.Issuer.FullBytes) && c.SerialNumber.Cmp(ias.Serial) == 0 {
			return c, nil
		}
	}
	return nil, errors.New("the signing certificate isn't included in the file")
}

// parseCertificateSet parses each DER certificate in a SET OF Certificate's
// contents, skipping any it can't parse (a catalog can carry extra
// timestamping or chain certificates the signer match never needs).
func parseCertificateSet(b []byte) []*x509.Certificate {
	var out []*x509.Certificate
	for len(b) > 0 {
		var el asn1.RawValue
		rest, err := asn1.Unmarshal(b, &el)
		if err != nil {
			break
		}
		if c, err := x509.ParseCertificate(el.FullBytes); err == nil {
			out = append(out, c)
		}
		b = rest
	}
	return out
}

// Thumbprint is a certificate's SHA-1 thumbprint as uppercase hex - the
// form Windows' certificate store and certmgr show, and what it's looked up
// by (CERT_FIND_SHA1_HASH).
func Thumbprint(c *x509.Certificate) string {
	sum := sha1.Sum(c.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// IsMicrosoftSigned reports whether c is Microsoft's own driver-signing
// certificate (a WHQL-countersigned package from before 2026-01-15) -
// trusted by every Windows machine already, so there's nothing to add.
func IsMicrosoftSigned(c *x509.Certificate) bool {
	for _, o := range c.Subject.Organization {
		if strings.EqualFold(o, "Microsoft Corporation") {
			return true
		}
	}
	return false
}

// PublisherName is the certificate's display name - its subject common
// name, falling back to the organization.
func PublisherName(c *x509.Certificate) string {
	if c.Subject.CommonName != "" {
		return c.Subject.CommonName
	}
	if len(c.Subject.Organization) > 0 {
		return c.Subject.Organization[0]
	}
	return c.Subject.String()
}
