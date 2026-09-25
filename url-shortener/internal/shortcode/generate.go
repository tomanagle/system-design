package shortcode

import (
	"crypto/rand"
	"math/big"
)

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// 62 ^ 7 = 3,521,614,606,208
const codeLength = 7

func Generate() string {
	code := make([]byte, 0, codeLength)
	max := big.NewInt(int64(len(base62)))

	for range codeLength {
		position, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		code = append(code, base62[position.Int64()])
	}

	return string(code)
}
