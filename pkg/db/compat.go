package db

// What a driver stores into, which decides whether a version means anything.
const (
	// KindServer is a server an operator runs and upgrades, so its version matters.
	KindServer = "server"

	// KindService is a hosted service with no version an operator can pick.
	KindService = "service"

	// KindEmbedded runs inside the process, so the library version is the only one there is.
	KindEmbedded = "embedded"
)

// Compat is what a driver says about where it runs and how to point it there.
// A caller reads it to tell an operator what a build supports before anything connects,
// so nothing in it needs a connection to fill in.
type Compat struct {
	// Kind is one of the constants above.
	Kind string `json:"kind"`

	// Min is the oldest server version the driver supports. Empty when the kind has no version.
	Min string `json:"min,omitempty"`

	// Tested are the versions this driver's own tests run against. A claim beyond them is a guess,
	// so the list is generated from what the test matrix runs rather than written by hand.
	Tested []string `json:"tested,omitempty"`

	// Connect is the one thing a list of settings cannot say: which of them replace each other.
	Connect string `json:"connect,omitempty"`

	// Notes is anything else an operator has to know before choosing this driver.
	Notes string `json:"notes,omitempty"`

	Settings []Setting `json:"settings,omitempty"`
}

// Setting is one configuration field of a driver, as an operator sets it.
type Setting struct {
	Env     string `json:"env"`
	YAML    string `json:"yaml"`
	Default string `json:"default,omitempty"`
	Doc     string `json:"doc,omitempty"`

	// IsSensitive keeps a value out of anything that displays or writes an example.
	IsSensitive bool `json:"sensitive,omitempty"`
}
