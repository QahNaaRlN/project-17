package manifest

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed catalogue_gen.json
var builtinCatalogueJSON []byte

var builtinContracts = sync.OnceValue(func() map[string]json.RawMessage {
	var catalogue map[string]json.RawMessage
	if err := json.Unmarshal(builtinCatalogueJSON, &catalogue); err != nil {
		panic(err)
	}
	return catalogue
})

// BuiltinContract returns an independent copy of a builtin's fixed MVP contract
// (03 §2.1), or nil for a native/unknown type. No defaults are materialized in IR.
func BuiltinContract(name string) map[string]any {
	data, ok := builtinContracts()[name]
	if !ok {
		return nil
	}
	var contract map[string]any
	if err := json.Unmarshal(data, &contract); err != nil {
		panic(err)
	}
	return contract
}
