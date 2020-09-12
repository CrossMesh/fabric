package common

// Errors contains a set of errors.
type Errors []error

func (e Errors) Error() (s string) {
	if len(e) < 1 {
		return
	}

	for _, err := range e[:len(e)-1] {
		s += err.Error() + "=>"
	}
	s += e[len(e)-1].Error()

	return
}

// Trace appends an err to Errors.
func (e *Errors) Trace(err error) {
	if err == nil {
		return
	}
	*e = append(*e, err)
}

// AsError presents itself as a normal error.
func (e Errors) AsError() error {
	switch {
	case len(e) < 1:
		return nil
	case len(e) == 1:
		return e[0]
	}
	return e
}
