package windows

import (
	"crypto/sha1"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"
	"unsafe"

	"PDT/internal/drvsign"

	"golang.org/x/sys/windows"
)

// TrustedPublisher is one driver publisher EnsurePublishersTrusted checked.
type TrustedPublisher struct {
	Name       string
	Thumbprint string
	// Added: PDT put the certificate into Trusted Publishers just now;
	// false means it was already there.
	Added bool
}

// trustedThisRun remembers certificates already confirmed in the store
// this run, so a batch of printers on the same driver checks the store once.
var (
	trustedThisRunMu sync.Mutex
	trustedThisRun   = map[string]bool{}
)

// machineTrustedPublisher is where Windows' "Always trust software from
// ..." checkbox puts a publisher: LocalMachine\TrustedPublisher.
var machineTrustedPublisher = certStoreLocation{name: "TrustedPublisher", flags: windows.CERT_SYSTEM_STORE_LOCAL_MACHINE}

type certStoreLocation struct {
	name  string
	flags uint32
}

// EnsurePublishersTrusted makes sure the publisher who signed the driver
// package at infPath is in the machine's Trusted Publishers store, adding
// its certificate if not - so StageInf installs it without Windows' "Would
// you like to install this device software?" prompt (Ken, 2026-11-04: since
// 2026-01-15 Microsoft no longer countersigns Type 3 print drivers, so a
// manufacturer-only signature from a publisher this machine hasn't trusted
// yet stops an unattended deploy to ask). This is exactly what that
// dialog's "Always trust software from ..." checkbox does.
//
// Read from the package actually being installed - its own signed catalog
// (.cat), which the deploy's full extraction has already put next to the
// .inf - so a new driver version with a different certificate is handled
// without any cache to keep in step; the whole check costs milliseconds
// next to the extraction before it.
//
// A certificate is only ever added after WinVerifyTrust confirms the
// catalog's signature is valid and chains to a root this machine already
// trusts. Microsoft-signed (WHQL) packages and unsigned ones need nothing.
// A non-nil error never means the install must stop - Windows just asks,
// as it did before.
func EnsurePublishersTrusted(infPath string) ([]TrustedPublisher, error) {
	return ensurePublishersTrustedIn(infPath, machineTrustedPublisher)
}

func ensurePublishersTrustedIn(infPath string, loc certStoreLocation) ([]TrustedPublisher, error) {
	cats, err := drvsign.CatalogFiles(infPath)
	if err != nil {
		return nil, err
	}
	var out []TrustedPublisher
	var errs []error
	seen := map[string]bool{}
	for _, cat := range cats {
		der, err := os.ReadFile(cat)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		cert, err := drvsign.SignerCertificate(der)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cat, err))
			continue
		}
		tp := drvsign.Thumbprint(cert)
		if seen[tp] || drvsign.IsMicrosoftSigned(cert) {
			continue
		}
		seen[tp] = true
		pub := TrustedPublisher{Name: drvsign.PublisherName(cert), Thumbprint: tp}

		key := loc.name + "|" + tp
		trustedThisRunMu.Lock()
		cached := trustedThisRun[key]
		trustedThisRunMu.Unlock()
		if cached {
			out = append(out, pub)
			continue
		}
		if err := verifyCatalogSignature(cat); err != nil {
			errs = append(errs, fmt.Errorf("%s's signature on %s isn't valid, so it wasn't trusted: %w", pub.Name, cat, err))
			continue
		}
		added, err := addCertToStore(cert, loc)
		if err != nil {
			errs = append(errs, fmt.Errorf("adding %s to Trusted Publishers: %w", pub.Name, err))
			continue
		}
		pub.Added = added
		trustedThisRunMu.Lock()
		trustedThisRun[key] = true
		trustedThisRunMu.Unlock()
		out = append(out, pub)
	}
	return out, errors.Join(errs...)
}

// verifyCatalogSignature checks catPath's Authenticode signature with
// WinVerifyTrust: intact, and chaining to a trusted root. Revocation isn't
// checked - a client site may have no Internet, and a revocation lookup
// that can't complete would only stall the deploy.
func verifyCatalogSignature(catPath string) error {
	path, err := windows.UTF16PtrFromString(catPath)
	if err != nil {
		return err
	}
	file := &windows.WinTrustFileInfo{Size: uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})), FilePath: path}
	data := &windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_NONE,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(file),
		ProvFlags:                       windows.WTD_CACHE_ONLY_URL_RETRIEVAL,
	}
	verifyErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	_ = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	return verifyErr
}

// addCertToStore adds cert to the system store at loc unless a certificate
// with the same SHA-1 thumbprint is already there; reports whether it added
// one.
func addCertToStore(cert *x509.Certificate, loc certStoreLocation) (bool, error) {
	name, err := windows.UTF16PtrFromString(loc.name)
	if err != nil {
		return false, err
	}
	store, err := windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM, 0, 0, loc.flags, uintptr(unsafe.Pointer(name)))
	if err != nil {
		return false, fmt.Errorf("opening the %s store: %w", loc.name, err)
	}
	defer windows.CertCloseStore(store, 0)

	const encoding = windows.X509_ASN_ENCODING | windows.PKCS_7_ASN_ENCODING
	sum := sha1.Sum(cert.Raw)
	hash := windows.CryptHashBlob{Size: uint32(len(sum)), Data: &sum[0]}
	if existing, _ := windows.CertFindCertificateInStore(store, encoding, 0, windows.CERT_FIND_SHA1_HASH, unsafe.Pointer(&hash), nil); existing != nil {
		windows.CertFreeCertificateContext(existing)
		return false, nil
	}
	ctx, err := windows.CertCreateCertificateContext(encoding, &cert.Raw[0], uint32(len(cert.Raw)))
	if err != nil {
		return false, err
	}
	defer windows.CertFreeCertificateContext(ctx)
	if err := windows.CertAddCertificateContextToStore(store, ctx, windows.CERT_STORE_ADD_NEW, nil); err != nil {
		return false, err
	}
	return true, nil
}
