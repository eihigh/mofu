package cubism

import (
	"encoding/json"
	"fmt"
	"os"
)

// Expression blend modes used by exp3.json.
const (
	BlendAdd       = "Add"
	BlendMultiply  = "Multiply"
	BlendOverwrite = "Overwrite"
)

// Expression3 is the contents of a .exp3.json file: a set of parameter
// adjustments applied on top of whatever motion is playing.
type Expression3 struct {
	Type        string                `json:"Type"`
	FadeInTime  *float64              `json:"FadeInTime"`
	FadeOutTime *float64              `json:"FadeOutTime"`
	Parameters  []ExpressionParameter `json:"Parameters"`
}

// ExpressionParameter adjusts one parameter.
type ExpressionParameter struct {
	Id    string  `json:"Id"`
	Value float64 `json:"Value"`
	// Blend is Add (the default when empty), Multiply or Overwrite.
	Blend string `json:"Blend"`
}

// Apply combines the adjustment with a current parameter value.
func (p *ExpressionParameter) Apply(current float64) float64 {
	switch p.Blend {
	case BlendMultiply:
		return current * p.Value
	case BlendOverwrite:
		return p.Value
	default: // Add
		return current + p.Value
	}
}

// LoadExpression3 reads and parses a .exp3.json file.
func LoadExpression3(path string) (*Expression3, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e Expression3
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &e, nil
}
