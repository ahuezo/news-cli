package catalogschema

import (
	_ "embed"
	"fmt"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed sources.schema.json
var sourcesSchema string

//go:embed auth_sources.schema.json
var authSourcesSchema string

//go:embed categories.schema.json
var categoriesSchema string

func schemaForKind(kind string) (string, error) {
	switch kind {
	case "sources":
		return sourcesSchema, nil
	case "categories":
		return categoriesSchema, nil
	case "auth":
		return authSourcesSchema, nil
	default:
		return "", fmt.Errorf("unsupported schema kind %q", kind)
	}
}

func ValidateFile(kind, path string) ([]string, error) {
	schemaText, err := schemaForKind(kind)
	if err != nil {
		return nil, err
	}

	result, err := gojsonschema.Validate(
		gojsonschema.NewStringLoader(schemaText),
		gojsonschema.NewReferenceLoader("file://"+path),
	)
	if err != nil {
		return nil, err
	}
	if result.Valid() {
		return nil, nil
	}

	issues := make([]string, 0, len(result.Errors()))
	for _, issue := range result.Errors() {
		issues = append(issues, issue.String())
	}
	return issues, nil
}
