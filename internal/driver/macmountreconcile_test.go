package driver

import "testing"

// realHdiutilInfoOutput is copied verbatim (2026-09-16) from a real `hdiutil
// info` run on Ken's own machine that caught GitHub issue #1's own leak
// live: a Canon UFR II package mounted via resolveMacZipSource's own temp
// extraction (pdt-maczip-*), its own nested locale .dmg mounted from inside
// that first volume (CANON_MAC), and a Toshiba .dmg.gz mounted via
// decompressGzipToTemp's own temp copy (pdt-mac-dmg-*.dmg) - all three with
// mounting process IDs that had long since exited by the time this was
// captured, confirming these were genuinely abandoned, not still in active
// use by anything.
const realHdiutilInfoOutput = `framework       : 683.160.3
driver          : 683.160.3
images          : 3
================================================
image-path      : /var/folders/19/_zp56lyn21n19vvtr232xxwc0000gn/T/pdt-maczip-4021173479/UFRII_v10.19.25_mac.dmg
image-alias     : /private/var/folders/19/_zp56lyn21n19vvtr232xxwc0000gn/T/pdt-maczip-4021173479/UFRII_v10.19.25_mac.dmg
shadow-path     : <none>
icon-path       : /System/Library/PrivateFrameworks/DiskImages.framework/Resources/CDiskImage.icns
image-type      : UDIF read-only compressed (zlib)
system-image    : false
blockcount      : 196901
blocksize       : 512
writeable       : false
autodiskmount   : TRUE
removable       : TRUE
image-encrypted : false
mounting user   : kteague
mounting mode   : <unknown>
process ID      : 40171
framework name  : DiskImages
/dev/disk4	GUID_partition_scheme
/dev/disk4s1	48465300-0000-11AA-AA11-00306543ECAC	/Volumes/CANON_MAC
================================================
image-path      : /Volumes/CANON_MAC/mac-UFRII-LIPSLX-v101925-05.dmg
image-alias     : /Volumes/CANON_MAC/mac-UFRII-LIPSLX-v101925-05.dmg
shadow-path     : <none>
icon-path       : /System/Library/PrivateFrameworks/DiskImages.framework/Resources/CDiskImage.icns
image-type      : UDIF read-only compressed (zlib)
system-image    : false
blockcount      : 197578
blocksize       : 512
writeable       : false
autodiskmount   : TRUE
removable       : TRUE
image-encrypted : false
mounting user   : kteague
mounting mode   : <unknown>
process ID      : 40201
framework name  : DiskImages
/dev/disk5	GUID_partition_scheme
/dev/disk5s1	48465300-0000-11AA-AA11-00306543ECAC	/Volumes/mac-UFRII-LIPSLX-v101925-05
================================================
image-path      : /var/folders/19/_zp56lyn21n19vvtr232xxwc0000gn/T/pdt-mac-dmg-2759616230.dmg
image-alias     : /private/var/folders/19/_zp56lyn21n19vvtr232xxwc0000gn/T/pdt-mac-dmg-2759616230.dmg
shadow-path     : <none>
icon-path       : /System/Library/PrivateFrameworks/DiskImages.framework/Resources/CDiskImage.icns
image-type      : UDIF read-only compressed (zlib)
system-image    : false
blockcount      : 81920
blocksize       : 512
writeable       : false
autodiskmount   : TRUE
removable       : TRUE
image-encrypted : false
mounting user   : kteague
mounting mode   : <unknown>
process ID      : 40314
framework name  : DiskImages
/dev/disk9	GUID_partition_scheme
/dev/disk9s1	48465300-0000-11AA-AA11-00306543ECAC	/Volumes/TOSHIBA ColorMFP
`

func TestParseHdiutilInfo_RealThreeMountOutput(t *testing.T) {
	mounts := parseHdiutilInfo(realHdiutilInfoOutput)
	if len(mounts) != 3 {
		t.Fatalf("got %d mounts, want 3: %+v", len(mounts), mounts)
	}
	want := []hdiutilMount{
		{imagePath: "/var/folders/19/_zp56lyn21n19vvtr232xxwc0000gn/T/pdt-maczip-4021173479/UFRII_v10.19.25_mac.dmg", mountPoint: "/Volumes/CANON_MAC"},
		{imagePath: "/Volumes/CANON_MAC/mac-UFRII-LIPSLX-v101925-05.dmg", mountPoint: "/Volumes/mac-UFRII-LIPSLX-v101925-05"},
		{imagePath: "/var/folders/19/_zp56lyn21n19vvtr232xxwc0000gn/T/pdt-mac-dmg-2759616230.dmg", mountPoint: "/Volumes/TOSHIBA ColorMFP"},
	}
	for i, w := range want {
		if mounts[i] != w {
			t.Errorf("mount %d = %+v, want %+v", i, mounts[i], w)
		}
	}
}

// TestMountsToDetach_RealLeakDetectsAllThreeNestedFirst is the direct
// regression test for the real, live leak this issue's own fix targets:
// given the real captured output above, every one of the 3 mounts should
// be recognized as PDT-owned and slated for detach, with the nested
// locale-variant mount (mounted from a file living on CANON_MAC, itself
// one of the temp-dir-rooted entries) coming back in the nested group -
// detaching it before CANON_MAC matters, since detaching CANON_MAC first
// would leave it reading from an already-vanished image.
func TestMountsToDetach_RealLeakDetectsAllThreeNestedFirst(t *testing.T) {
	mounts := parseHdiutilInfo(realHdiutilInfoOutput)
	tmpDir := "/var/folders/19/_zp56lyn21n19vvtr232xxwc0000gn/T"
	nested, outer := mountsToDetach(mounts, tmpDir, "/Users/kteague/Library/Application Support/PDT/Drivers/macOS")

	if len(nested) != 1 || nested[0] != "/Volumes/mac-UFRII-LIPSLX-v101925-05" {
		t.Errorf("nested = %v, want exactly [\"/Volumes/mac-UFRII-LIPSLX-v101925-05\"]", nested)
	}
	gotOuter := map[string]bool{}
	for _, mp := range outer {
		gotOuter[mp] = true
	}
	for _, want := range []string{"/Volumes/CANON_MAC", "/Volumes/TOSHIBA ColorMFP"} {
		if !gotOuter[want] {
			t.Errorf("outer = %v, want it to include %q", outer, want)
		}
	}
	if len(outer) != 2 {
		t.Errorf("outer = %v, want exactly 2 entries", outer)
	}
}

// TestMountsToDetach_UnrelatedMountNeverTouched guards the safety property
// that matters most here: a real, unrelated disk image the user mounted
// themselves (nothing about its own image-path matches a PDT temp-dir
// pattern or lives under the Drivers folder) must never be detached, no
// matter how it's shaped.
func TestMountsToDetach_UnrelatedMountNeverTouched(t *testing.T) {
	mounts := []hdiutilMount{
		{imagePath: "/Users/kteague/Downloads/SomeOtherApp.dmg", mountPoint: "/Volumes/SomeOtherApp"},
		{imagePath: "/Volumes/Backups/TimeMachine.sparsebundle", mountPoint: "/Volumes/TimeMachine"},
	}
	nested, outer := mountsToDetach(mounts, "/var/folders/xx/T", "/Users/kteague/Library/Application Support/PDT/Drivers/macOS")
	if len(nested) != 0 || len(outer) != 0 {
		t.Errorf("nested=%v outer=%v, want both empty - neither mount is PDT's own", nested, outer)
	}
}

// TestMountsToDetach_DirectlyMountedRealDriversFolderFile guards the case
// most manufacturers actually hit (everyone except Canon's own zip-wrapped
// download and Toshiba's own gzip-wrapped one): mountDmg called directly on
// a real .dmg sitting under the Drivers folder, no temp copy involved at
// all - must still be recognized as PDT-owned via the driversRoot check,
// not just the temp-dir one.
func TestMountsToDetach_DirectlyMountedRealDriversFolderFile(t *testing.T) {
	driversRoot := "/Users/kteague/Library/Application Support/PDT/Drivers/macOS"
	mounts := []hdiutilMount{
		{imagePath: driversRoot + "/Ricoh/26-Tahoe/RicohPrinterDrivers.dmg", mountPoint: "/Volumes/Ricoh_PrinterSupportManual"},
	}
	nested, outer := mountsToDetach(mounts, "/var/folders/xx/T", driversRoot)
	if len(nested) != 0 || len(outer) != 1 || outer[0] != "/Volumes/Ricoh_PrinterSupportManual" {
		t.Errorf("nested=%v outer=%v, want outer=[\"/Volumes/Ricoh_PrinterSupportManual\"]", nested, outer)
	}
}

func TestMountsToDetach_EmptyInput(t *testing.T) {
	nested, outer := mountsToDetach(nil, "/tmp", "/drivers")
	if len(nested) != 0 || len(outer) != 0 {
		t.Errorf("nested=%v outer=%v, want both empty for no mounts at all", nested, outer)
	}
}
