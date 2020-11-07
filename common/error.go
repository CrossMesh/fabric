package common

import (
	"errors"
	"fmt"
)

// Errors contains a set of errors.
type Errors []error

func (e Errors) Error() (s string) {
	if len(e) < 1 {
		return
	}

	for _, err := range e[:len(e)-1] {
		s += err.Error() + " => "
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

// ModelVersionUnmatchedError indicates that the version is invalid for current model structures.
type ModelVersionUnmatchedError struct {
	Actual, Expected uint16
	Name             string
}

func (e *ModelVersionUnmatchedError) Error() string {
	return fmt.Sprintf("%v version %v structure contains unmatched version %v", e.Name, e.Expected, e.Actual)
}

var ErrBrokenStream = errors.New("broken binary stream")
