package shadiaosocketio

import (
	"errors"
	"github.com/Baiguoshuai1/shadiaosocketio/protocol"
	"log"
	"time"
)

var (
	ErrorSendTimeout     = errors.New("timeout")
	ErrorSocketOverflood = errors.New("socket overflood")
)

type MessageContext struct {
	Priority bool
}

/*
*
Send message packet to socket
*/
func send(c *Channel, msg *protocol.Message, ctx *MessageContext) error {
	defer func() {
		if r := recover(); r != nil {
			log.Println("socket.io send panic: ", r)
		}
	}()

	var out any
	out = protocol.GetMsgPacket(msg)

	if ctx != nil && ctx.Priority {
		out = &protocol.ContextMsgPack{MsgPack: out.(*protocol.MsgPack), Priority: true}
	}

	c.out <- out

	return nil
}

func (c *Channel) Emit(method string, ctx *MessageContext, args ...interface{}) error {
	msg := &protocol.Message{
		Type:   protocol.EVENT,
		AckId:  -1,
		Method: method,
		Nsp:    protocol.DefaultNsp,
		Args:   args,
	}

	return send(c, msg, ctx)
}

func (c *Channel) Ack(method string, timeout time.Duration, ctx *MessageContext, args ...interface{}) (interface{}, error) {
	msg := &protocol.Message{
		Type:   protocol.EVENT,
		AckId:  c.ack.getNextId(),
		Method: method,
		Nsp:    protocol.DefaultNsp,
		Args:   args,
	}

	waiter := make(chan interface{})
	c.ack.addWaiter(msg.AckId, waiter)

	err := send(c, msg, ctx)
	if err != nil {
		c.ack.removeWaiter(msg.AckId)
	}

	select {
	case result := <-waiter:
		c.ack.removeWaiter(msg.AckId)
		return result, nil
	case <-time.After(timeout):
		c.ack.removeWaiter(msg.AckId)
		return nil, ErrorSendTimeout
	}
}
