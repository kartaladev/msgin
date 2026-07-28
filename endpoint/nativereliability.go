package endpoint

// noNativeReliability is the NativeReliability default for sources that do not
// implement the optional capability (e.g. memory): neither native redelivery
// nor native dead-letter. Using a value (never nil) upholds NF-11 — the runtime
// never nil-calls the capability.
type noNativeReliability struct{}

func (noNativeReliability) NativeRedelivery() bool { return false }
func (noNativeReliability) NativeDeadLetter() bool { return false }
