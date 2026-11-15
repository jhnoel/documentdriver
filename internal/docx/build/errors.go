package build

import (
	"errors"
	"fmt"
)

// ErrContent is wrapped by every refusal of the content itself (what the
// HTML asks for cannot be built here): the operation reports it as
// invalid_operation on its html field. Other errors are failures of the
// engine.
var ErrContent = errors.New("html")

// contentErr returns a refusal of the content with a formatted message.
func contentErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrContent, fmt.Sprintf(format, args...))
}

// ElementError is content the lowering does not build yet, named by its
// HTML element ("table", "ol", "img", "footnote"). It is a refusal of the
// content (errors.Is ErrContent).
type ElementError struct {
	Element, Msg string
}

// Error returns the message.
func (e *ElementError) Error() string { return "<" + e.Element + ">: " + e.Msg }

// Is makes an ElementError a refusal of the content.
func (e *ElementError) Is(target error) bool { return target == ErrContent }

// unsupported returns an ElementError saying the element is not supported here yet.
func unsupported(element string) error {
	return &ElementError{Element: element, Msg: "not supported here yet"}
}

// RefError is an item whose data-ref names nothing in the content being
// replaced (contract.md section 7: not_found).
type RefError struct {
	// Ref is the address the item carried.
	Ref string
}

// Error returns the message.
func (e *RefError) Error() string {
	return "data-ref " + e.Ref + " names nothing in the content being replaced"
}

// ErrStrict is returned for an equation in a Strict document: the
// converter writes transitional Office Math only.
var ErrStrict = errors.New("build: new equations cannot be written into a Strict document")

// AttributeError is a refusal of an attribute's value, named by the
// attribute ("style"). It is a refusal of the content (errors.Is
// ErrContent).
type AttributeError struct {
	Attr, Msg string
}

// Error returns the message.
func (e *AttributeError) Error() string { return e.Attr + ": " + e.Msg }

// Is makes an AttributeError a refusal of the content.
func (e *AttributeError) Is(target error) bool { return target == ErrContent }

// styleErr refuses a table, row or cell style.
func styleErr(format string, args ...any) error {
	return &AttributeError{Attr: "style", Msg: fmt.Sprintf(format, args...)}
}
