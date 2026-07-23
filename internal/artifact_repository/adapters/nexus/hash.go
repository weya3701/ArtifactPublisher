package nexus

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
)

type sha256Writer struct{ hash hash.Hash }

func newSHA256Writer() *sha256Writer                   { return &sha256Writer{hash: sha256.New()} }
func (w *sha256Writer) Write(data []byte) (int, error) { return w.hash.Write(data) }
func (w *sha256Writer) Sum() string                    { return hex.EncodeToString(w.hash.Sum(nil)) }
