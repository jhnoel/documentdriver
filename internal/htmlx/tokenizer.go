package htmlx

import (
	"html"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// The tokenizer is a port of xmloxide 0.4.3 src/html5/tokenizer.rs, the
// WHATWG tokenizer docxdriver 0.5.2 parses HTML input with. It keeps that
// implementation's error codes and byte offsets, because docxdriver fails on
// the first one and reports it. The script data states are not ported (the
// tree builder rejects <script> first), nor CDATA sections, which HTML
// allows only inside SVG and MathML, both rejected here.

type tokenKind uint8

const (
	tokDoctype tokenKind = iota
	tokStartTag
	tokEndTag
	tokChar
	tokComment
	tokEOF
)

type token struct {
	kind        tokenKind
	name        string // tag name or doctype name
	attrs       []Attr
	selfClosing bool
	r           rune   // tokChar
	data        string // tokComment

	hasName, hasPublic, hasSystem bool
	publicID, systemID            string
	forceQuirks                   bool
}

type state uint8

const (
	sData state = iota
	sRCData
	sRawText
	sPlaintext
	sTagOpen
	sEndTagOpen
	sTagName
	sRCDataLessThan
	sRCDataEndTagOpen
	sRCDataEndTagName
	sRawTextLessThan
	sRawTextEndTagOpen
	sRawTextEndTagName
	sBeforeAttrName
	sAttrName
	sAfterAttrName
	sBeforeAttrValue
	sAttrValueDouble
	sAttrValueSingle
	sAttrValueUnquoted
	sAfterAttrValueQuoted
	sSelfClosingStartTag
	sBogusComment
	sMarkupDeclarationOpen
	sCommentStart
	sCommentStartDash
	sComment
	sCommentLessThan
	sCommentLessThanBang
	sCommentLessThanBangDash
	sCommentLessThanBangDashDash
	sCommentEndDash
	sCommentEnd
	sCommentEndBang
	sDoctype
	sBeforeDoctypeName
	sDoctypeName
	sAfterDoctypeName
	sAfterDoctypePublicKeyword
	sBeforeDoctypePublicID
	sDoctypePublicIDDouble
	sDoctypePublicIDSingle
	sAfterDoctypePublicID
	sBetweenDoctypePublicAndSystem
	sAfterDoctypeSystemKeyword
	sBeforeDoctypeSystemID
	sDoctypeSystemIDDouble
	sDoctypeSystemIDSingle
	sAfterDoctypeSystemID
	sBogusDoctype
	sCharRef
	sNamedCharRef
	sAmbiguousAmpersand
	sNumericCharRef
	sHexCharRefStart
	sDecimalCharRefStart
	sHexCharRef
	sDecimalCharRef
	sNumericCharRefEnd
)

// tokError is the first tokenizer parse error: its WHATWG code and the byte
// offset (into the newline-normalised input) where it was raised.
type tokError struct {
	code string
	span int
}

type tokenizer struct {
	input       string
	pos         int
	state       state
	returnState state

	tagName        strings.Builder
	tagAttrs       []Attr
	tagSelfClosing bool
	tagIsEnd       bool
	attrName       strings.Builder
	attrValue      strings.Builder

	comment strings.Builder

	doctypeName        strings.Builder
	hasDoctypeName     bool
	doctypePublic      strings.Builder
	hasDoctypePublic   bool
	doctypeSystem      strings.Builder
	hasDoctypeSystem   bool
	doctypeForceQuirks bool

	tempBuffer      strings.Builder
	pending         []token
	head            int
	err             *tokError
	lastStartTag    string
	hasLastStartTag bool
	charRefCode     uint32
	duplicate       string          // first attribute name repeated on a start tag
	attrSeen        map[string]bool // attribute names of the current tag
}

// newTokenizer returns a tokenizer over input with CR and CRLF normalised to LF.
func newTokenizer(input string) *tokenizer {
	if strings.IndexByte(input, '\r') >= 0 {
		input = normalizeNewlines(input)
	}
	return &tokenizer{input: input}
}

// normalizeNewlines returns s with CR and CRLF replaced by LF.
func normalizeNewlines(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' {
			b.WriteByte('\n')
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// setState sets the tokenizer state to s.
func (t *tokenizer) setState(s state) { t.state = s }

// setLastStartTag records name as the last start tag seen.
func (t *tokenizer) setLastStartTag(name string) {
	t.lastStartTag = name
	t.hasLastStartTag = true
}

// next returns the next token. After the first parse error it returns EOF
// forever: the caller fails on that error, so nothing after it matters.
func (t *tokenizer) next() token {
	for {
		if t.err != nil {
			return token{kind: tokEOF}
		}
		if t.head < len(t.pending) {
			tok := t.pending[t.head]
			t.pending[t.head] = token{}
			t.head++
			if t.head == len(t.pending) {
				t.pending = t.pending[:0]
				t.head = 0
			}
			return tok
		}
		t.step()
	}
}

// step advances the tokenizer by one state, dispatching to that state's handler.
func (t *tokenizer) step() {
	switch t.state {
	case sData:
		t.stateData()
	case sRCData:
		t.stateRCData()
	case sRawText:
		t.stateRawText()
	case sPlaintext:
		t.statePlaintext()
	case sTagOpen:
		t.stateTagOpen()
	case sEndTagOpen:
		t.stateEndTagOpen()
	case sTagName:
		t.stateTagName()
	case sRCDataLessThan:
		t.stateTextLessThan(sRCDataEndTagOpen, sRCData)
	case sRCDataEndTagOpen:
		t.stateTextEndTagOpen(sRCDataEndTagName, sRCData)
	case sRCDataEndTagName:
		t.stateTextEndTagName(sRCData)
	case sRawTextLessThan:
		t.stateTextLessThan(sRawTextEndTagOpen, sRawText)
	case sRawTextEndTagOpen:
		t.stateTextEndTagOpen(sRawTextEndTagName, sRawText)
	case sRawTextEndTagName:
		t.stateTextEndTagName(sRawText)
	case sBeforeAttrName:
		t.stateBeforeAttrName()
	case sAttrName:
		t.stateAttrName()
	case sAfterAttrName:
		t.stateAfterAttrName()
	case sBeforeAttrValue:
		t.stateBeforeAttrValue()
	case sAttrValueDouble:
		t.stateAttrValueQuoted('"', sAttrValueDouble)
	case sAttrValueSingle:
		t.stateAttrValueQuoted('\'', sAttrValueSingle)
	case sAttrValueUnquoted:
		t.stateAttrValueUnquoted()
	case sAfterAttrValueQuoted:
		t.stateAfterAttrValueQuoted()
	case sSelfClosingStartTag:
		t.stateSelfClosingStartTag()
	case sBogusComment:
		t.stateBogusComment()
	case sMarkupDeclarationOpen:
		t.stateMarkupDeclarationOpen()
	case sCommentStart:
		t.stateCommentStart()
	case sCommentStartDash:
		t.stateCommentStartDash()
	case sComment:
		t.stateComment()
	case sCommentLessThan:
		t.stateCommentLessThan()
	case sCommentLessThanBang:
		t.stateCommentLessThanBang()
	case sCommentLessThanBangDash:
		t.stateCommentLessThanBangDash()
	case sCommentLessThanBangDashDash:
		t.stateCommentLessThanBangDashDash()
	case sCommentEndDash:
		t.stateCommentEndDash()
	case sCommentEnd:
		t.stateCommentEnd()
	case sCommentEndBang:
		t.stateCommentEndBang()
	default:
		t.stepDoctypeOrRef()
	}
}

// stepDoctypeOrRef dispatches doctype and character-reference states to their handlers.
func (t *tokenizer) stepDoctypeOrRef() {
	switch t.state {
	case sDoctype:
		t.stateDoctype()
	case sBeforeDoctypeName:
		t.stateBeforeDoctypeName()
	case sDoctypeName:
		t.stateDoctypeName()
	case sAfterDoctypeName:
		t.stateAfterDoctypeName()
	case sAfterDoctypePublicKeyword:
		t.stateAfterDoctypeKeyword(true)
	case sBeforeDoctypePublicID:
		t.stateBeforeDoctypeID(true)
	case sDoctypePublicIDDouble:
		t.stateDoctypeID(true, '"')
	case sDoctypePublicIDSingle:
		t.stateDoctypeID(true, '\'')
	case sAfterDoctypePublicID:
		t.stateAfterDoctypePublicID()
	case sBetweenDoctypePublicAndSystem:
		t.stateBetweenDoctypePublicAndSystem()
	case sAfterDoctypeSystemKeyword:
		t.stateAfterDoctypeKeyword(false)
	case sBeforeDoctypeSystemID:
		t.stateBeforeDoctypeID(false)
	case sDoctypeSystemIDDouble:
		t.stateDoctypeID(false, '"')
	case sDoctypeSystemIDSingle:
		t.stateDoctypeID(false, '\'')
	case sAfterDoctypeSystemID:
		t.stateAfterDoctypeSystemID()
	case sBogusDoctype:
		t.stateBogusDoctype()
	case sCharRef:
		t.stateCharRef()
	case sNamedCharRef:
		t.stateNamedCharRef()
	case sAmbiguousAmpersand:
		t.stateAmbiguousAmpersand()
	case sNumericCharRef:
		t.stateNumericCharRef()
	case sHexCharRefStart:
		t.stateNumericCharRefStart(ascii.IsHex)
	case sDecimalCharRefStart:
		t.stateNumericCharRefStart(ascii.IsDigit)
	case sHexCharRef:
		t.stateDigitsCharRef(16)
	case sDecimalCharRef:
		t.stateDigitsCharRef(10)
	case sNumericCharRefEnd:
		t.stateNumericCharRefEnd()
	}
}

// eof is the sentinel consume and peek return at the end of input.
const eof rune = -1

// peek returns the next rune without consuming it, or eof at end of input.
func (t *tokenizer) peek() rune {
	if t.pos >= len(t.input) {
		return eof
	}
	r, _ := utf8.DecodeRuneInString(t.input[t.pos:])
	return r
}

// consume returns and advances past the next rune, or eof at end of input.
func (t *tokenizer) consume() rune {
	if t.pos >= len(t.input) {
		return eof
	}
	r, n := utf8.DecodeRuneInString(t.input[t.pos:])
	t.pos += n
	return r
}

// reconsume steps the position back over rune r.
func (t *tokenizer) reconsume(r rune) { t.pos -= utf8.RuneLen(r) }

// nextCharsASCIIFold reports whether the next bytes equal needle case-insensitively.
func (t *tokenizer) nextCharsASCIIFold(needle string) bool {
	if t.pos+len(needle) > len(t.input) {
		return false
	}
	return ascii.EqualFold(t.input[t.pos:t.pos+len(needle)], needle)
}

// error records the first tokenizer error code at the current position, if none exists.
func (t *tokenizer) error(code string) {
	if t.err == nil {
		t.err = &tokError{code: code, span: t.pos}
	}
}

// emit appends tok to the pending token queue.
func (t *tokenizer) emit(tok token) { t.pending = append(t.pending, tok) }

// emitChar queues a character token for rune r.
func (t *tokenizer) emitChar(r rune) { t.emit(token{kind: tokChar, r: r}) }

// emitEOF queues an EOF token.
func (t *tokenizer) emitEOF() { t.emit(token{kind: tokEOF}) }

// emitComment queues the buffered comment token and clears the buffer.
func (t *tokenizer) emitComment() {
	t.emit(token{kind: tokComment, data: t.comment.String()})
	t.comment.Reset()
}

// emitCurrentTag finishes the pending attribute and queues the start or end tag token.
func (t *tokenizer) emitCurrentTag() {
	t.finishAttr()
	name := t.tagName.String()
	t.tagName.Reset()
	if t.tagIsEnd {
		t.emit(token{kind: tokEndTag, name: name})
	} else {
		t.setLastStartTag(name)
		t.emit(token{kind: tokStartTag, name: name, attrs: t.tagAttrs, selfClosing: t.tagSelfClosing})
		t.tagAttrs = nil
	}
	t.tagSelfClosing = false
}

// emitDoctype queues the buffered doctype token and clears doctype buffers.
func (t *tokenizer) emitDoctype() {
	t.emit(token{
		kind:        tokDoctype,
		name:        t.doctypeName.String(),
		hasName:     t.hasDoctypeName,
		publicID:    t.doctypePublic.String(),
		hasPublic:   t.hasDoctypePublic,
		systemID:    t.doctypeSystem.String(),
		hasSystem:   t.hasDoctypeSystem,
		forceQuirks: t.doctypeForceQuirks,
	})
	t.createDoctype()
}

// createTag resets tag buffers and starts a new start or end tag.
func (t *tokenizer) createTag(end bool) {
	clear(t.attrSeen)
	t.tagName.Reset()
	t.tagAttrs = nil
	t.tagSelfClosing = false
	t.tagIsEnd = end
	t.attrName.Reset()
	t.attrValue.Reset()
}

// startNewAttr finishes the current attribute and starts a blank one.
func (t *tokenizer) startNewAttr() {
	t.finishAttr()
	t.attrName.Reset()
	t.attrValue.Reset()
}

// finishAttr appends the current attribute unless its name is empty or
// already present. xmloxide keeps the first of duplicate attributes
// silently; the duplicate is recorded so the builder can reject it.
func (t *tokenizer) finishAttr() {
	if t.attrName.Len() == 0 {
		return
	}
	name := t.attrName.String()
	value := t.attrValue.String()
	t.attrName.Reset()
	t.attrValue.Reset()
	if t.attrSeen == nil {
		t.attrSeen = map[string]bool{}
	}
	if t.attrSeen[name] {
		if !t.tagIsEnd && t.duplicate == "" {
			t.duplicate = name
		}
		return
	}
	t.attrSeen[name] = true
	t.tagAttrs = append(t.tagAttrs, Attr{Name: name, Value: value})
}

// createDoctype clears the doctype name, identifiers, and quirks flag.
func (t *tokenizer) createDoctype() {
	t.doctypeName.Reset()
	t.hasDoctypeName = false
	t.doctypePublic.Reset()
	t.hasDoctypePublic = false
	t.doctypeSystem.Reset()
	t.hasDoctypeSystem = false
	t.doctypeForceQuirks = false
}

// isAppropriateEndTag reports whether the tag name matches the last start tag.
func (t *tokenizer) isAppropriateEndTag() bool {
	return t.hasLastStartTag && t.lastStartTag == t.tagName.String()
}

// isAttrValueState reports whether s is a quoted or unquoted attribute-value state.
func isAttrValueState(s state) bool {
	return s == sAttrValueDouble || s == sAttrValueSingle || s == sAttrValueUnquoted
}

// flushCharRef moves the buffered reference to the attribute value or emits its characters.
func (t *tokenizer) flushCharRef() {
	buf := t.tempBuffer.String()
	t.tempBuffer.Reset()
	if isAttrValueState(t.returnState) {
		t.attrValue.WriteString(buf)
		return
	}
	for _, r := range buf {
		t.emitChar(r)
	}
}

// emitTempBufferChars emits any characters buffered in tempBuffer and clears it.
func (t *tokenizer) emitTempBufferChars() {
	buf := t.tempBuffer.String()
	t.tempBuffer.Reset()
	for _, r := range buf {
		t.emitChar(r)
	}
}

// stateData emits data characters and dispatches on &, <, null, and EOF.
func (t *tokenizer) stateData() {
	start := t.pos
	i := start
	for i < len(t.input) {
		b := t.input[i]
		if b == '<' || b == '&' || b == 0 {
			break
		}
		i++
	}
	if i > start {
		for _, r := range t.input[start:i] {
			t.emitChar(r)
		}
		t.pos = i
		return
	}
	switch c := t.consume(); c {
	case '&':
		t.returnState = sData
		t.state = sCharRef
	case '<':
		t.state = sTagOpen
	case 0:
		t.error("unexpected-null-character")
		t.emitChar(0)
	case eof:
		t.emitEOF()
	default:
		t.emitChar(c)
	}
}

// stateRCData emits RCDATA characters and dispatches on &, <, null, and EOF.
func (t *tokenizer) stateRCData() {
	switch c := t.consume(); c {
	case '&':
		t.returnState = sRCData
		t.state = sCharRef
	case '<':
		t.state = sRCDataLessThan
	case 0:
		t.error("unexpected-null-character")
		t.emitChar('�')
	case eof:
		t.emitEOF()
	default:
		t.emitChar(c)
	}
}

// stateRawText emits raw-text characters and dispatches on <, null, and EOF.
func (t *tokenizer) stateRawText() {
	switch c := t.consume(); c {
	case '<':
		t.state = sRawTextLessThan
	case 0:
		t.error("unexpected-null-character")
		t.emitChar('�')
	case eof:
		t.emitEOF()
	default:
		t.emitChar(c)
	}
}

// statePlaintext emits plaintext characters, replacing null with U+FFFD.
func (t *tokenizer) statePlaintext() {
	switch c := t.consume(); c {
	case 0:
		t.error("unexpected-null-character")
		t.emitChar('�')
	case eof:
		t.emitEOF()
	default:
		t.emitChar(c)
	}
}

// stateTagOpen dispatches the character after < to tag, markup, or bogus-comment handling.
func (t *tokenizer) stateTagOpen() {
	c := t.consume()
	switch {
	case c == '!':
		t.state = sMarkupDeclarationOpen
	case c == '/':
		t.state = sEndTagOpen
	case ascii.IsAlpha(c):
		t.createTag(false)
		t.reconsume(c)
		t.state = sTagName
	case c == '?':
		t.error("unexpected-question-mark-instead-of-tag-name")
		t.comment.Reset()
		t.reconsume('?')
		t.state = sBogusComment
	case c == eof:
		t.error("eof-before-tag-name")
		t.emitChar('<')
		t.emitEOF()
	default:
		t.error("invalid-first-character-of-tag-name")
		t.reconsume(c)
		t.state = sData
		t.emitChar('<')
	}
}

// stateEndTagOpen dispatches the character after </ to an end tag or bogus comment.
func (t *tokenizer) stateEndTagOpen() {
	c := t.consume()
	switch {
	case ascii.IsAlpha(c):
		t.createTag(true)
		t.reconsume(c)
		t.state = sTagName
	case c == '>':
		t.error("missing-end-tag-name")
		t.state = sData
	case c == eof:
		t.error("eof-before-tag-name")
		t.emitChar('<')
		t.emitChar('/')
		t.emitEOF()
	default:
		t.error("invalid-first-character-of-tag-name")
		t.comment.Reset()
		t.reconsume(c)
		t.state = sBogusComment
	}
}

// stateTagName accumulates the lowercased tag name until whitespace, /, >, null, or EOF.
func (t *tokenizer) stateTagName() {
	i := t.pos
	for i < len(t.input) {
		b := t.input[i]
		if b == '\t' || b == '\n' || b == '\f' || b == ' ' || b == '/' || b == '>' || b == 0 || b >= 0x80 {
			break
		}
		t.tagName.WriteByte(ascii.LowerByte(b))
		i++
	}
	t.pos = i
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
		t.state = sBeforeAttrName
	case '/':
		t.state = sSelfClosingStartTag
	case '>':
		t.state = sData
		t.emitCurrentTag()
	case 0:
		t.error("unexpected-null-character")
		t.tagName.WriteRune('�')
	case eof:
		t.error("eof-in-tag")
		t.emitEOF()
	default:
		t.tagName.WriteRune(ascii.LowerRune(c))
	}
}

// stateTextLessThan checks for </ in RCDATA or raw text, otherwise emitting <.
func (t *tokenizer) stateTextLessThan(endTagOpen, text state) {
	if t.peek() == '/' {
		t.consume()
		t.tempBuffer.Reset()
		t.state = endTagOpen
		return
	}
	t.state = text
	t.emitChar('<')
}

// stateTextEndTagOpen checks for an end tag in RCDATA or raw text, otherwise emitting </.
func (t *tokenizer) stateTextEndTagOpen(endTagName, text state) {
	if ascii.IsAlpha(t.peek()) {
		t.createTag(true)
		t.state = endTagName
		return
	}
	t.state = text
	t.emitChar('<')
	t.emitChar('/')
}

// stateTextEndTagName matches an end tag against the last start tag in RCDATA or raw text.
func (t *tokenizer) stateTextEndTagName(text state) {
	notEnd := func(c rune) {
		t.emitChar('<')
		t.emitChar('/')
		t.emitTempBufferChars()
		if c != eof {
			t.reconsume(c)
		}
		t.state = text
	}
	c := t.consume()
	switch {
	case c == '\t' || c == '\n' || c == '\f' || c == ' ':
		if t.isAppropriateEndTag() {
			t.state = sBeforeAttrName
		} else {
			notEnd(c)
		}
	case c == '/':
		if t.isAppropriateEndTag() {
			t.state = sSelfClosingStartTag
		} else {
			notEnd(c)
		}
	case c == '>':
		if t.isAppropriateEndTag() {
			t.state = sData
			t.emitCurrentTag()
		} else {
			notEnd(c)
		}
	case ascii.IsAlpha(c):
		t.tagName.WriteRune(ascii.LowerRune(c))
		t.tempBuffer.WriteRune(c)
	default:
		notEnd(c)
	}
}

// stateBeforeAttrName skips whitespace before an attribute name and starts the attribute.
func (t *tokenizer) stateBeforeAttrName() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
	case '/', '>':
		t.reconsume(c)
		t.state = sAfterAttrName
	case eof:
		t.state = sAfterAttrName
	case '=':
		t.error("unexpected-equals-sign-before-attribute-name")
		t.startNewAttr()
		t.attrName.WriteByte('=')
		t.state = sAttrName
	default:
		t.startNewAttr()
		t.reconsume(c)
		t.state = sAttrName
	}
}

// stateAttrName accumulates the lowercased attribute name until a delimiter or EOF.
func (t *tokenizer) stateAttrName() {
	i := t.pos
	for i < len(t.input) {
		b := t.input[i]
		if b == '\t' || b == '\n' || b == '\f' || b == ' ' || b == '/' || b == '>' || b == '=' ||
			b == 0 || b == '"' || b == '\'' || b == '<' || b >= 0x80 {
			break
		}
		t.attrName.WriteByte(ascii.LowerByte(b))
		i++
	}
	t.pos = i
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ', '/', '>':
		t.reconsume(c)
		t.state = sAfterAttrName
	case '=':
		t.state = sBeforeAttrValue
	case 0:
		t.error("unexpected-null-character")
		t.attrName.WriteRune('�')
	case '"', '\'', '<':
		t.error("unexpected-character-in-attribute-name")
		t.attrName.WriteRune(c)
	case eof:
		t.state = sAfterAttrName
	default:
		t.attrName.WriteRune(ascii.LowerRune(c))
	}
}

// stateAfterAttrName handles whitespace and =, /, >, or the next attribute after an attribute name.
func (t *tokenizer) stateAfterAttrName() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
	case '/':
		t.state = sSelfClosingStartTag
	case '=':
		t.state = sBeforeAttrValue
	case '>':
		t.state = sData
		t.emitCurrentTag()
	case eof:
		t.error("eof-in-tag")
		t.emitEOF()
	default:
		t.startNewAttr()
		t.reconsume(c)
		t.state = sAttrName
	}
}

// stateBeforeAttrValue skips whitespace and selects quoted or unquoted attribute-value parsing.
func (t *tokenizer) stateBeforeAttrValue() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
	case '"':
		t.state = sAttrValueDouble
	case '\'':
		t.state = sAttrValueSingle
	case '>':
		t.error("missing-attribute-value")
		t.state = sData
		t.emitCurrentTag()
	case eof:
		t.state = sAttrValueUnquoted
	default:
		t.reconsume(c)
		t.state = sAttrValueUnquoted
	}
}

// stateAttrValueQuoted accumulates a quoted attribute value until its quote, &, null, or EOF.
func (t *tokenizer) stateAttrValueQuoted(quote byte, self state) {
	start := t.pos
	i := start
	for i < len(t.input) {
		b := t.input[i]
		if b == quote || b == '&' || b == 0 {
			break
		}
		i++
	}
	if i > start {
		t.attrValue.WriteString(t.input[start:i])
		t.pos = i
	}
	switch c := t.consume(); c {
	case rune(quote):
		t.state = sAfterAttrValueQuoted
	case '&':
		t.returnState = self
		t.state = sCharRef
	case 0:
		t.error("unexpected-null-character")
		t.attrValue.WriteRune('�')
	case eof:
		t.error("eof-in-tag")
		t.emitEOF()
	default:
		t.attrValue.WriteRune(c)
	}
}

// stateAttrValueUnquoted accumulates an unquoted attribute value until whitespace, >, &, or EOF.
func (t *tokenizer) stateAttrValueUnquoted() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
		t.state = sBeforeAttrName
	case '&':
		t.returnState = sAttrValueUnquoted
		t.state = sCharRef
	case '>':
		t.state = sData
		t.emitCurrentTag()
	case 0:
		t.error("unexpected-null-character")
		t.attrValue.WriteRune('�')
	case '"', '\'', '<', '=', '`':
		t.error("unexpected-character-in-unquoted-attribute-value")
		t.attrValue.WriteRune(c)
	case eof:
		t.error("eof-in-tag")
		t.emitEOF()
	default:
		t.attrValue.WriteRune(c)
	}
}

// stateAfterAttrValueQuoted expects whitespace, /, >, or a new attribute after a quoted value.
func (t *tokenizer) stateAfterAttrValueQuoted() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
		t.state = sBeforeAttrName
	case '/':
		t.state = sSelfClosingStartTag
	case '>':
		t.state = sData
		t.emitCurrentTag()
	case eof:
		t.error("eof-in-tag")
		t.emitEOF()
	default:
		t.error("missing-whitespace-between-attributes")
		t.reconsume(c)
		t.state = sBeforeAttrName
	}
}

// stateSelfClosingStartTag handles > after / as self-closing, otherwise returning to attribute parsing.
func (t *tokenizer) stateSelfClosingStartTag() {
	switch c := t.consume(); c {
	case '>':
		t.tagSelfClosing = true
		t.state = sData
		t.emitCurrentTag()
	case eof:
		t.error("eof-in-tag")
		t.emitEOF()
	default:
		t.error("unexpected-solidus-in-tag")
		t.reconsume(c)
		t.state = sBeforeAttrName
	}
}

// stateBogusComment accumulates a bogus comment until > or EOF and emits it.
func (t *tokenizer) stateBogusComment() {
	switch c := t.consume(); c {
	case '>':
		t.state = sData
		t.emitComment()
	case eof:
		t.emitComment()
		t.emitEOF()
	case 0:
		t.error("unexpected-null-character")
		t.comment.WriteRune('�')
	default:
		t.comment.WriteRune(c)
	}
}

// stateMarkupDeclarationOpen dispatches <! input to comment, doctype, or bogus-comment handling.
func (t *tokenizer) stateMarkupDeclarationOpen() {
	switch {
	case t.nextCharsASCIIFold("--"):
		t.pos += 2
		t.comment.Reset()
		t.state = sCommentStart
	case t.nextCharsASCIIFold("DOCTYPE"):
		t.pos += 7
		t.state = sDoctype
	case t.nextCharsASCIIFold("[CDATA["):
		t.pos += 7
		t.error("cdata-in-html-content")
		t.comment.Reset()
		t.comment.WriteString("[CDATA[")
		t.state = sBogusComment
	default:
		t.error("incorrectly-opened-comment")
		t.comment.Reset()
		t.state = sBogusComment
	}
}

// stateCommentStart handles the first character after <!--, covering empty and EOF comments.
func (t *tokenizer) stateCommentStart() {
	switch c := t.consume(); c {
	case '-':
		t.state = sCommentStartDash
	case '>':
		t.error("abrupt-closing-of-empty-comment")
		t.state = sData
		t.emitComment()
	case eof:
		t.state = sComment
	default:
		t.reconsume(c)
		t.state = sComment
	}
}

// stateCommentStartDash handles the character after <!---, covering abrupt comment ends.
func (t *tokenizer) stateCommentStartDash() {
	switch c := t.consume(); c {
	case '-':
		t.state = sCommentEnd
	case '>':
		t.error("abrupt-closing-of-empty-comment")
		t.state = sData
		t.emitComment()
	case eof:
		t.error("eof-in-comment")
		t.emitComment()
		t.emitEOF()
	default:
		t.comment.WriteByte('-')
		t.reconsume(c)
		t.state = sComment
	}
}

// stateComment accumulates comment characters until <, -, null, or EOF.
func (t *tokenizer) stateComment() {
	switch c := t.consume(); c {
	case '<':
		t.comment.WriteByte('<')
		t.state = sCommentLessThan
	case '-':
		t.state = sCommentEndDash
	case 0:
		t.error("unexpected-null-character")
		t.comment.WriteRune('�')
	case eof:
		t.error("eof-in-comment")
		t.emitComment()
		t.emitEOF()
	default:
		t.comment.WriteRune(c)
	}
}

// stateCommentLessThan tracks < inside a comment toward a nested-comment sequence.
func (t *tokenizer) stateCommentLessThan() {
	switch c := t.consume(); c {
	case '!':
		t.comment.WriteByte('!')
		t.state = sCommentLessThanBang
	case '<':
		t.comment.WriteByte('<')
	case eof:
		t.state = sComment
	default:
		t.reconsume(c)
		t.state = sComment
	}
}

// stateCommentLessThanBang checks for - after <! inside a comment.
func (t *tokenizer) stateCommentLessThanBang() {
	if t.peek() == '-' {
		t.consume()
		t.state = sCommentLessThanBangDash
		return
	}
	t.state = sComment
}

// stateCommentLessThanBangDash checks for a second - after <!-- inside a comment.
func (t *tokenizer) stateCommentLessThanBangDash() {
	if t.peek() == '-' {
		t.consume()
		t.state = sCommentLessThanBangDashDash
		return
	}
	t.state = sCommentEndDash
}

// stateCommentLessThanBangDashDash records a nested comment unless > or EOF follows, then continues to comment end.
func (t *tokenizer) stateCommentLessThanBangDashDash() {
	if c := t.peek(); c != '>' && c != eof {
		t.error("nested-comment")
	}
	t.state = sCommentEnd
}

// stateCommentEndDash handles - inside a comment, entering comment end on another -.
func (t *tokenizer) stateCommentEndDash() {
	switch c := t.consume(); c {
	case '-':
		t.state = sCommentEnd
	case eof:
		t.error("eof-in-comment")
		t.emitComment()
		t.emitEOF()
	default:
		t.comment.WriteByte('-')
		t.reconsume(c)
		t.state = sComment
	}
}

// stateCommentEnd handles > , !, -, or other input after -- inside a comment.
func (t *tokenizer) stateCommentEnd() {
	switch c := t.consume(); c {
	case '>':
		t.state = sData
		t.emitComment()
	case '!':
		t.state = sCommentEndBang
	case '-':
		t.comment.WriteByte('-')
	case eof:
		t.error("eof-in-comment")
		t.emitComment()
		t.emitEOF()
	default:
		t.comment.WriteString("--")
		t.reconsume(c)
		t.state = sComment
	}
}

// stateCommentEndBang handles input after --! inside a comment, emitting on > or EOF.
func (t *tokenizer) stateCommentEndBang() {
	switch c := t.consume(); c {
	case '-':
		t.comment.WriteString("--!")
		t.state = sCommentEndDash
	case '>':
		t.error("incorrectly-closed-comment")
		t.state = sData
		t.emitComment()
	case eof:
		t.error("eof-in-comment")
		t.emitComment()
		t.emitEOF()
	default:
		t.comment.WriteString("--!")
		t.reconsume(c)
		t.state = sComment
	}
}

// stateCharRef starts a character reference, dispatching to named or numeric handling.
func (t *tokenizer) stateCharRef() {
	t.tempBuffer.Reset()
	t.tempBuffer.WriteByte('&')
	switch c := t.peek(); {
	case ascii.IsAlnum(c):
		t.state = sNamedCharRef
	case c == '#':
		t.consume()
		t.tempBuffer.WriteByte('#')
		t.state = sNumericCharRef
	default:
		t.flushCharRef()
		t.state = t.returnState
	}
}

// stateNamedCharRef keeps xmloxide's matching rule: consume alphanumerics
// greedily; a name in the table immediately followed by ';' wins at once,
// otherwise the longest legacy (semicolon-optional) name seen is used.
func (t *tokenizer) stateNamedCharRef() {
	start := t.pos
	var semi, legacy string
	semiEnd, legacyEnd := -1, -1
	nameStart := t.pos
	for ascii.IsAlnum(t.peek()) {
		t.consume()
		name := t.input[nameStart:t.pos]
		if repl, ok := lookupEntity(name); ok {
			if t.peek() == ';' {
				t.consume()
				semi, semiEnd = repl, t.pos
				break
			}
			if isLegacyEntity(name) {
				legacy, legacyEnd = repl, t.pos
			}
		}
	}
	repl, end, hadSemi := semi, semiEnd, true
	if semiEnd < 0 {
		repl, end, hadSemi = legacy, legacyEnd, false
	}
	if end < 0 {
		t.pos = start
		t.flushCharRef()
		t.state = sAmbiguousAmpersand
		return
	}
	t.pos = end
	if !hadSemi {
		if isAttrValueState(t.returnState) {
			if next := t.peek(); next != eof && (next == '=' || ascii.IsAlnum(next)) {
				t.pos = start
				t.flushCharRef()
				t.state = t.returnState
				return
			}
		}
		t.error("missing-semicolon-after-character-reference")
	}
	t.tempBuffer.Reset()
	t.tempBuffer.WriteString(repl)
	t.flushCharRef()
	t.state = t.returnState
}

// stateAmbiguousAmpersand emits text after an unmatched & and reports an unknown reference before ;.
func (t *tokenizer) stateAmbiguousAmpersand() {
	c := t.consume()
	switch {
	case ascii.IsAlnum(c):
		if isAttrValueState(t.returnState) {
			t.attrValue.WriteRune(c)
		} else {
			t.emitChar(c)
		}
	case c == ';':
		t.error("unknown-named-character-reference")
		t.reconsume(';')
		t.state = t.returnState
	case c == eof:
		t.state = t.returnState
	default:
		t.reconsume(c)
		t.state = t.returnState
	}
}

// stateNumericCharRef starts a numeric reference, selecting hex or decimal digit parsing.
func (t *tokenizer) stateNumericCharRef() {
	t.charRefCode = 0
	if c := t.peek(); c == 'x' || c == 'X' {
		t.consume()
		t.tempBuffer.WriteRune(c)
		t.state = sHexCharRefStart
		return
	}
	t.state = sDecimalCharRefStart
}

// stateNumericCharRefStart selects hex or decimal digit parsing when a digit follows, otherwise flushing literally.
func (t *tokenizer) stateNumericCharRefStart(digit func(rune) bool) {
	if digit(t.peek()) {
		if t.state == sHexCharRefStart {
			t.state = sHexCharRef
		} else {
			t.state = sDecimalCharRef
		}
		return
	}
	t.error("absence-of-digits-in-numeric-character-reference")
	t.flushCharRef()
	t.state = t.returnState
}

// stateDigitsCharRef accumulates digits for a numeric reference in the given base.
func (t *tokenizer) stateDigitsCharRef(base uint32) {
	c := t.consume()
	var d uint32
	switch {
	case base == 16 && ascii.IsHex(c):
		d = hexDigitValue(c)
	case base == 10 && ascii.IsDigit(c):
		d = hexDigitValue(c)
	case c == ';':
		t.state = sNumericCharRefEnd
		return
	case c == eof:
		t.error("missing-semicolon-after-character-reference")
		t.state = sNumericCharRefEnd
		return
	default:
		t.error("missing-semicolon-after-character-reference")
		t.reconsume(c)
		t.state = sNumericCharRefEnd
		return
	}
	t.charRefCode = saturatingAdd(saturatingMul(t.charRefCode, base), d)
}

// saturatingMul returns the product of a and b clamped to MaxUint32.
func saturatingMul(a, b uint32) uint32 {
	r := uint64(a) * uint64(b)
	if r > 0xFFFFFFFF {
		return 0xFFFFFFFF
	}
	return uint32(r)
}

// saturatingAdd returns the sum of a and b clamped to MaxUint32.
func saturatingAdd(a, b uint32) uint32 {
	r := uint64(a) + uint64(b)
	if r > 0xFFFFFFFF {
		return 0xFFFFFFFF
	}
	return uint32(r)
}

// stateNumericCharRefEnd validates the numeric reference and emits its replacement character.
func (t *tokenizer) stateNumericCharRefEnd() {
	code := t.charRefCode
	var r rune
	switch {
	case code == 0:
		t.error("null-character-reference")
		r = '�'
	case code > 0x10FFFF:
		t.error("character-reference-outside-unicode-range")
		r = '�'
	case code >= 0xD800 && code <= 0xDFFF:
		t.error("surrogate-character-reference")
		r = '�'
	case isNoncharacter(code):
		t.error("noncharacter-character-reference")
		r = rune(code)
	case code == 0x0D || (isControl(code) && !isASCIIWhitespaceCode(code)):
		t.error("control-character-reference")
		r = numericRefReplacement(code)
	default:
		r = rune(code)
	}
	t.tempBuffer.Reset()
	t.tempBuffer.WriteRune(r)
	t.flushCharRef()
	t.state = t.returnState
}

// isNoncharacter reports whether code is a Unicode noncharacter.
func isNoncharacter(code uint32) bool {
	if code >= 0xFDD0 && code <= 0xFDEF {
		return true
	}
	return code <= 0x10FFFF && (code&0xFFFE) == 0xFFFE
}

// isControl reports whether code is a C0 or DEL/C1 control code.
func isControl(code uint32) bool { return code <= 0x1F || (code >= 0x7F && code <= 0x9F) }

// isASCIIWhitespaceCode reports whether code is ASCII whitespace.
func isASCIIWhitespaceCode(code uint32) bool {
	return code == 0x09 || code == 0x0A || code == 0x0C || code == 0x0D || code == 0x20
}

// numericRefReplacement returns the Windows-1252 replacement for a C1 code, otherwise the code itself.
func numericRefReplacement(code uint32) rune {
	switch code {
	case 0x80:
		return '€'
	case 0x82:
		return '‚'
	case 0x83:
		return 'ƒ'
	case 0x84:
		return '„'
	case 0x85:
		return '…'
	case 0x86:
		return '†'
	case 0x87:
		return '‡'
	case 0x88:
		return 'ˆ'
	case 0x89:
		return '‰'
	case 0x8A:
		return 'Š'
	case 0x8B:
		return '‹'
	case 0x8C:
		return 'Œ'
	case 0x8E:
		return 'Ž'
	case 0x91:
		return '‘'
	case 0x92:
		return '’'
	case 0x93:
		return '“'
	case 0x94:
		return '”'
	case 0x95:
		return '•'
	case 0x96:
		return '–'
	case 0x97:
		return '—'
	case 0x98:
		return '˜'
	case 0x99:
		return '™'
	case 0x9A:
		return 'š'
	case 0x9B:
		return '›'
	case 0x9C:
		return 'œ'
	case 0x9E:
		return 'ž'
	case 0x9F:
		return 'Ÿ'
	}
	return rune(code) //nolint:gosec // G115: callers pass code <= 0x10FFFF
}

// lookupEntity returns the replacement of a named character reference
// (name without its semicolon), using the standard library's copy of the
// WHATWG table. html.UnescapeString falls back to a legacy prefix when the
// full name is unknown; that fallback leaves the semicolon in place, so a
// result ending in ';' is not an exact match (except for &semi;).
func lookupEntity(name string) (string, bool) {
	in := "&" + name + ";"
	out := html.UnescapeString(in)
	if out == in || (strings.HasSuffix(out, ";") && name != "semi") {
		return "", false
	}
	return out, true
}

// isLegacyEntity reports whether name is one of the references recognised
// without a semicolon. Their replacements never end in an ASCII
// alphanumeric, while the standard library's shorter-prefix fallback
// leaves the rest of the name (alphanumerics) in place.
func isLegacyEntity(name string) bool {
	in := "&" + name
	out := html.UnescapeString(in)
	if out == in || out == "" {
		return false
	}
	last := out[len(out)-1]
	return !(last < 0x80 && ascii.IsAlnum(rune(last)))
}

// hexDigitValue returns the value of an ASCII hexadecimal digit, or zero otherwise.
func hexDigitValue(r rune) uint32 {
	switch {
	case r >= '0' && r <= '9':
		return uint32(r - '0')
	case r >= 'a' && r <= 'f':
		return uint32(r-'a') + 10
	case r >= 'A' && r <= 'F':
		return uint32(r-'A') + 10
	}
	return 0
}
