package profile

// Snapshot contains leak samples from one profile source.
type Snapshot struct {
	Source string
	Leaks  []Leak
}

// Leak represents one positive-count sample. Stack is ordered leaf-to-root.
type Leak struct {
	Count  int64
	Stack  []Frame
	Labels LabelSet
}

// Frame is a symbolized logical stack frame. Inlined is true for an inline
// callee and false for the outermost frame represented by a profile location.
type Frame struct {
	Function string
	File     string
	Line     int64
	Inlined  bool
}

// LabelSet maps each label key to its string values.
type LabelSet map[string][]string
