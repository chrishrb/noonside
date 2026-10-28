package httpapi

import (
	_ "embed"
	"net/http"
)

//go:embed api.yaml
var openapiYAML []byte

// HandleOpenAPISpec serves the OpenAPI specification in YAML format.
func HandleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapiYAML)
}
