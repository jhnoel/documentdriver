package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

// The error codes of the contract (docs/contract.md, section 2).
const (
	CodeUsage            = "usage"
	CodeIO               = "io"
	CodeInvalidJSON      = "invalid_json"
	CodeInvalidPlan      = "invalid_plan"
	CodeInvalidOperation = "invalid_operation"
	CodeInvalidDocument  = "invalid_document"
	CodeStaleBase        = "stale_base"
	CodeNotFound         = "not_found"
	CodeAmbiguous        = "ambiguous"
	CodeRefusedInMode    = "refused_in_mode"
	CodeUnreadableTarget = "unreadable_target"
	CodeOperationFailed  = "operation_failed"
	CodeInvalidOutput    = "invalid_output"
	CodeInternal         = "internal"
)

// Exit codes.
const (
	ExitOK     = 0
	ExitFailed = 1
	ExitUsage  = 2
)

// Span is a range of the plan text: 1-based lines and columns, columns
// counted in characters.
type Span struct {
	Line      int `json:"line"`
	Column    int `json:"column"`
	EndLine   int `json:"end_line"`
	EndColumn int `json:"end_column"`
}

// Error is one entry of the errors object.
type Error struct {
	// Code is one of the Code constants.
	Code string `json:"code"`
	// Path names what is wrong: a plan path or a flag.
	Path string `json:"path,omitempty"`
	// Message is one sentence for a reader.
	Message string `json:"message"`
	// Hint is the next step that usually fixes it.
	Hint string `json:"hint,omitempty"`
	// Span is where in the plan text.
	Span *Span `json:"span,omitempty"`
	// Matches is the number of matches of an ambiguous selection.
	Matches int `json:"matches,omitempty"`
	// Detail says what would be introduced, what is wrong with the
	// target, or what was left out.
	Detail string `json:"detail,omitempty"`
	// Cause is the error this entry reports, for errors.Is; not printed.
	Cause error `json:"-"`
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Cause }

// Error returns the code, path and message.
func (e *Error) Error() string {
	if e.Path == "" {
		return e.Code + ": " + e.Message
	}
	return e.Code + " " + e.Path + ": " + e.Message
}

// Errorf returns an Error.
func Errorf(code, path, format string, args ...any) *Error {
	return &Error{Code: code, Path: path, Message: fmt.Sprintf(format, args...)}
}

// WithHint returns e with its hint set.
func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
	return e
}

// Errors is several errors reported together (the document-independent
// checks of a plan).
type Errors []*Error

// Unwrap returns the entries, so that errors.Is and errors.As see them.
func (es Errors) Unwrap() []error {
	out := make([]error, len(es))
	for i, e := range es {
		out[i] = e
	}
	return out
}

// Error joins the messages.
func (es Errors) Error() string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

// AsErrors turns any error into the entries to report. An error that is
// neither an *Error nor an Errors is a file error when the file system
// says so and an internal error otherwise: callers classify what they
// know with Errorf or Wrap.
func AsErrors(err error) Errors {
	var es Errors
	if errors.As(err, &es) && len(es) > 0 {
		return es
	}
	var e *Error
	if errors.As(err, &e) {
		return Errors{e}
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return Errors{{Code: CodeIO, Message: err.Error()}}
	}
	return Errors{{Code: CodeInternal, Message: err.Error()}}
}

// Wrap classifies err under code, keeping an error that already is one.
func Wrap(code, path string, err error) error {
	if err == nil {
		return nil
	}
	var es Errors
	var e *Error
	if errors.As(err, &es) || errors.As(err, &e) {
		return err
	}
	return &Error{Code: code, Path: path, Message: err.Error(), Cause: err}
}

// ExitCode is the exit code for the entries: 2 when the command line is
// wrong, 1 otherwise.
func (es Errors) ExitCode() int {
	for _, e := range es {
		if e.Code != CodeUsage {
			return ExitFailed
		}
	}
	return ExitUsage
}

// WriteErrors writes the errors object, the only thing a failed command
// puts on stderr, and returns the exit code.
func WriteErrors(stderr io.Writer, err error) int {
	es := AsErrors(err)
	b, merr := marshal(struct {
		Errors Errors `json:"errors"`
	}{es})
	if merr != nil {
		b = []byte(`{"errors":[{"code":"internal","message":"cannot encode the errors"}]}`)
	}
	_, _ = stderr.Write(append(b, '\n')) //nolint:errcheck // nothing to do when stderr is gone
	return es.ExitCode()
}

// marshal is json.Marshal without HTML escaping.
func marshal(v any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(b.String(), "\n")), nil
}
