package contexts

// RequestPrefix marks a context value that is taken from the incoming request
// payload instead of being generated, e.g. `request:order.payment.currency`.
const RequestPrefix = "request:"

// RequestRef is the parsed form of a `request:` context value. It carries the
// dotted path into the request payload; the value itself can only be resolved
// per request, during generation.
type RequestRef struct {
	Path string
}

// HasRequestRefs reports whether any of the given contexts contains a RequestRef.
// Callers use it to skip reading and parsing request payloads nobody references.
func HasRequestRefs(data []map[string]any) bool {
	for _, ctx := range data {
		if mapHasRequestRef(ctx) {
			return true
		}
	}
	return false
}

func mapHasRequestRef(ctx map[string]any) bool {
	for _, value := range ctx {
		switch v := value.(type) {
		case RequestRef:
			return true
		case map[string]any:
			if mapHasRequestRef(v) {
				return true
			}
		}
	}
	return false
}
