package driver

import (
	"compress/zlib"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
)

// xarHeader is a xar container's own fixed 28-byte header (confirmed live,
// GitHub issue #3 Phase 1's real extracted Kyocera .pkg: magic "xar!",
// header_size=0x001c=28, version=1, toc_length_compressed=0x2dfe,
// toc_length_uncompressed=0x14292, checksum_alg=1, immediately followed by a
// real zlib stream header (0x78 0xda) - exactly this shape). All fields
// big-endian, per the xar spec.
type xarHeader struct {
	Magic                 [4]byte
	HeaderSize            uint16
	Version               uint16
	TOCLengthCompressed   uint64
	TOCLengthUncompressed uint64
	ChecksumAlg           uint32
}

const xarMagic = "xar!"

// xarTOC is the root of a xar container's own TOC XML - the zlib-compressed
// blob immediately following the header, up to TOCLengthCompressed bytes.
type xarTOC struct {
	XMLName xml.Name  `xml:"xar"`
	Files   []xarFile `xml:"toc>file"`
}

// xarFile is one <file> entry in the TOC - either a leaf with its own <data>
// (an offset/length into the heap, plus optional gzip encoding), or a
// directory with nested <file> children of its own. A real Distribution-style
// product archive (Kyocera's own "Web Build", Canon's Core/Device split)
// nests one such directory per component sub-package, each holding its own
// PackageInfo/Bom/Payload/Scripts children - exactly the shape a real
// `pkgutil --expand` reproduces as real subdirectories on disk. A flat,
// single-component .pkg has no such wrapping directory at all - its
// PackageInfo/Payload/Bom sit directly in Files at the TOC's own root,
// matching how `pkgutil --expand` also leaves destDir itself as that one
// component's own directory for that shape.
type xarFile struct {
	Name  string    `xml:"name"`
	Type  string    `xml:"type"`
	Data  *xarData  `xml:"data"`
	Files []xarFile `xml:"file"`
}

type xarData struct {
	Offset   int64        `xml:"offset"`
	Length   int64        `xml:"length"`
	Encoding *xarEncoding `xml:"encoding"`
}

type xarEncoding struct {
	Style string `xml:"style,attr"`
}

// xarGzipEncoding is the one real per-entry encoding style seen in a real
// macOS installer package (Kyocera's own real .pkg, confirmed live) -
// Distribution/PackageInfo/Payload/Bom/Scripts are each stored
// xar-level-compressed under this label. Anything else (absent <encoding>,
// or a different style) is treated as stored raw - xar itself supports that
// too, just not seen in a real driver package so far.
//
// Real, confirmed-live gotcha: despite the "application/x-gzip" name, the
// actual on-disk bytes under this label are zlib-wrapped deflate (RFC 1950,
// a 2-byte header like 0x78 0xda - confirmed directly against this exact
// package's own real Distribution entry), NOT the real gzip file format
// (RFC 1952, magic 0x1f 0x8b) - a documented xar-format oddity, not a bug in
// this package's own downloads. compress/gzip.NewReader on these bytes fails
// outright ("invalid header"); compress/zlib.NewReader is what actually
// works. This is a *different* layer from Payload's own separate, genuinely
// real-gzip-format inner compression (the cpio archive's own gzip, applied
// before the result was ever stored in the xar container at all) - decoding
// this outer zlib layer for Payload specifically still leaves real gzip
// bytes on disk afterward, exactly matching what a real `pkgutil --expand`
// leaves behind (see xarExtract's own doc comment) - only the encoding *name*
// this constant matches against is misleading, not the two-layer model
// itself.
const xarGzipEncoding = "application/x-gzip"

// parseXarTOC reads r's own xar header and TOC XML - r must support Seek
// (a plain *os.File), since the header declares HeaderSize/TOCLengthCompressed
// as byte counts to seek past, not something a plain streaming read can
// recover from if it guessed wrong. Returns the parsed TOC plus heapStart,
// the absolute byte offset every <data><offset> in it is relative to.
func parseXarTOC(r io.ReadSeeker) (toc *xarTOC, heapStart int64, err error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}
	var hdr xarHeader
	if err := binary.Read(r, binary.BigEndian, &hdr); err != nil {
		return nil, 0, fmt.Errorf("reading xar header: %w", err)
	}
	if string(hdr.Magic[:]) != xarMagic {
		return nil, 0, fmt.Errorf("not a xar file (magic %q)", hdr.Magic[:])
	}

	if _, err := r.Seek(int64(hdr.HeaderSize), io.SeekStart); err != nil {
		return nil, 0, err
	}
	tocZlib := io.LimitReader(r, int64(hdr.TOCLengthCompressed))
	zr, err := zlib.NewReader(tocZlib)
	if err != nil {
		return nil, 0, fmt.Errorf("decompressing xar TOC: %w", err)
	}
	defer zr.Close()
	tocXML, err := io.ReadAll(zr)
	if err != nil {
		return nil, 0, fmt.Errorf("reading xar TOC: %w", err)
	}

	toc = &xarTOC{}
	if err := xml.Unmarshal(tocXML, toc); err != nil {
		return nil, 0, fmt.Errorf("parsing xar TOC XML: %w", err)
	}
	heapStart = int64(hdr.HeaderSize) + int64(hdr.TOCLengthCompressed)
	return toc, heapStart, nil
}

// xarExtract walks pkgPath's own xar TOC (a flat or Distribution-style
// product .pkg either way - see xarFile's own doc comment) and, for every
// leaf entry whose reconstructed relative path satisfies want, decodes its
// bytes out of the heap and writes them to destDir/<relPath> (creating
// parent directories as needed). Every other entry is still walked (needed
// to reach a directory's own children at all) but never extracted - the real
// scope reduction GitHub issue #3's own tracing confirmed: nothing in this
// codebase ever reads a .pkg's Bom or Scripts contents, only PackageInfo and
// Payload (plus the top-level Distribution file for a product archive), so a
// caller's own want only ever needs to admit those.
//
// Each leaf's own bytes are xar-level-gunzipped here if declared
// (xarGzipEncoding) - a *different* compression layer than the TOC's own
// zlib above. For Payload specifically, this strips only the xar container's
// own compression: the resulting on-disk bytes are still gzip-compressed
// (the separate, inner cpio-archive-level gzip macOS's own installer tooling
// applies), exactly matching what a real `pkgutil --expand` leaves on disk -
// macppd.go's own gzip.NewReader(f) call against a real Payload file needs no
// changes for this.
func xarExtract(pkgPath, destDir string, want func(relPath string) bool) error {
	f, err := os.Open(pkgPath)
	if err != nil {
		return err
	}
	defer f.Close()

	toc, heapStart, err := parseXarTOC(f)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", pkgPath, err)
	}

	return xarExtractFiles(f, heapStart, toc.Files, "", destDir, want)
}

func xarExtractFiles(f *os.File, heapStart int64, files []xarFile, relDir, destDir string, want func(relPath string) bool) error {
	for _, xf := range files {
		relPath := xf.Name
		if relDir != "" {
			relPath = path.Join(relDir, xf.Name)
		}
		if len(xf.Files) > 0 {
			if err := xarExtractFiles(f, heapStart, xf.Files, relPath, destDir, want); err != nil {
				return err
			}
			continue
		}
		if xf.Data == nil || !want(relPath) {
			continue
		}
		if err := xarExtractOne(f, heapStart, xf, relPath, destDir); err != nil {
			return err
		}
	}
	return nil
}

func xarExtractOne(f *os.File, heapStart int64, xf xarFile, relPath, destDir string) error {
	if _, err := f.Seek(heapStart+xf.Data.Offset, io.SeekStart); err != nil {
		return err
	}
	var r io.Reader = io.LimitReader(f, xf.Data.Length)
	if xf.Data.Encoding != nil && xf.Data.Encoding.Style == xarGzipEncoding {
		zr, err := zlib.NewReader(r)
		if err != nil {
			return fmt.Errorf("inflating %s: %w", relPath, err)
		}
		defer zr.Close()
		r = zr
	}

	dest := filepath.Join(destDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return err
}

// macPkgExpandWant is expandPkg's own real want filter (macpkgexpand_windows.go) -
// the top-level Distribution file (product-archive metadata KyoceraSelectivePackages/
// CanonCoreDevicePackages read directly) plus every component's own
// PackageInfo and Payload. Bom/Scripts are walked (as directory children,
// same as everything else) but never written to disk - see xarExtract's own
// doc comment for why that's safe.
func macPkgExpandWant(relPath string) bool {
	if relPath == "Distribution" {
		return true
	}
	switch path.Base(relPath) {
	case "PackageInfo", "Payload":
		return true
	default:
		return false
	}
}
