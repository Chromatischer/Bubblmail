package embeddings

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []float32
		b    []float32
		want float32
	}{
		{name: "same direction", a: []float32{1, 0}, b: []float32{2, 0}, want: 1},
		{name: "orthogonal", a: []float32{1, 0}, b: []float32{0, 1}, want: 0},
		{name: "zero vector", a: []float32{0, 0}, b: []float32{1, 1}, want: 0},
		{name: "mismatched dimensions", a: []float32{1}, b: []float32{1, 2}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, 0, tt.b, 0)
			if math.Abs(float64(got-tt.want)) > 1e-6 {
				t.Fatalf("CosineSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncodeDecodeVectorRoundTrip(t *testing.T) {
	want := []float32{0, 1.25, -3.5, float32(math.Pi)}

	encoded, err := EncodeVector(want)
	if err != nil {
		t.Fatalf("EncodeVector: %v", err)
	}
	got, err := DecodeVector(encoded)
	if err != nil {
		t.Fatalf("DecodeVector: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("decoded length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("decoded[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestDecodeVectorRejectsInvalidLength(t *testing.T) {
	if _, err := DecodeVector([]byte{1, 2, 3}); err == nil {
		t.Fatal("DecodeVector() error = nil, want invalid length error")
	}
}
