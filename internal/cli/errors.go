package cli

import "errors"

type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string    { return e.Err.Error() }
func (e ExitError) Unwrap() error    { return e.Err }
func fail(code int, err error) error { return ExitError{Code: code, Err: err} }
func codeOf(err error) int {
	var e ExitError
	if errors.As(err, &e) {
		return e.Code
	}
	return 1
}
