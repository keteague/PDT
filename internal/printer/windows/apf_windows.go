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

// SetPrintSpooledDocumentsFirst toggles the printer's Advanced tab radio
// option between "Print spooled documents first" (PRINTER_ATTRIBUTE_DO_COMPLETE_FIRST
// set) and "Start printing after last page is spooled" (cleared). Deploy
// always requests enable=true for every printer it creates or updates - this
// isn't a per-row setting like APF, just a fixed default every deployment
// should get, matching PRINTER_INFO_2's own default of "off" only ever being
// desirable when nothing has explicitly asked for the other behavior.
func SetPrintSpooledDocumentsFirst(name string, enable bool) error {
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
		info.Info.Attributes |= PrinterAttributeDoCompleteFirst
	} else {
		info.Info.Attributes &^= PrinterAttributeDoCompleteFirst
	}

	err = p.SetInfo2(info)
	runtime.KeepAlive(info)
	return err
}
