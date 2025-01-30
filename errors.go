package autodemo

import (
	"errors"
	"fmt"
)

type MultiError []error

func (e MultiError) Error() string {
	return fmt.Sprintf("%d errors", len(e))
}

func (e MultiError) Unwrap() []error {
	return e
}

func (e *MultiError) Pop() error {
	if len(*e) == 0 {
		return nil
	}
	last := (*e)[len(*e)-1]
	(*e) = (*e)[:len(*e)-1]
	return last
}

func (e *MultiError) Push(err error) {
	(*e) = append(*e, err)
}

var IterDone = errors.New("iter done")
