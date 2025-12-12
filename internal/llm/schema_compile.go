package llm

import (
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

var intentSchema *jsonschema.Schema

func init() {
	compiler := jsonschema.NewCompiler()

	compiler.AddResource(
		"intent.schema.json",
		strings.NewReader(intentSchemaJSON),
	)

	var err error
	intentSchema, err = compiler.Compile("intent.schema.json")
	if err != nil {
		// Fallar en arranque es CORRECTO aquí
		panic(err)
	}
}
