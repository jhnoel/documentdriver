// Package opc reads and writes the OPC zip package. Members are read lazily,
// untouched members are copied with zip.Writer.Copy so their compressed bytes
// survive, a save goes to a temporary file that is renamed over the original,
// and a no-op save writes nothing.
package opc
