package windows

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const tcpipPortsRegPath = `SYSTEM\CurrentControlSet\Control\Print\Monitors\Standard TCP/IP Port\Ports`

// FindTcpIpPortByHost looks for an already-configured Standard TCP/IP port
// targeting hostAddress, by reading every port's "HostName" value directly
// from the registry (confirmed on a real machine: each port is a subkey of
// tcpipPortsRegPath, named after itself, with its target address in a
// "HostName" string value) - the direct Win32-world equivalent of the
// original tool's Win32_TCPIPPrinterPort.HostAddress WMI lookup, used both
// for UseExistingPort's reuse requirement and to avoid ever creating a
// genuine duplicate port for a host that already has one on redeploy.
func FindTcpIpPortByHost(hostAddress string) (portName string, found bool, err error) {
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, tcpipPortsRegPath, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	defer root.Close()

	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return "", false, err
	}

	for _, name := range names {
		sub, openErr := registry.OpenKey(registry.LOCAL_MACHINE, tcpipPortsRegPath+`\`+name, registry.QUERY_VALUE)
		if openErr != nil {
			continue
		}
		host, _, valErr := sub.GetStringValue("HostName")
		sub.Close()
		if valErr != nil {
			continue
		}
		if strings.EqualFold(host, hostAddress) {
			return name, true, nil
		}
	}
	return "", false, nil
}
