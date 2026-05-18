package schema

// Discriminator describes a discriminator for oneOf/anyOf schemas.
// It maps discriminator values to the property names that should be used.
type Discriminator struct {
	// PropertyName is the JSON property name that holds the discriminator value
	PropertyName string

	// Mapping maps discriminator values to property names in the schema
	// For example: {"simple": "nodeType", "compound": "nodeType"}
	// The keys are the valid discriminator values
	Mapping map[string]string
}

// Schema is a struct that represents an OpenAPI schema.
// It is compatible with all versions of OpenAPI.
// All schema provider should implement the Document and KinOperation interfaces.
// This provides a unified way to work with different OpenAPI parsers.
type Schema struct {
	Type string `yaml:"type,omitempty"`

	// in 3.1 examples can be an array (which is recommended)
	Examples []any `yaml:"examples,omitempty"`

	// items can be a schema in 2.0, 3.0 and 3.1 or a bool in 3.1
	Items *Schema `yaml:"items,omitempty"`

	// Compatible with all versions
	MultipleOf           *float64           `yaml:"multipleOf,omitempty"`
	Maximum              *float64           `yaml:"maximum,omitempty"`
	ExclusiveMaximum     *float64           `yaml:"exclusiveMaximum,omitempty"`
	Minimum              *float64           `yaml:"minimum,omitempty"`
	ExclusiveMinimum     *float64           `yaml:"exclusiveMinimum,omitempty"`
	MaxLength            *int64             `yaml:"maxLength,omitempty"`
	MinLength            *int64             `yaml:"minLength,omitempty"`
	Pattern              string             `yaml:"pattern,omitempty"`
	Format               string             `yaml:"format,omitempty"`
	MaxItems             *int64             `yaml:"maxItems,omitempty"`
	MinItems             *int64             `yaml:"minItems,omitempty"`
	MaxProperties        *int64             `yaml:"maxProperties,omitempty"`
	MinProperties        *int64             `yaml:"minProperties,omitempty"`
	Required             []string           `yaml:"required,omitempty"`
	Enum                 []any              `yaml:"enum,omitempty"`
	Properties           map[string]*Schema `yaml:"properties,omitempty"`
	Default              any                `yaml:"default,omitempty"`
	Nullable             bool               `yaml:"nullable,omitempty"`
	ReadOnly             bool               `yaml:"readOnly,omitempty"`
	WriteOnly            bool               `yaml:"writeOnly,omitempty"`
	Example              any                `yaml:"example,omitempty"`
	Deprecated           bool               `yaml:"deprecated,omitempty"`
	AdditionalProperties *Schema            `yaml:"additionalProperties,omitempty"`

	// AdditionalPropertiesForbidden is true when the spec declares
	// `additionalProperties: false` (as a bool, not a schema). The
	// generator uses this to refuse synthesising properties that
	// aren't in the declared properties map - notably the placeholder
	// fill for `required` names that have no matching `properties`
	// entry, which would otherwise produce a body the validator
	// rejects for "additional properties not allowed".
	AdditionalPropertiesForbidden bool `yaml:"-" json:"-"`

	// Discriminator describes the discriminator for oneOf/anyOf schemas.
	// When set, the generator should use one of the valid discriminator values
	// for the discriminator property instead of generating a random value.
	Discriminator *Discriminator `yaml:"-" json:"-"`

	// Recursive indicates this schema was truncated due to circular reference.
	// The content generator should return nil for such schemas.
	Recursive bool `yaml:"-" json:"-"`

	// StaticContent holds pre-rendered content for static responses.
	// When set, the generator should return this content directly instead of generating.
	// This is used for static services where responses are pre-defined files.
	StaticContent string `yaml:"-" json:"-"`
}
