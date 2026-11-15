package read

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// invalidDocument is the error for a file that cannot be opened as a
// workbook at all (doc.go lists the cases).
func invalidDocument(msg string, cause error) error {
	e := &cli.Error{Code: cli.CodeInvalidDocument, Message: msg, Cause: cause}
	if cause != nil {
		e.Detail = cause.Error()
	}
	return e
}

// unreadable is the error for a targeted sheet whose part cannot be read.
func unreadable(path, sheet string, cause error) error {
	e := &cli.Error{Code: cli.CodeUnreadableTarget, Path: path, Message: "sheet " + sheet + " cannot be read", Cause: cause}
	if cause != nil {
		e.Detail = cause.Error()
	}
	return e
}

// notFound is the error for a sheet, name, table or column the workbook
// does not have; what is the address as the agent typed it.
func notFound(path, what string) error {
	return &cli.Error{Code: cli.CodeNotFound, Path: path, Message: "the workbook has no " + what}
}

// badAddress maps a model.AddressError (or any address problem) to
// invalid_operation at path.
func badAddress(path string, err error) error {
	e := &cli.Error{Code: cli.CodeInvalidOperation, Path: path, Message: "the address cannot be used", Cause: err}
	var ae *model.AddressError
	if errors.As(err, &ae) {
		e.Detail = ae.Reason
	} else if err != nil {
		e.Detail = err.Error()
	}
	return e
}
