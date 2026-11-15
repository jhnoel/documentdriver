package htmlx

import "github.com/jhnoel/documentdriver/internal/ascii"

// DOCTYPE states of the tokenizer (WHATWG 13.2.5.53 to 13.2.5.68), ported
// from xmloxide 0.4.3. Only an error-free <!DOCTYPE html> reaches the tree
// builder in practice, but the states decide which inputs are errors.

// doctypeEOF records an eof-in-doctype error, forces quirks, and emits the doctype then EOF.
func (t *tokenizer) doctypeEOF() {
	t.error("eof-in-doctype")
	t.doctypeForceQuirks = true
	t.emitDoctype()
	t.emitEOF()
}

// stateDoctype consumes the character after <!DOCTYPE and advances toward the name or end-of-input.
func (t *tokenizer) stateDoctype() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
		t.state = sBeforeDoctypeName
	case '>':
		t.reconsume('>')
		t.state = sBeforeDoctypeName
	case eof:
		t.error("eof-in-doctype")
		t.createDoctype()
		t.doctypeForceQuirks = true
		t.emitDoctype()
		t.emitEOF()
	default:
		t.error("missing-whitespace-before-doctype-name")
		t.reconsume(c)
		t.state = sBeforeDoctypeName
	}
}

// stateBeforeDoctypeName skips whitespace and starts the doctype name, handling missing or absent names.
func (t *tokenizer) stateBeforeDoctypeName() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
	case 0:
		t.error("unexpected-null-character")
		t.createDoctype()
		t.hasDoctypeName = true
		t.doctypeName.WriteRune('�')
		t.state = sDoctypeName
	case '>':
		t.error("missing-doctype-name")
		t.createDoctype()
		t.doctypeForceQuirks = true
		t.state = sData
		t.emitDoctype()
	case eof:
		t.error("eof-in-doctype")
		t.createDoctype()
		t.doctypeForceQuirks = true
		t.emitDoctype()
		t.emitEOF()
	default:
		t.createDoctype()
		t.hasDoctypeName = true
		t.doctypeName.WriteRune(ascii.LowerRune(c))
		t.state = sDoctypeName
	}
}

// stateDoctypeName accumulates the lowercased doctype name until whitespace, >, or end-of-input.
func (t *tokenizer) stateDoctypeName() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
		t.state = sAfterDoctypeName
	case '>':
		t.state = sData
		t.emitDoctype()
	case 0:
		t.error("unexpected-null-character")
		t.doctypeName.WriteRune('�')
	case eof:
		t.doctypeEOF()
	default:
		t.doctypeName.WriteRune(ascii.LowerRune(c))
	}
}

// stateAfterDoctypeName dispatches after the doctype name to PUBLIC, SYSTEM, or bogus-doctype handling.
func (t *tokenizer) stateAfterDoctypeName() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
	case '>':
		t.state = sData
		t.emitDoctype()
	case eof:
		t.doctypeEOF()
	default:
		t.reconsume(c)
		switch {
		case t.nextCharsASCIIFold("PUBLIC"):
			t.pos += 6
			t.state = sAfterDoctypePublicKeyword
		case t.nextCharsASCIIFold("SYSTEM"):
			t.pos += 6
			t.state = sAfterDoctypeSystemKeyword
		default:
			t.consume()
			t.error("invalid-character-sequence-after-doctype-name")
			t.doctypeForceQuirks = true
			t.state = sBogusDoctype
		}
	}
}

// startDoctypeID begins a public (or system) identifier quoted by quote.
func (t *tokenizer) startDoctypeID(public bool, quote rune) {
	if public {
		t.hasDoctypePublic = true
		t.doctypePublic.Reset()
		t.state = sDoctypePublicIDDouble
		if quote == '\'' {
			t.state = sDoctypePublicIDSingle
		}
		return
	}
	t.hasDoctypeSystem = true
	t.doctypeSystem.Reset()
	t.state = sDoctypeSystemIDDouble
	if quote == '\'' {
		t.state = sDoctypeSystemIDSingle
	}
}

// keywordNames returns the error words and next state for a public or system doctype keyword.
func keywordNames(public bool) (missingWS, missingID, missingQuote string, before state) {
	if public {
		return "missing-whitespace-after-doctype-public-keyword", "missing-doctype-public-identifier",
			"missing-quote-before-doctype-public-identifier", sBeforeDoctypePublicID
	}
	return "missing-whitespace-after-doctype-system-keyword", "missing-doctype-system-identifier",
		"missing-quote-before-doctype-system-identifier", sBeforeDoctypeSystemID
}

// stateAfterDoctypeKeyword handles the character after PUBLIC or SYSTEM, starting the identifier or marking quirks.
func (t *tokenizer) stateAfterDoctypeKeyword(public bool) {
	missingWS, missingID, missingQuote, before := keywordNames(public)
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
		t.state = before
	case '"', '\'':
		t.error(missingWS)
		t.startDoctypeID(public, c)
	case '>':
		t.error(missingID)
		t.doctypeForceQuirks = true
		t.state = sData
		t.emitDoctype()
	case eof:
		t.doctypeEOF()
	default:
		t.error(missingQuote)
		t.doctypeForceQuirks = true
		t.state = sBogusDoctype
	}
}

// stateBeforeDoctypeID skips whitespace and starts a public or system identifier, forcing quirks on bad input.
func (t *tokenizer) stateBeforeDoctypeID(public bool) {
	_, missingID, missingQuote, _ := keywordNames(public)
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
	case '"', '\'':
		t.startDoctypeID(public, c)
	case '>':
		t.error(missingID)
		t.doctypeForceQuirks = true
		t.state = sData
		t.emitDoctype()
	case eof:
		t.doctypeEOF()
	default:
		t.error(missingQuote)
		t.doctypeForceQuirks = true
		t.state = sBogusDoctype
	}
}

// stateDoctypeID accumulates identifier characters until its quote, >, or end-of-input forces quirks.
func (t *tokenizer) stateDoctypeID(public bool, quote rune) {
	id, after, abrupt := &t.doctypeSystem, sAfterDoctypeSystemID, "abrupt-doctype-system-identifier"
	if public {
		id, after, abrupt = &t.doctypePublic, sAfterDoctypePublicID, "abrupt-doctype-public-identifier"
	}
	switch c := t.consume(); c {
	case quote:
		t.state = after
	case 0:
		t.error("unexpected-null-character")
		id.WriteRune('�')
	case '>':
		t.error(abrupt)
		t.doctypeForceQuirks = true
		t.state = sData
		t.emitDoctype()
	case eof:
		t.doctypeEOF()
	default:
		id.WriteRune(c)
	}
}

// stateAfterDoctypePublicID handles the character after a public identifier, expecting whitespace, >, or the system identifier.
func (t *tokenizer) stateAfterDoctypePublicID() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
		t.state = sBetweenDoctypePublicAndSystem
	case '>':
		t.state = sData
		t.emitDoctype()
	case '"', '\'':
		t.error("missing-whitespace-between-doctype-public-and-system-identifiers")
		t.startDoctypeID(false, c)
	case eof:
		t.doctypeEOF()
	default:
		t.error("missing-quote-before-doctype-system-identifier")
		t.doctypeForceQuirks = true
		t.state = sBogusDoctype
	}
}

// stateBetweenDoctypePublicAndSystem skips whitespace between identifiers and starts the system identifier or emits the doctype.
func (t *tokenizer) stateBetweenDoctypePublicAndSystem() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
	case '>':
		t.state = sData
		t.emitDoctype()
	case '"', '\'':
		t.startDoctypeID(false, c)
	case eof:
		t.doctypeEOF()
	default:
		t.error("missing-quote-before-doctype-system-identifier")
		t.doctypeForceQuirks = true
		t.state = sBogusDoctype
	}
}

// stateAfterDoctypeSystemID handles input after the system identifier, emitting the doctype on > or end-of-input.
func (t *tokenizer) stateAfterDoctypeSystemID() {
	switch c := t.consume(); c {
	case '\t', '\n', '\f', ' ':
	case '>':
		t.state = sData
		t.emitDoctype()
	case eof:
		t.doctypeEOF()
	default:
		t.error("unexpected-character-after-doctype-system-identifier")
		t.state = sBogusDoctype
	}
}

// stateBogusDoctype discards input until >, emitting the doctype and EOF on end-of-input.
func (t *tokenizer) stateBogusDoctype() {
	switch c := t.consume(); c {
	case '>':
		t.state = sData
		t.emitDoctype()
	case 0:
		t.error("unexpected-null-character")
	case eof:
		t.emitDoctype()
		t.emitEOF()
	}
}
