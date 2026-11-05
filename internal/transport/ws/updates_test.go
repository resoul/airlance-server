package ws_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/coder/websocket"

	"go.airlance.org/server/internal/transport/codec"
	"go.airlance.org/server/internal/transport/ws"
)

func subscribeFrame(id, after uint64) codec.Frame {
	return codec.Frame{RequestID: id, Kind: codec.KindUpdatesSubscribe, Body: codec.EncodeUpdatesSubscribe(after)}
}

func assertUpdate(t *testing.T, frame codec.Frame, seq uint64, firstName string) {
	t.Helper()

	if frame.Seq != seq || frame.RequestID != 0 || frame.Kind != codec.KindProfileData {
		t.Fatalf("want update %d, got %+v", seq, frame)
	}

	data, err := codec.DecodeProfileData(frame.Body)
	if err != nil || data.Fields.FirstName != firstName {
		t.Fatalf("got %+v, %v", data, err)
	}
}

func TestNoUpdatesArriveWithoutSubscribing(t *testing.T) {
	t.Parallel()

	target := start(t, time.Minute, ws.DefaultLimits())
	c := signedIn(t, target)

	target.updates.publish("Анна")

	pong := c.ask(codec.Frame{RequestID: 2, Kind: codec.KindPing, Body: codec.EncodePing(1)})
	if pong.Kind != codec.KindPong || pong.Seq != 0 {
		t.Fatalf("got %+v", pong)
	}
}

func TestSubscribingCatchesUpThenFollows(t *testing.T) {
	t.Parallel()

	target := start(t, time.Minute, ws.DefaultLimits())
	target.updates.publish("один")
	target.updates.publish("два")
	target.updates.publish("три")

	c := signedIn(t, target)
	response := c.ask(subscribeFrame(2, 1))
	latest, err := codec.DecodeUpdatesSubscribed(response.Body)
	if response.Kind != codec.KindUpdatesSubscribed || err != nil || latest != 3 {
		t.Fatalf("got %+v, %d, %v", response, latest, err)
	}

	assertUpdate(t, c.next(), 2, "два")
	assertUpdate(t, c.next(), 3, "три")

	target.updates.publish("четыре")
	assertUpdate(t, c.next(), 4, "четыре")
}

func TestABacklogLongerThanAPageArrivesWhole(t *testing.T) {
	t.Parallel()

	const total = 250

	target := start(t, time.Minute, ws.DefaultLimits())
	for i := range total {
		target.updates.publish(fmt.Sprint(i + 1))
	}

	c := signedIn(t, target)
	c.ask(subscribeFrame(2, 0))

	for i := range total {
		assertUpdate(t, c.next(), uint64(i+1), fmt.Sprint(i+1))
	}
}

func TestEveryConnectionOfAPersonGetsTheUpdate(t *testing.T) {
	t.Parallel()

	target := start(t, time.Minute, ws.DefaultLimits())
	phone := signedIn(t, target)
	tablet := signedIn(t, target)
	phone.ask(subscribeFrame(2, 0))
	tablet.ask(subscribeFrame(2, 0))

	target.updates.publish("Анна")

	assertUpdate(t, phone.next(), 1, "Анна")
	assertUpdate(t, tablet.next(), 1, "Анна")
}

func TestSubscribingTwiceIsRefused(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))
	c.ask(subscribeFrame(2, 0))
	assertError(t, c.ask(subscribeFrame(3, 0)), 409, "already_subscribed")
}

func TestARefusedSubscriptionMayBeRetried(t *testing.T) {
	t.Parallel()

	target := start(t, time.Minute, ws.DefaultLimits())
	target.updates.publish("Анна")

	c := signedIn(t, target)
	assertError(t, c.ask(subscribeFrame(2, 403)), 403, "forbidden")
	assertError(t, c.ask(subscribeFrame(3, math.MaxUint64)), 400, "invalid_seq")

	broken := codec.Frame{RequestID: 4, Kind: codec.KindUpdatesSubscribe, Body: []byte{1}}
	assertError(t, c.ask(broken), 400, "invalid body")

	c.ask(subscribeFrame(5, 0))
	assertUpdate(t, c.next(), 1, "Анна")
}

func TestUpdatesAndRepliesShareOneOrderedChannel(t *testing.T) {
	t.Parallel()

	const rounds = 40

	target := start(t, time.Minute, ws.DefaultLimits())
	c := signedIn(t, target)
	c.ask(subscribeFrame(2, 0))

	go func() {
		for i := range rounds {
			target.updates.publish(fmt.Sprint(i + 1))
		}
	}()

	for i := range rounds {
		c.send(codec.Frame{RequestID: uint64(i + 3), Kind: codec.KindPing, Body: codec.EncodePing(int64(i))})
	}

	var nextSeq, pongs uint64 = 1, 0
	for nextSeq <= rounds || pongs < rounds {
		frame := c.next()
		if frame.Kind == codec.KindPong {
			pongs++

			continue
		}

		if frame.Seq != nextSeq {
			t.Fatalf("update %d after %d", frame.Seq, nextSeq-1)
		}

		nextSeq++
	}
}

func TestAFailedDeliveryAsksTheClientToReconnect(t *testing.T) {
	t.Parallel()

	target := start(t, time.Minute, ws.DefaultLimits())
	c := signedIn(t, target)
	c.ask(subscribeFrame(2, 0))

	target.updates.breakReads()
	target.updates.publish("Анна")

	if status := closeStatus(t, c); status != websocket.StatusTryAgainLater {
		t.Fatalf("got %d", status)
	}
}

func TestAnExpiringTokenStillClosesWithItsOwnCode(t *testing.T) {
	t.Parallel()

	target := start(t, time.Second, ws.DefaultLimits())
	c := signedIn(t, target)
	c.ask(subscribeFrame(2, 0))

	if status := closeStatus(t, c); status != ws.StatusTokenExpired {
		t.Fatalf("got %d", status)
	}
}
