package wireauth_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"testing"

	"go.airlance.org/server/internal/transport/wireauth"
)

type pipe struct {
	in  chan []byte
	out chan []byte
	hit func([]byte) []byte
}

func newPipes() (server, client *pipe) {
	toServer := make(chan []byte, 4)
	toClient := make(chan []byte, 4)

	return &pipe{in: toServer, out: toClient}, &pipe{in: toClient, out: toServer}
}

func (p *pipe) ReadMessage() (int, []byte, error) {
	message := <-p.in
	if p.hit != nil {
		message = p.hit(message)
	}

	return wireauth.BinaryMessageType, message, nil
}

func (p *pipe) WriteMessage(_ int, payload []byte) error {
	p.out <- append([]byte(nil), payload...)

	return nil
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	return public, private
}

type outcome struct {
	channel *wireauth.Channel
	err     error
}

func serve(t *testing.T, keys map[uint32]ed25519.PrivateKey, conn *pipe) chan outcome {
	t.Helper()

	server, err := wireauth.NewServer(keys)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan outcome, 1)

	go func() {
		channel, err := server.Perform(conn)
		done <- outcome{channel, err}
	}()

	return done
}

func establish(t *testing.T) (server, client *wireauth.Channel) {
	t.Helper()

	public, private := newKey(t)
	serverSide, clientSide := newPipes()
	done := serve(t, map[uint32]ed25519.PrivateKey{1: private}, serverSide)

	client, err := wireauth.Client(clientSide, map[uint32]ed25519.PublicKey{1: public}, []uint32{1})
	if err != nil {
		t.Fatal(err)
	}

	finished := <-done
	if finished.err != nil {
		t.Fatal(finished.err)
	}

	return finished.channel, client
}

func TestBothDirectionsWork(t *testing.T) {
	t.Parallel()

	server, client := establish(t)

	got, err := server.Open(client.Seal([]byte("привет")))
	if err != nil || string(got) != "привет" {
		t.Fatalf("to the server: %q, %v", got, err)
	}

	got, err = client.Open(server.Seal([]byte("ответ")))
	if err != nil || string(got) != "ответ" {
		t.Fatalf("to the client: %q, %v", got, err)
	}
}

func TestReplayedAndReorderedPacketsAreRefused(t *testing.T) {
	t.Parallel()

	server, client := establish(t)
	first := client.Seal([]byte("1"))
	second := client.Seal([]byte("2"))

	if _, err := server.Open(second); !errors.Is(err, wireauth.ErrOutOfOrder) {
		t.Fatalf("skipped ahead: %v", err)
	}

	if _, err := server.Open(first); err != nil {
		t.Fatal(err)
	}

	if _, err := server.Open(first); !errors.Is(err, wireauth.ErrOutOfOrder) {
		t.Fatalf("replayed: %v", err)
	}

	if _, err := server.Open(second); err != nil {
		t.Fatalf("the next packet after a refusal: %v", err)
	}
}

func TestReflectedAndDamagedPacketsAreRefused(t *testing.T) {
	t.Parallel()

	server, client := establish(t)

	reflected := client.Seal([]byte("x"))
	if _, err := client.Open(reflected); !errors.Is(err, wireauth.ErrDecryptionFailed) {
		t.Fatalf("reflected: %v", err)
	}

	if _, err := server.Open(reflected); err != nil {
		t.Fatal(err)
	}

	damaged := client.Seal([]byte("x"))
	damaged[len(damaged)-1] ^= 1
	if _, err := server.Open(damaged); !errors.Is(err, wireauth.ErrDecryptionFailed) {
		t.Fatalf("damaged: %v", err)
	}

	if _, err := server.Open([]byte{1, 2, 3}); !errors.Is(err, wireauth.ErrPacketTooShort) {
		t.Fatalf("short: %v", err)
	}
}

func TestTheServerSignsWithTheFirstKeyItHolds(t *testing.T) {
	t.Parallel()

	publicOld, privateOld := newKey(t)
	publicNew, _ := newKey(t)
	serverSide, clientSide := newPipes()
	done := serve(t, map[uint32]ed25519.PrivateKey{1: privateOld}, serverSide)
	pins := map[uint32]ed25519.PublicKey{1: publicOld, 2: publicNew}

	if _, err := wireauth.Client(clientSide, pins, []uint32{2, 1}); err != nil {
		t.Fatal(err)
	}

	if finished := <-done; finished.err != nil {
		t.Fatal(finished.err)
	}
}

func TestNoCommonKeyEndsTheHandshake(t *testing.T) {
	t.Parallel()

	_, private := newKey(t)
	serverSide, clientSide := newPipes()
	done := serve(t, map[uint32]ed25519.PrivateKey{1: private}, serverSide)

	hello := binary.LittleEndian.AppendUint32(nil, 201)
	hello = append(hello, make([]byte, 16)...)
	hello = append(hello, 1)
	hello = binary.LittleEndian.AppendUint32(hello, 9)
	if err := clientSide.WriteMessage(wireauth.BinaryMessageType, hello); err != nil {
		t.Fatal(err)
	}

	if finished := <-done; !errors.Is(finished.err, wireauth.ErrNoCommonKey) {
		t.Fatalf("got %v", finished.err)
	}
}

func TestAPinOtherThanTheSignerIsRefused(t *testing.T) {
	t.Parallel()

	wrong, _ := newKey(t)
	_, private := newKey(t)
	serverSide, clientSide := newPipes()
	serve(t, map[uint32]ed25519.PrivateKey{1: private}, serverSide)

	_, err := wireauth.Client(clientSide, map[uint32]ed25519.PublicKey{1: wrong}, []uint32{1})
	if !errors.Is(err, wireauth.ErrInvalidSignature) {
		t.Fatalf("got %v", err)
	}
}

func TestTamperingWithAnyMessageBreaksTheSignature(t *testing.T) {
	t.Parallel()

	for name, tamper := range map[string]func(*pipe, *pipe){
		"the offered keys": func(server, _ *pipe) {
			server.hit = flipOnce(func(message []byte) bool {
				return len(message) > 21 && message[0] == 201
			}, func(message []byte) []byte {
				message[21] = 5

				return message
			})
		},
		"the server nonce": func(_, client *pipe) {
			client.hit = flipOnce(func(message []byte) bool {
				return len(message) == 20
			}, func(message []byte) []byte {
				message[0] ^= 1

				return message
			})
		},
	} {
		public, private := newKey(t)
		serverSide, clientSide := newPipes()
		tamper(serverSide, clientSide)
		serve(t, map[uint32]ed25519.PrivateKey{1: private}, serverSide)

		_, err := wireauth.Client(clientSide, map[uint32]ed25519.PublicKey{1: public}, []uint32{7, 1})
		if !errors.Is(err, wireauth.ErrInvalidSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func flipOnce(match func([]byte) bool, change func([]byte) []byte) func([]byte) []byte {
	done := false

	return func(message []byte) []byte {
		if done || !match(message) {
			return message
		}

		done = true

		return change(message)
	}
}

func TestBrokenHellosAreRefused(t *testing.T) {
	t.Parallel()

	_, private := newKey(t)
	keys := map[uint32]ed25519.PrivateKey{1: private}

	for name, hello := range map[string][]byte{
		"empty":     {},
		"short":     {201, 0, 0, 0},
		"the old":   append(binary.LittleEndian.AppendUint32(nil, 101), make([]byte, 16)...),
		"no keys":   append(binary.LittleEndian.AppendUint32(nil, 201), make([]byte, 17)...),
		"too many":  append(append(binary.LittleEndian.AppendUint32(nil, 201), make([]byte, 16)...), append([]byte{9}, make([]byte, 36)...)...),
		"truncated": append(append(binary.LittleEndian.AppendUint32(nil, 201), make([]byte, 16)...), 2, 1, 0, 0, 0),
	} {
		serverSide, clientSide := newPipes()
		done := serve(t, keys, serverSide)
		if err := clientSide.WriteMessage(wireauth.BinaryMessageType, hello); err != nil {
			t.Fatal(err)
		}

		if finished := <-done; !errors.Is(finished.err, wireauth.ErrInvalidHandshakePacket) {
			t.Errorf("%s: %v", name, finished.err)
		}
	}
}

func TestAServerNeedsAValidKey(t *testing.T) {
	t.Parallel()

	if _, err := wireauth.NewServer(nil); !errors.Is(err, wireauth.ErrInvalidSigningKey) {
		t.Fatalf("no keys: %v", err)
	}

	if _, err := wireauth.NewServer(map[uint32]ed25519.PrivateKey{1: {1, 2}}); !errors.Is(err, wireauth.ErrInvalidSigningKey) {
		t.Fatalf("a short key: %v", err)
	}
}
