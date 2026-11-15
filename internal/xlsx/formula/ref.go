package formula

import "strings"

// Prefix is the qualifier in front of a Ref, Name or Func token, split
// into its parts.
type Prefix struct {
	// Text is the prefix as written, "!" included; "" when there is none.
	Text string
	// Book is the workbook between brackets ("1" for "[1]"), with any
	// path written before the bracket in a quoted prefix; HasBook says a
	// bracket was present.
	Book    string
	HasBook bool
	// First and Last are the sheet names, unquoted and unescaped. Last is
	// set only for a 3-D span ("Jan:Mar").
	First, Last string
	// Quoted says the sheet part was written in quotes.
	Quoted bool
}

// HasSheet reports whether the prefix names a sheet.
func (p Prefix) HasSheet() bool { return p.First != "" }

// ThreeD reports whether the prefix names a span of sheets.
func (p Prefix) ThreeD() bool { return p.Last != "" }

// SplitQualified splits the text of a Ref, Name or Func token into its
// prefix and the rest (the cell, range, #REF!, name or function). ok is
// false when a quoted prefix is malformed.
func SplitQualified(text string) (p Prefix, rest string, ok bool) {
	end := prefixEnd(text)
	if end == 0 {
		return Prefix{}, text, true
	}
	p, ok = parsePrefix(text[:end])
	return p, text[end:], ok
}

// prefixEnd returns the length of the prefix (through "!") at the start
// of a token's text, or 0.
func prefixEnd(t string) int {
	if t != "" && t[0] == '!' {
		return 1
	}
	return prefixAt(t, 0)
}

// parsePrefix parses a prefix text that prefixEnd found.
func parsePrefix(t string) (Prefix, bool) {
	p := Prefix{Text: t}
	body := t[:len(t)-1] // without "!"
	if body == "" {
		return p, true
	}
	if body[0] == '[' {
		k := strings.IndexByte(body, ']')
		p.HasBook, p.Book = true, body[1:k]
		body = body[k+1:]
		if body == "" {
			return p, true
		}
	}
	e := sheetPart(body, 0)
	first := body[:e]
	var last string
	if e < len(body) {
		last = body[e+1:] // after ":"
		if last != "" && last[0] == '\'' {
			p.Quoted = true
			last = unquote(last)
		}
	}
	if first[0] == '\'' {
		p.Quoted = true
		first = unquote(first)
		if last == "" && !p.HasBook {
			// '[1]Sheet'!, 'C:\dir\[Book.xlsx]Sheet'! and 'Jan:Mar'!
			if open := strings.IndexByte(first, '['); open >= 0 {
				cl := strings.IndexByte(first[open:], ']')
				if cl < 0 {
					return p, false
				}
				p.HasBook = true
				p.Book = first[:open] + first[open+1:open+cl]
				first = first[open+cl+1:]
			}
			first, last, _ = strings.Cut(first, ":")
		}
	}
	if first == "" || e < len(body) && last == "" {
		return p, false
	}
	p.First, p.Last = first, last
	return p, true
}

// unquote strips the quotes of a quoted sheet name and undoes "”".
func unquote(q string) string {
	return strings.ReplaceAll(q[1:len(q)-1], "''", "'")
}

// NeedsQuote reports whether a sheet name must be quoted in a reference:
// it is empty, does not start with a letter, "_" or "\", holds a
// character a name may not, or reads as a cell reference in A1 or R1C1
// form (LooksLikeCellRef). It errs towards quoting, which is always
// valid.
func NeedsQuote(sheet string) bool {
	if sheet == "" || LooksLikeCellRef(sheet) {
		return true
	}
	for i, r := range sheet {
		if i == 0 && !isNameStart(r) || !isNameChar(r) {
			return true
		}
	}
	return false
}

// SheetPrefix writes the prefix for a sheet or a 3-D span (last may be
// ""), "!" included, quoting when either name needs it.
func SheetPrefix(first, last string) string {
	var b strings.Builder
	writeSheetPrefix(&b, "", false, first, last)
	return b.String()
}

// writeSheetPrefix writes the sheet or 3-D span prefix, with its workbook, to b.
func writeSheetPrefix(b *strings.Builder, book string, hasBook bool, first, last string) {
	if last != "" && strings.ContainsRune(first+last, ':') {
		// A span whose name holds ":" (no Excel sheet name does) cannot
		// be quoted as one 'First:Last' (it would split elsewhere when
		// read back): each end is quoted on its own, as parsePrefix reads.
		var one strings.Builder
		writeSheetPrefix(&one, book, hasBook, first, "")
		b.WriteString(strings.TrimSuffix(one.String(), "!"))
		b.WriteByte(':')
		b.WriteByte('\'')
		b.WriteString(strings.ReplaceAll(last, "'", "''"))
		b.WriteString("'!")
		return
	}
	quote := NeedsQuote(first) || last != "" && NeedsQuote(last)
	if quote {
		b.WriteByte('\'')
	}
	if hasBook {
		b.WriteByte('[')
		b.WriteString(book)
		b.WriteByte(']')
	}
	if quote {
		b.WriteString(strings.ReplaceAll(first, "'", "''"))
	} else {
		b.WriteString(first)
	}
	if last != "" {
		b.WriteByte(':')
		if quote {
			b.WriteString(strings.ReplaceAll(last, "'", "''"))
		} else {
			b.WriteString(last)
		}
	}
	if quote {
		b.WriteByte('\'')
	}
	b.WriteByte('!')
}

// ParseRefBody parses what follows a Ref token's prefix: a range, or
// #REF! (isErr).
func ParseRefBody(body string) (r Range, isErr, ok bool) {
	if strings.EqualFold(body, "#REF!") {
		return Range{}, true, true
	}
	r, ok = ParseRange(body)
	return r, false, ok
}
