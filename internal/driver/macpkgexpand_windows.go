package driver

// expandPkg is macppd.go/macmount.go's own expandPkg seam on Windows: no
// pkgutil equivalent exists here, so this unpacks pkgPath's own xar
// container directly via the hand-rolled parser in macxar.go (GitHub issue
// #3 Phase 2) - no 7-Zip dependency at all for this layer, its own xar
// support being unreliable in practice (confirmed via the issue's own
// research before this was written). Only materializes what anything in
// this codebase actually reads (macPkgExpandWant - Distribution plus each
// component's own PackageInfo/Payload) rather than the whole tree, the same
// real scope reduction `pkgutil --expand` itself gets for free by not being
// `--expand-full` (see macmount.go's own PackageLabel, which now goes
// through this same seam rather than a separate `--expand-full` call - it
// only ever needed a component's own PackageInfo, already covered here).
func expandPkg(pkgPath, destDir string) error {
	return xarExtract(pkgPath, destDir, macPkgExpandWant)
}
