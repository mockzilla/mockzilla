package libopenapi

import "runtime/debug"

// Lift the per-goroutine stack limit to the Go runtime's effective
// 64-bit ceiling. pb33f's recursive *SchemaProxy.Schema() resolution
// grows the stack linearly with schema depth; the 1 GB default
// overflows on multi-megabyte specs with mutually recursive
// components. Specs whose graphs need more than ~2 GB of stack are
// excluded via the leading-"-" convention.
func init() {
	debug.SetMaxStack(2 << 30)
}
