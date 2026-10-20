package codec

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"
)

// El simulador copia el mundo con Parts bajo el bloqueo y codifica la copia en
// segundo plano: tiene que dar los mismos bytes que codificar el mundo.
func TestEncodingACopyOfThePartsGivesTheSameRowAsEncodingTheWorld(t *testing.T) {
	w := richWorld(t)
	parts := w.Parts()
	fromParts, err := EncodeParts(parts)
	if err != nil {
		t.Fatal(err)
	}
	fromWorld := mustEncode(t, w)
	if !proto.Equal(fromParts, fromWorld) || !bytes.Equal(fromParts.GetWorldState(), fromWorld.GetWorldState()) {
		t.Fatal("EncodeParts(w.Parts()) debe dar la misma fila que EncodeSnapshot(w)")
	}
}
