// Package manifest parses capability manifests — the small JSON sidecar
// each capability module ships so eichec can type-check "::" calls against
// it without loading any WASM. Shape is frozen by the brief:
//
//	{
//	  "name": "eiche-payroll",
//	  "version": "1.0.0",
//	  "abi": 1,
//	  "exports": {
//	    "isCoherent": { "args": ["string", "int", "int", "float"], "returns": "bool" }
//	  }
//	}
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
)

type Export struct {
	Args    []string `json:"args"`
	Returns string   `json:"returns"`
}

type Manifest struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	ABI     int               `json:"abi"`
	Exports map[string]Export `json:"exports"`
}

func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Name == "" {
		return nil, fmt.Errorf("parse manifest: missing required \"name\"")
	}
	return &m, nil
}

func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load manifest %s: %w", path, err)
	}
	return Parse(data)
}
