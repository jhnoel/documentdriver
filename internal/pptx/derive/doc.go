// Package derive computes facts the file already implies: group transforms,
// connector endpoints, connection sites, resolved colours and inherited
// placeholder values. Each is arithmetic or a table lookup and is inverted on
// write where it applies. It never decides a position, route, size or style;
// when a fact cannot be derived faithfully it is omitted, never defaulted.
package derive
