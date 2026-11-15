package derive

// Origin labels where a value came from.
type Origin string

// Origins. A value stored on the object itself is FromSlide.
const (
	FromSlide    Origin = "slide"
	FromLayout   Origin = "layout"
	FromMaster   Origin = "master"
	FromTheme    Origin = "theme"
	FromStyle    Origin = "style"        // the object's p:style, through the theme's format scheme
	FromPres     Origin = "presentation" // presentation defaultTextStyle
	FromGroup    Origin = "group"        // a:grpFill: the enclosing group's fill
	FromSpecDflt Origin = "spec"         // the schema default of an attribute (ECMA-376)
)

// Val is a value read from a document: either the value with where it came
// from, or absent with the reason it could not be derived. Readers return
// a Val instead of substituting a default; the caller records the reason
// as an omission. The zero Val is absent.
type Val[T any] struct {
	v      T
	ok     bool
	origin Origin
	reason string
}

// Got returns a present value.
func Got[T any](v T, origin Origin) Val[T] { return Val[T]{v: v, ok: true, origin: origin} }

// Missing returns an absent value with the reason.
func Missing[T any](reason string) Val[T] { return Val[T]{reason: reason} }

// OK reports whether the value is present.
func (v Val[T]) OK() bool { return v.ok }

// Get returns the value and whether it is present. An absent value
// returns T's zero value, which must not be shown.
func (v Val[T]) Get() (T, bool) { return v.v, v.ok }

// Origin returns where a present value came from, "" if absent.
func (v Val[T]) Origin() Origin { return v.origin }

// Reason returns why the value is absent, "" if present.
func (v Val[T]) Reason() string {
	if v.ok {
		return ""
	}
	if v.reason == "" {
		return "not derived"
	}
	return v.reason
}
