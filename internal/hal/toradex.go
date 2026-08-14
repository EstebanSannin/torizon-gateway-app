package hal

// toradex enriches the generic reads with Toradex-specific identity (module
// name from the device tree, serial from the Toradex EEPROM / `tdx-info`).
//
// SCAFFOLD: currently delegates health metrics to the generic reader and reads
// the device-tree model. Extend with:
//   - `tdx-info` parsing (module family, PID8, SoM version) once we mount/allow it
//   - Toradex EEPROM serial
//   - board-specific thermal zones per validated SoM (Verdin iMX8MP, etc.)
type toradex struct {
	generic // embed generic as the baseline
}

func newToradex() BoardInfo { return &toradex{} }

func (t *toradex) Kind() string { return "toradex" }

// Model prefers the device-tree model string (already Toradex-branded there).
func (t *toradex) Model() string {
	if v := firstLine(deviceTreeModelPath()); v != "" {
		return trimNull(v)
	}
	return t.generic.Model()
}

func trimNull(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return s[:i]
		}
	}
	return s
}
