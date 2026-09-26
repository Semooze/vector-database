package providers

import "testing"

func TestVectorLiteralRejectsNonFiniteValues(t *testing.T) {
	if _, err := vectorLiteral([]float32{1, 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := vectorLiteral(nil); err == nil {
		t.Fatal("empty vector accepted")
	}
}

func TestPineconeIndexUsesModelName(t *testing.T) {
	if got := pineconeIndex("vl_abcdef_qwen"); got != "vectorlab-qwen" {
		t.Fatal(got)
	}
}
