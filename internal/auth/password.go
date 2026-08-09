package auth

import "github.com/alexedwards/argon2id"

type Argon2Hasher struct {
	params *argon2id.Params
}

func NewArgon2Hasher() *Argon2Hasher {
	return &Argon2Hasher{params: &argon2id.Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}}
}

func (h *Argon2Hasher) Hash(password string) (string, error) {
	return argon2id.CreateHash(password, h.params)
}

func (h *Argon2Hasher) Verify(password, encodedHash string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, encodedHash)
}
