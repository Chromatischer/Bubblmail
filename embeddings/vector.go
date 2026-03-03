package embeddings

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
)

func HashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func VectorNorm(v []float32) float32 {
	var sum float64
	for _, x := range v {
		fx := float64(x)
		sum += fx * fx
	}
	if sum == 0 {
		return 0
	}
	return float32(math.Sqrt(sum))
}

func CosineSimilarity(a []float32, normA float32, b []float32, normB float32) float32 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	if normA == 0 {
		normA = VectorNorm(a)
	}
	if normB == 0 {
		normB = VectorNorm(b)
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	var dot float64
	for i, x := range a {
		dot += float64(x) * float64(b[i])
	}
	return float32(dot / (float64(normA) * float64(normB)))
}

func EncodeVector(v []float32) ([]byte, error) {
	buf := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(buf[i*4:(i+1)*4], math.Float32bits(x))
	}
	return buf, nil
}

func DecodeVector(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, errors.New("invalid vector length")
	}
	count := len(b) / 4
	vec := make([]float32, count)
	for i := 0; i < count; i++ {
		bits := binary.LittleEndian.Uint32(b[i*4 : (i+1)*4])
		vec[i] = math.Float32frombits(bits)
	}
	return vec, nil
}
