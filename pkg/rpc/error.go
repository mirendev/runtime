package rpc

type ErrorCategory interface {
	ErrorCategory() string
}

type ErrorCode interface {
	ErrorCode() string
}

type ErrorMessage interface {
	ErrorMessage() string
}
