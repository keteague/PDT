package windows

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// copyUTF16 copies s into a fixed-size UTF-16 field, failing loudly rather
// than silently truncating if it doesn't fit (better than a corrupted
// IP/host/port name reaching the spooler unnoticed).
func copyUTF16(dst []uint16, s string) error {
	u16, err := windows.UTF16FromString(s)
	if err != nil {
		return fmt.Errorf("string %q contains an embedded NUL", s)
	}
	if len(u16) > len(dst) {
		return fmt.Errorf("string %q is too long for this field (max %d characters)", s, len(dst)-1)
	}
	copy(dst, u16)
	return nil
}

// AddStandardTcpIpPort creates a Standard TCP/IP port via XcvData "AddPort"
// against the "Standard TCP/IP Port" monitor. There is no PowerShell-cmdlet
// internals to port from here - the original tool relied on the
// Win32_TCPIPPrinterPort WMI class instead, a completely different mechanism
// - so this is built directly from Microsoft's TCPMON Xcv Commands and
// PORT_DATA_1 documentation. portNumber 9100 is the standard raw TCP/IP
// print port; snmpEnabled/snmpCommunity mirror the grid's own SNMP checkbox.
func AddStandardTcpIpPort(portName, hostAddress string, portNumber uint32, snmpEnabled bool, snmpCommunity string) error {
	h, err := openXcvMonitor("Standard TCP/IP Port")
	if err != nil {
		return err
	}
	defer procClosePrinter.Call(uintptr(h))

	var pd PortData1
	if err := copyUTF16(pd.PortName[:], portName); err != nil {
		return err
	}
	pd.Version = 1
	pd.Protocol = ProtocolRawTcpType
	pd.CbSize = uint32(unsafe.Sizeof(pd))
	if err := copyUTF16(pd.HostAddress[:], hostAddress); err != nil {
		return err
	}
	if err := copyUTF16(pd.IPAddress[:], hostAddress); err != nil {
		return err
	}
	pd.PortNumber = portNumber
	if snmpEnabled {
		pd.SNMPEnabled = 1
		community := snmpCommunity
		if community == "" {
			community = "public"
		}
		if err := copyUTF16(pd.SNMPCommunity[:], community); err != nil {
			return err
		}
	}

	status, err := xcvDataRaw(h, "AddPort", unsafe.Pointer(&pd), unsafe.Sizeof(pd))
	runtime.KeepAlive(pd)
	if err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("XcvData(AddPort, %s): %w", portName, syscall.Errno(status))
	}
	return nil
}

// DeleteStandardTcpIpPort removes a Standard TCP/IP port via XcvData
// "DeletePort".
func DeleteStandardTcpIpPort(portName string) error {
	h, err := openXcvMonitor("Standard TCP/IP Port")
	if err != nil {
		return err
	}
	defer procClosePrinter.Call(uintptr(h))

	var dd DeletePortData1
	if err := copyUTF16(dd.PortName[:], portName); err != nil {
		return err
	}
	dd.Version = 1

	status, err := xcvDataRaw(h, "DeletePort", unsafe.Pointer(&dd), unsafe.Sizeof(dd))
	runtime.KeepAlive(dd)
	if err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("XcvData(DeletePort, %s): %w", portName, syscall.Errno(status))
	}
	return nil
}

// EnumLocalPortNames lists every local port name (winspool's EnumPorts, not
// part of the Xcv interface) - used here to verify AddStandardTcpIpPort/
// EnsureNulPort actually took effect, and later for UseExistingPort's
// reuse-by-name lookups.
func EnumLocalPortNames() ([]string, error) {
	var needed, returned uint32
	procEnumPortsW.Call(0, 1, 0, 0, uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if needed == 0 {
		return nil, nil
	}
	buf := make([]byte, needed)
	r1, _, callErr := procEnumPortsW.Call(0, 1, uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if r1 == 0 {
		return nil, fmt.Errorf("EnumPorts: %w", callErr)
	}
	names := make([]string, 0, returned)
	entrySize := unsafe.Sizeof(PortInfo1{})
	for i := uint32(0); i < returned; i++ {
		info := (*PortInfo1)(unsafe.Pointer(&buf[uintptr(i)*entrySize]))
		if info.PortName != nil {
			names = append(names, windows.UTF16PtrToString(info.PortName))
		}
	}
	return names, nil
}
