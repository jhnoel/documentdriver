package ops

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// bookmarkNameRule is docxdriver's bookmark name rule as its messages
// state it.
const bookmarkNameRule = `a bookmark name starts with a letter, continues with letters, digits or underscores, and has at most 40 characters (for example "Clause_4_2")`

// alphabetic is Rust's char::is_alphabetic (the Alphabetic property).
func alphabetic(r rune) bool {
	return unicode.IsLetter(r) || unicode.In(r, unicode.Nl, unicode.Other_Alphabetic)
}

// newBookmarkName is bookmarks::validate_new_name: the message for a name
// an operation would create, or "".
func newBookmarkName(name, field string) string {
	if strings.HasPrefix(name, "_") {
		return fmt.Sprintf(`%s %s starts with "_", which marks a hidden or reserved bookmark (_GoBack, _Toc…); choose a name that starts with a letter`, field, strconv.Quote(name))
	}
	valid := name != ""
	for i, r := range name {
		ok := alphabetic(r)
		if i > 0 {
			ok = ok || unicode.IsNumber(r) || r == '_'
		}
		valid = valid && ok
	}
	if !valid {
		var hint string
		switch {
		case name == "":
			hint = "it is empty"
		case strings.Contains(name, " "):
			hint = "use underscores instead of spaces, e.g. " + strconv.Quote(strings.ReplaceAll(name, " ", "_"))
		case name[0] >= '0' && name[0] <= '9':
			hint = "it starts with a digit"
		default:
			hint = "it contains a character other than a letter, digit or underscore"
		}
		return fmt.Sprintf("%s %s is not a valid bookmark name: %s; %s", field, strconv.Quote(name), hint, bookmarkNameRule)
	}
	if n := utf8.RuneCountInString(name); n > 40 {
		return fmt.Sprintf("%s %s has %d characters; %s", field, strconv.Quote(name), n, bookmarkNameRule)
	}
	return ""
}

const existingBookmarkMsg = `at must name an existing bookmark as bookmark:Name; read --kind bookmarks lists them`

// newNameRule returns the rule checking a new bookmark name field.
func newNameRule[T any](field string, name func(*T) string, example string) Rule {
	return ruleX(field+` "…" starts with "_" … | `+field+` "…" is not a valid bookmark name: … | `+field+` "…" has N characters; …`,
		func(t *T) (string, string, bool) {
			msg := newBookmarkName(name(t), field)
			return msg, field, msg != ""
		}, example)
}

// existingNameRule returns the rule checking an existing bookmark address.
func existingNameRule[T any](at func(*T) BookmarkAddress, example string) Rule {
	return rule(existingBookmarkMsg, "at", func(t *T) bool { _, ok := at(t).Name(); return !ok }, example)
}

// selectRules is validate_bookmark_select.
func selectRules[T any](sel func(*T) (*string, *uint32), op, head string) []Rule {
	return []Rule{
		rule("select must be non-empty; omit it to bookmark the whole paragraph", "select",
			func(t *T) bool { s, _ := sel(t); return s != nil && *s == "" },
			fmt.Sprintf(`{"op":%q,%s,"select":""}`, op, head)),
		rule("occurrence must be positive", "occurrence",
			func(t *T) bool { _, o := sel(t); return zero32(o) },
			fmt.Sprintf(`{"op":%q,%s,"select":"x","occurrence":0}`, op, head)),
		rule("occurrence requires select", "occurrence",
			func(t *T) bool { s, o := sel(t); return o != nil && s == nil },
			fmt.Sprintf(`{"op":%q,%s,"occurrence":1}`, op, head)),
	}
}

var bookmarkAddRules = append([]Rule{
	newNameRule("name", func(b *BookmarkAdd) string { return b.Name },
		`{"op":"bookmark.add","at":"0000000A","name":"Clause 4"}`),
}, selectRules(func(b *BookmarkAdd) (*string, *uint32) { return b.Select, b.Occurrence },
	"bookmark.add", `"at":"0000000A","name":"Mark"`)...)

var bookmarkMoveRules = append([]Rule{
	existingNameRule(func(b *BookmarkMove) BookmarkAddress { return b.At },
		`{"op":"bookmark.move","at":"bookmark:","to":"0000000A"}`),
}, selectRules(func(b *BookmarkMove) (*string, *uint32) { return b.Select, b.Occurrence },
	"bookmark.move", `"at":"bookmark:Mark","to":"0000000A"`)...)

var bookmarkRenameRules = []Rule{
	existingNameRule(func(b *BookmarkRename) BookmarkAddress { return b.At },
		`{"op":"bookmark.rename","at":"bookmark:","name":"Mark"}`),
	newNameRule("name", func(b *BookmarkRename) string { return b.Name },
		`{"op":"bookmark.rename","at":"bookmark:Old","name":"_Hidden"}`),
}

var bookmarkDeleteRules = []Rule{
	existingNameRule(func(b *BookmarkDelete) BookmarkAddress { return b.At },
		`{"op":"bookmark.delete","at":"Mark"}`),
}

const commentTextMsg = "comment text must be non-empty"

var commentAddRules = []Rule{
	rule(commentTextMsg, "text",
		func(c *CommentAdd) bool { return c.Text == "" },
		`{"op":"comment.add","at":"0000000A","text":""}`),
	rule("occurrence must be positive", "occurrence",
		func(c *CommentAdd) bool { return zero32(c.Occurrence) },
		`{"op":"comment.add","at":"0000000A","select":"x","occurrence":0,"text":"t"}`),
	rule("occurrence requires select", "occurrence",
		func(c *CommentAdd) bool { return c.Occurrence != nil && c.Select == nil },
		`{"op":"comment.add","at":"0000000A","occurrence":1,"text":"t"}`),
}

// commented is an op that names an existing comment.
type commented interface {
	CommentReply | CommentSetStatus | CommentDelete
}

// commentAtOf returns comment specs' address, or empty for others.
func commentAtOf(s any) CommentAddress {
	switch x := s.(type) {
	case *CommentReply:
		return x.At
	case *CommentSetStatus:
		return x.At
	case *CommentDelete:
		return x.At
	}
	return ""
}

// commentAtRules returns the rule checking a comment address field.
func commentAtRules[T commented](example string) []Rule {
	return []Rule{rule("at must be a comment address: comment:N, N the comment's decimal id", "at",
		func(t *T) bool { _, ok := commentAtOf(t).ID(); return !ok }, example)}
}

var commentReplyRules = append(
	commentAtRules[CommentReply](`{"op":"comment.reply","at":"comment:c1","text":"t"}`),
	rule(commentTextMsg, "text",
		func(c *CommentReply) bool { return c.Text == "" },
		`{"op":"comment.reply","at":"comment:1","text":""}`),
)

// revisionRules are the checks of revision.accept and revision.reject,
// whose selection is revision.convert_to_formatting's: one revision, all
// of them or the revisions of some paragraphs, the last two narrowed to
// one author's by author.
func revisionRules[T any](get func(*T) (*RevisionScope, bool, *RevisionAuthor), op string) []Rule {
	return []Rule{
		rule("give at (a revision or paragraphs) or all: true; author narrows either", "at",
			func(t *T) bool { at, all, _ := get(t); return at == nil && !all },
			fmt.Sprintf(`{"op":%q,"author":"Ann"}`, op)),
		rule("at and all exclude each other", "all",
			func(t *T) bool { at, all, _ := get(t); return at != nil && all },
			fmt.Sprintf(`{"op":%q,"at":"revision:1","all":true}`, op)),
		rule("at must name at least one paragraph when given", "at",
			func(t *T) bool {
				at, _, _ := get(t)
				return at != nil && at.Paragraphs != nil && len(at.Paragraphs) == 0
			},
			fmt.Sprintf(`{"op":%q,"at":[]}`, op)),
		rule("author narrows all or a list of paragraphs, not one revision", "author",
			func(t *T) bool { at, _, author := get(t); return author != nil && at != nil && at.ID != "" },
			fmt.Sprintf(`{"op":%q,"at":"revision:1","author":"Ann"}`, op)),
	}
}

var convertRules = []Rule{
	rule("give at (a revision or paragraphs) or all: true; author narrows either", "",
		func(c *ConvertRevisionsToFormatting) bool { return c.At == nil && !c.All },
		`{"op":"revision.convert_to_formatting","author":"Ann"}`),
	rule("at and all exclude each other", "all",
		func(c *ConvertRevisionsToFormatting) bool { return c.All && c.At != nil },
		`{"op":"revision.convert_to_formatting","all":true,"at":["0000000A"]}`),
	rule("at must name at least one paragraph when given", "at",
		func(c *ConvertRevisionsToFormatting) bool {
			return c.At != nil && c.At.Paragraphs != nil && len(c.At.Paragraphs) == 0
		},
		`{"op":"revision.convert_to_formatting","at":[]}`),
	rule("author narrows all or a list of paragraphs, not one revision", "author",
		func(c *ConvertRevisionsToFormatting) bool { return c.Author != nil && c.At != nil && c.At.ID != "" },
		`{"op":"revision.convert_to_formatting","at":"revision:1","author":"Ann"}`),
}
