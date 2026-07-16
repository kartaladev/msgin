// Package memory is an in-process, at-most-once msgin adapter backed by a Go
// channel. It carries live Go values (no payload codec) and is the reference
// adapter and the test double for the core.
package memory
