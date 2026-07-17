package einvoice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ErrorCode represents a single error code entry
type ErrorCode struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	Reason     string `json:"reason"`
	Resolution string `json:"resolution"`
}

var errorCodes []ErrorCode

func init() {
	// Find the package directory reliably
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return
	}
	dir := filepath.Dir(filename)
	filePath := filepath.Join(dir, "errorcode.json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Println("einvoice: failed to read errorcode.json:", err)
		return
	}

	if err := json.Unmarshal(data, &errorCodes); err != nil {
		fmt.Println("einvoice: failed to parse errorcode.json:", err)
		return
	}
}

// FindError looks up an error code and returns it if found
func FindError(code int) (*ErrorCode, bool) {
	for _, e := range errorCodes {
		if e.Code == code {
			return &e, true
		}
	}
	return nil, false
}
