package wireauth

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	seqSize      = 8
	gcmNonceSize = 12
	gcmTagSize   = 16
)

var (
	ErrPacketTooShort   = errors.New("wireauth: packet too short")
	ErrOutOfOrder       = errors.New("wireauth: packet out of order")
	ErrDecryptionFailed = errors.New("wireauth: decryption failed")
)

type Channel struct {
	sending   cipher.AEAD
	receiving cipher.AEAD
	sendSeq   uint64
	recvSeq   uint64
}

func newChannel(sendKey, receiveKey []byte) (*Channel, error) {
	sending, err := newGCM(sendKey)
	if err != nil {
		return nil, err
	}

	receiving, err := newGCM(receiveKey)
	if err != nil {
		return nil, err
	}

	return &Channel{sending: sending, receiving: receiving}, nil
}

func (c *Channel) Seal(plaintext []byte) []byte {
	packet := binary.BigEndian.AppendUint64(make([]byte, 0, seqSize+len(plaintext)+gcmTagSize), c.sendSeq)
	sealed := c.sending.Seal(packet, nonce(c.sendSeq), plaintext, packet[:seqSize])
	c.sendSeq++

	return sealed
}

func (c *Channel) Open(packet []byte) ([]byte, error) {
	if len(packet) < seqSize+gcmTagSize {
		return nil, ErrPacketTooShort
	}

	seq := binary.BigEndian.Uint64(packet)
	if seq != c.recvSeq {
		return nil, ErrOutOfOrder
	}

	plaintext, err := c.receiving.Open(nil, nonce(seq), packet[seqSize:], packet[:seqSize])
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	c.recvSeq++

	return plaintext, nil
}

func nonce(seq uint64) []byte {
	out := make([]byte, gcmNonceSize)
	binary.BigEndian.PutUint64(out[gcmNonceSize-seqSize:], seq)

	return out
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("wireauth: invalid key: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("wireauth: GCM unavailable: %w", err)
	}

	return gcm, nil
}
