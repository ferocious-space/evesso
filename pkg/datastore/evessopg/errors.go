package evessopg

import (
	"errors"
)

var (
	ErrUniqueViolation   = errors.New("Unable to insert or update resource because a resource with that value already exists")
	ErrConcurrentUpdate  = errors.New("Unable to serialize access due to a concurrent update in another session")
	ErrNoRows            = errors.New("Unable to locate the resource")
	ErrTranscationOpen   = errors.New("Transaction already exist in this context")
	ErrNoTranscationOpen = errors.New("no Transaction in this context")
)
