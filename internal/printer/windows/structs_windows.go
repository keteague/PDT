package windows

// Win32 structs used across the winspool.drv/DEVMODE syscall surface. Field
// names follow Go convention (no Hungarian prefix) but field order and types
// are laid out to match the real C structs byte-for-byte - see structs_test.go
// for unsafe.Sizeof assertions confirming that. Every string field is a raw
// UTF-16 pointer (*uint16) since Go has no automatic string marshaling for
// syscalls; callers must keep the underlying UTF16PtrFromString result alive
// (runtime.KeepAlive) until after the Win32 call returns.

const (
	// Access rights (winspool.drv / accctrl.h).
	PrinterAllAccess = 0x000F000C
	PrinterAccessUse = 0x00000008

	// PRINTER_INFO_2.Attributes bits actually used here.
	PrinterAttributeRawOnly         = 0x00001000
	PrinterAttributeDoCompleteFirst = 0x00000200

	// DEVMODE.Fields bits for the two fields this tool edits.
	DmOrientation = 0x00000001
	DmColor       = 0x00000200
	DmDuplex      = 0x00001000

	// DEVMODE.Color values.
	DmColorMonochrome = 1
	DmColorColor      = 2

	// DEVMODE.Duplex values.
	DmDupSimplex  = 1
	DmDupVertical = 2
	DmDupHorizontal = 3

	// SetPrinter Command values (0 = just apply the Level-2 info supplied).
	printerControlNone = 0
)

// DevMode mirrors DEVMODEW (wingdi.h). Total size must be 220 bytes on
// amd64/arm64 - see TestDevModeSize.
type DevMode struct {
	DeviceName    [32]uint16
	SpecVersion   uint16
	DriverVersion uint16
	Size          uint16
	DriverExtra   uint16
	Fields        uint32

	Orientation   int16
	PaperSize     int16
	PaperLength   int16
	PaperWidth    int16
	Scale         int16
	Copies        int16
	DefaultSource int16
	PrintQuality  int16

	Color      int16
	Duplex     int16
	YResolution int16
	TTOption   int16
	Collate    int16

	FormName [32]uint16

	LogPixels        uint16
	BitsPerPel       uint32
	PelsWidth        uint32
	PelsHeight       uint32
	DisplayFlags     uint32
	DisplayFrequency uint32
	ICMMethod        uint32
	ICMIntent        uint32
	MediaType        uint32
	DitherType       uint32
	Reserved1        uint32
	Reserved2        uint32
	PanningWidth     uint32
	PanningHeight    uint32
}

// PrinterDefaults mirrors PRINTER_DEFAULTSW.
type PrinterDefaults struct {
	DatatypePtr   *uint16
	DevModePtr    uintptr
	DesiredAccess uint32
}

// tcpxcv.h constants (Standard TCP/IP port monitor's Xcv data structures).
const (
	MaxPortNameLen         = 64
	MaxNetworkNameLen      = 49
	MaxSnmpCommunityStrLen = 33
	MaxQueueNameLen        = 33
	MaxIPAddrStrLen        = 16

	ProtocolRawTcpType = 1
	ProtocolLprType    = 2
)

// PortData1 mirrors tcpxcv.h's PORT_DATA_1 - the input (AddPort/ConfigPort)
// and output (GetConfigInfo) structure for the Standard TCP/IP port
// monitor's Xcv commands. Field order/types match the documented C struct
// exactly so Go's automatic alignment reproduces the same padding (notably 2
// bytes before PortNumber, after the 540-byte Reserved block) - see
// TestPortData1Size.
type PortData1 struct {
	PortName      [MaxPortNameLen]uint16
	Version       uint32
	Protocol      uint32
	CbSize        uint32
	Reserved0     uint32
	HostAddress   [MaxNetworkNameLen]uint16
	SNMPCommunity [MaxSnmpCommunityStrLen]uint16
	DoubleSpool   uint32
	Queue         [MaxQueueNameLen]uint16
	IPAddress     [MaxIPAddrStrLen]uint16
	Reserved1     [540]byte
	PortNumber    uint32
	SNMPEnabled   uint32
	SNMPDevIndex  uint32
}

// DeletePortData1 mirrors tcpxcv.h's DELETE_PORT_DATA_1.
type DeletePortData1 struct {
	PortName  [MaxPortNameLen]uint16
	Reserved0 [98]byte
	Version   uint32
	Reserved1 uint32
}

// PortInfo1 mirrors PORT_INFO_1W, the level EnumPorts uses here (just the
// port name - enough to confirm a port by that name exists after AddPort).
type PortInfo1 struct {
	PortName *uint16
}

// PrinterInfo2 mirrors PRINTER_INFO_2W - the level used for create/read/update
// throughout this package.
type PrinterInfo2 struct {
	ServerName         *uint16
	PrinterName        *uint16
	ShareName          *uint16
	PortName           *uint16
	DriverName         *uint16
	Comment            *uint16
	Location           *uint16
	DevMode            uintptr
	SepFile            *uint16
	PrintProcessor     *uint16
	Datatype           *uint16
	Parameters         *uint16
	SecurityDescriptor uintptr
	Attributes         uint32
	Priority           uint32
	DefaultPriority    uint32
	StartTime          uint32
	UntilTime          uint32
	Status             uint32
	CJobs              uint32
	AveragePPM         uint32
}
