package driver

import "os/exec"

// expandPkg is macppd.go/macmount.go's own expandPkg seam on darwin: the
// real `pkgutil --expand`, exactly as every call site already used inline
// before GitHub issue #3 Phase 2 - moved here verbatim, zero behavior
// change, so a Windows-native implementation (macpkgexpand_windows.go, via
// the hand-rolled xar TOC parser in macxar.go - no pkgutil equivalent exists
// there) can stand in under the same name/signature.
func expandPkg(pkgPath, destDir string) error {
	return exec.Command("pkgutil", "--expand", pkgPath, destDir).Run()
}
