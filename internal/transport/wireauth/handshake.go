package wireauth

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	BinaryMessageType = 2

	cmdHello    = 201
	cmdExchange = 202

	commandSize    = 4
	nonceSize      = 16
	keyIDSize      = 4
	publicKeySize  = 65
	helloFixedSize = commandSize + nonceSize + 1
	maxOfferedKeys = 8
	serverHelloLen = nonceSize + keyIDSize
	exchangeSize   = commandSize + publicKeySize
	serverKeyLen   = publicKeySize + ed25519.SignatureSize
	trafficKeySize = 32

	transcriptLabel = "airlance-wireauth/v3/transcript"
	signatureLabel  = "airlance-wireauth/v3/signature"
	clientKeyLabel  = "airlance-wireauth/v3/client-to-server"
	serverKeyLabel  = "airlance-wireauth/v3/server-to-client"
)

var (
	ErrHandshakeReadFailed    = errors.New("wireauth: failed to read handshake message")
	ErrHandshakeWriteFailed   = errors.New("wireauth: failed to write handshake message")
	ErrInvalidHandshakePacket = errors.New("wireauth: invalid handshake packet")
	ErrInvalidPublicKey       = errors.New("wireauth: invalid ECDH public key")
	ErrInvalidSigningKey      = errors.New("wireauth: invalid signing key")
	ErrNoCommonKey            = errors.New("wireauth: no common signing key")
	ErrInvalidSignature       = errors.New("wireauth: invalid signature")
)

type MessageReadWriter interface {
	ReadMessage() (messageType int, payload []byte, err error)
	WriteMessage(messageType int, payload []byte) error
}

type Server struct {
	keys map[uint32]ed25519.PrivateKey
}

func NewServer(keys map[uint32]ed25519.PrivateKey) (*Server, error) {
	if len(keys) == 0 {
		return nil, ErrInvalidSigningKey
	}

	for _, key := range keys {
		if len(key) != ed25519.PrivateKeySize {
			return nil, ErrInvalidSigningKey
		}
	}

	return &Server{keys: keys}, nil
}

func (s *Server) Perform(conn MessageReadWriter) (*Channel, error) {
	hello, err := readMessage(conn)
	if err != nil {
		return nil, err
	}

	clientNonce, offered, err := parseHello(hello)
	if err != nil {
		return nil, err
	}

	keyID, key, err := s.choose(offered)
	if err != nil {
		return nil, err
	}

	serverHello := make([]byte, nonceSize, serverHelloLen)
	if _, err := rand.Read(serverHello); err != nil {
		return nil, fmt.Errorf("wireauth: failed to generate nonce: %w", err)
	}

	serverNonce := serverHello[:nonceSize]
	serverHello = binary.LittleEndian.AppendUint32(serverHello, keyID)
	if err := writeMessage(conn, serverHello); err != nil {
		return nil, err
	}

	exchange, err := readMessage(conn)
	if err != nil {
		return nil, err
	}

	if len(exchange) != exchangeSize || binary.LittleEndian.Uint32(exchange) != cmdExchange {
		return nil, ErrInvalidHandshakePacket
	}

	sharedSecret, serverPublic, err := respondECDH(exchange[commandSize:])
	if err != nil {
		return nil, err
	}

	hash := transcript(hello, serverHello, exchange, serverPublic)
	signature := ed25519.Sign(key, signedMessage(hash))
	if err := writeMessage(conn, append(serverPublic, signature...)); err != nil {
		return nil, err
	}

	clientToServer, serverToClient, err := deriveKeys(sharedSecret, clientNonce, serverNonce, hash)
	if err != nil {
		return nil, err
	}

	return newChannel(serverToClient, clientToServer)
}

func Client(conn MessageReadWriter, pins map[uint32]ed25519.PublicKey, offered []uint32) (*Channel, error) {
	hello, clientNonce, err := buildHello(offered)
	if err != nil {
		return nil, err
	}

	if err := writeMessage(conn, hello); err != nil {
		return nil, err
	}

	serverHello, err := readMessage(conn)
	if err != nil {
		return nil, err
	}

	if len(serverHello) != serverHelloLen {
		return nil, ErrInvalidHandshakePacket
	}

	pin, known := pins[binary.LittleEndian.Uint32(serverHello[nonceSize:])]
	if !known {
		return nil, ErrNoCommonKey
	}

	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("wireauth: failed to generate ECDH key: %w", err)
	}

	exchange := binary.LittleEndian.AppendUint32(nil, cmdExchange)
	exchange = append(exchange, private.PublicKey().Bytes()...)
	if err := writeMessage(conn, exchange); err != nil {
		return nil, err
	}

	reply, err := readMessage(conn)
	if err != nil {
		return nil, err
	}

	if len(reply) != serverKeyLen {
		return nil, ErrInvalidHandshakePacket
	}

	serverPublic := reply[:publicKeySize]
	hash := transcript(hello, serverHello, exchange, serverPublic)
	if !ed25519.Verify(pin, signedMessage(hash), reply[publicKeySize:]) {
		return nil, ErrInvalidSignature
	}

	peer, err := ecdh.P256().NewPublicKey(serverPublic)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPublicKey, err)
	}

	sharedSecret, err := private.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("wireauth: ECDH failed: %w", err)
	}

	clientToServer, serverToClient, err := deriveKeys(sharedSecret, clientNonce, serverHello[:nonceSize], hash)
	if err != nil {
		return nil, err
	}

	return newChannel(clientToServer, serverToClient)
}

func buildHello(offered []uint32) (hello, clientNonce []byte, err error) {
	if len(offered) == 0 || len(offered) > maxOfferedKeys {
		return nil, nil, ErrNoCommonKey
	}

	hello = binary.LittleEndian.AppendUint32(nil, cmdHello)
	clientNonce = make([]byte, nonceSize)
	if _, err := rand.Read(clientNonce); err != nil {
		return nil, nil, fmt.Errorf("wireauth: failed to generate nonce: %w", err)
	}

	hello = append(hello, clientNonce...)
	hello = append(hello, byte(len(offered)))
	for _, id := range offered {
		hello = binary.LittleEndian.AppendUint32(hello, id)
	}

	return hello, clientNonce, nil
}

func parseHello(hello []byte) (clientNonce []byte, offered []uint32, err error) {
	if len(hello) < helloFixedSize || binary.LittleEndian.Uint32(hello) != cmdHello {
		return nil, nil, ErrInvalidHandshakePacket
	}

	count := int(hello[helloFixedSize-1])
	if count == 0 || count > maxOfferedKeys || len(hello) != helloFixedSize+count*keyIDSize {
		return nil, nil, ErrInvalidHandshakePacket
	}

	offered = make([]uint32, count)
	for i := range offered {
		offered[i] = binary.LittleEndian.Uint32(hello[helloFixedSize+i*keyIDSize:])
	}

	return hello[commandSize : commandSize+nonceSize], offered, nil
}

func (s *Server) choose(offered []uint32) (uint32, ed25519.PrivateKey, error) {
	for _, id := range offered {
		if key, held := s.keys[id]; held {
			return id, key, nil
		}
	}

	return 0, nil, ErrNoCommonKey
}

func respondECDH(clientPublic []byte) (sharedSecret, serverPublic []byte, err error) {
	curve := ecdh.P256()

	peer, err := curve.NewPublicKey(clientPublic)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidPublicKey, err)
	}

	private, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("wireauth: failed to generate ECDH key: %w", err)
	}

	sharedSecret, err = private.ECDH(peer)
	if err != nil {
		return nil, nil, fmt.Errorf("wireauth: ECDH failed: %w", err)
	}

	return sharedSecret, private.PublicKey().Bytes(), nil
}

func transcript(hello, serverHello, exchange, serverPublic []byte) []byte {
	hash := sha256.New()
	for _, part := range [][]byte{[]byte(transcriptLabel), hello, serverHello, exchange, serverPublic} {
		hash.Write(part)
	}

	return hash.Sum(nil)
}

func signedMessage(transcriptHash []byte) []byte {
	return append([]byte(signatureLabel), transcriptHash...)
}

func deriveKeys(sharedSecret, clientNonce, serverNonce, transcriptHash []byte) (clientToServer, serverToClient []byte, err error) {
	salt := append(append([]byte(nil), clientNonce...), serverNonce...)

	clientToServer, err = hkdf.Key(sha256.New, sharedSecret, salt, clientKeyLabel+string(transcriptHash), trafficKeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("wireauth: key derivation failed: %w", err)
	}

	serverToClient, err = hkdf.Key(sha256.New, sharedSecret, salt, serverKeyLabel+string(transcriptHash), trafficKeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("wireauth: key derivation failed: %w", err)
	}

	return clientToServer, serverToClient, nil
}

func readMessage(conn MessageReadWriter) ([]byte, error) {
	kind, payload, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshakeReadFailed, err)
	}

	if kind != BinaryMessageType {
		return nil, ErrInvalidHandshakePacket
	}

	return payload, nil
}

func writeMessage(conn MessageReadWriter, payload []byte) error {
	if err := conn.WriteMessage(BinaryMessageType, payload); err != nil {
		return fmt.Errorf("%w: %w", ErrHandshakeWriteFailed, err)
	}

	return nil
}
