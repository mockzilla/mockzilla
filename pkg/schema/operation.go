package schema

type RouteInfo struct {
	ID     string
	Method string
	Path   string

	// IsStatic is true when at least one of this operation's responses
	// carries an `x-static-response` extension, i.e. the response body
	// comes from a static file overlay rather than the generator. The
	// UI uses this to badge overridden endpoints.
	IsStatic bool
}

// Operation represents an OpenAPI operation (endpoint).
type Operation struct {
	ID string `json:"id,omitempty"`

	// Content-Type of the request body (e.g., "application/json")
	ContentType string          `json:"contentType,omitempty"`
	Method      string          `json:"method,omitempty"`
	Path        string          `json:"path,omitempty"`
	PathParams  *Schema         `json:"pathParams,omitempty"`
	Query       QueryParameters `json:"query,omitempty"`
	Headers     *Schema         `json:"headers,omitempty"`
	Body        *Schema         `json:"body,omitempty"`

	// Encoding metadata for form fields
	BodyEncoding map[string]RequestBodyEncoding `json:"bodyEncoding,omitempty"`
	Response     *Response                      `json:"response,omitempty"`
}

// QueryParameter represents a single query parameter.
type QueryParameter struct {
	Schema   *Schema            `json:"schema,omitempty"`
	Required bool               `json:"required,omitempty"`
	Encoding *ParameterEncoding `json:"encoding,omitempty"`
}

// RequestBodyEncoding describes how one field of a form body is serialised.
type RequestBodyEncoding struct {
	ContentType string
	Style       string
	Explode     *bool
}

// ParameterEncoding describes how a query parameter is serialised.
type ParameterEncoding struct {
	Style         string
	Explode       *bool
	Required      bool
	AllowReserved bool
}

// QueryParameters is a map of parameter name to parameter info.
type QueryParameters map[string]*QueryParameter

// Response is a struct that represents an OpenAPI Response.
type Response struct {
	All         map[int]*ResponseItem `json:"all.omitempty"`
	SuccessCode int                   `json:"successCode,omitempty"`
}

// GetSuccess returns the success response.
func (r *Response) GetSuccess() *ResponseItem {
	return r.All[r.SuccessCode]
}

// GetResponse returns the response for the given status code.
func (r *Response) GetResponse(code int) *ResponseItem {
	return r.All[code]
}

// NewResponse creates a new Response instance.
func NewResponse(all map[int]*ResponseItem, successCode int) *Response {
	return &Response{
		All:         all,
		SuccessCode: successCode,
	}
}

// ResponseItem represents a single response for a specific status code.
type ResponseItem struct {
	Headers     map[string]*Schema `json:"headers,omitempty"`
	Content     *Schema            `json:"content,omitempty"`
	ContentType string             `json:"contentType,omitempty"`
	StatusCode  int                `json:"statusCode,omitempty"`
}
