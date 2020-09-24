package common

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestError(t *testing.T) {
	var errs Errors

	assert.NoError(t, errs.AsError())
	err1 := errors.New("error1")
	errs.Trace(err1)
	assert.Equal(t, err1, errs.AsError())
	err2 := errors.New("error2")
	errs.Trace(err2)
	assert.Equal(t, errs, errs.AsError())
}
