package typedef

import (
	"fmt"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

func CreateParseContext(docContents []byte, cfg codegen.Configuration, specOptions *config.SpecOptions) (*codegen.ParseContext, []error) {
	doc, err := codegen.CreateDocument(docContents, cfg)
	if err != nil {
		return nil, []error{fmt.Errorf("error filtering document: %w", err)}
	}

	if specOptions == nil {
		specOptions = config.NewSpecOptions()
	}

	// Build model - optionally simplify it before parsing
	var optConfig *OptionalPropertyConfig
	if specOptions.Simplify && specOptions.OptionalProperties != nil {
		props := specOptions.OptionalProperties
		optConfig = &OptionalPropertyConfig{
			Min: props.Min,
			Max: props.Max,
		}
	}

	model, err := BuildModel(doc, specOptions.Simplify, optConfig)
	if err != nil {
		return nil, []error{fmt.Errorf("error building model: %w", err)}
	}

	res, err := codegen.CreateParseContextFromModel(model, cfg)
	if err != nil {
		return nil, []error{err}
	}
	return res, nil
}
