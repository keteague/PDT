package windows

import "runtime"

// SetAdvancedPrintingFeatures ports the already-proven PrinterAttributeHelper
// C# logic from Create-Printers.ps1: "Enable advanced printing features" on
// the printer's Advanced tab corresponds to PRINTER_ATTRIBUTE_RAW_ONLY being
// CLEARED (off); disabling it means the bit is SET (on) - an inverted
// relationship confirmed against Microsoft's PRINTER_INFO_2 documentation
// during the original tool's development, after an earlier wrong guess
// (PRINTER_ATTRIBUTE_ENABLE_DEVQ) was disproven.
func SetAdvancedPrintingFeatures(name string, enable bool) error {
	p, err := OpenPrinter(name, PrinterAllAccess)
	if err != nil {
		return err
	}
	defer p.Close()

	info, err := p.GetInfo2()
	if err != nil {
		return err
	}

	if enable {
		info.Info.Attributes &^= PrinterAttributeRawOnly
	} else {
		info.Info.Attributes |= PrinterAttributeRawOnly
	}

	err = p.SetInfo2(info)
	runtime.KeepAlive(info)
	return err
}
