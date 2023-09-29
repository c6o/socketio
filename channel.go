package shadiaosocketio

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Baiguoshuai1/shadiaosocketio/protocol"
	"github.com/Baiguoshuai1/shadiaosocketio/utils"
	"github.com/Baiguoshuai1/shadiaosocketio/websocket"
	gorillaws "github.com/gorilla/websocket"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	queueBufferSize = 10000
)
const (
	DefaultCloseTxt  = "transport close"
	DefaultCloseCode = 101
)

var (
	ErrorWrongHeader = errors.New("Wrong header")
)

/*
*
engine.io header to send or receive
*/
type Header struct {
	Sid          string   `json:"sid"`
	Upgrades     []string `json:"upgrades"`
	PingInterval int      `json:"pingInterval"`
	PingTimeout  int      `json:"pingTimeout"`
}

const (
	ChannelStateConnecting = iota
	ChannelStateConnected
	ChannelStateClosing
	ChannelStateClosed
)

/*
*
socket.io connection handler

use IsConnected to check that handler is still working
use Dial to connect to websocket
use In and Out channels for message exchange
Close message means channel is closed
ping is automatic
*/
type Channel struct {
	Backoff func(int) time.Duration

	conn *websocket.Connection

	out    chan interface{}
	header Header

	state atomic.Uint32

	ack ackProcessor

	server  *Server
	ip      string
	request *http.Request

	pingChan chan bool
}

func (c *Channel) BinaryMessage() bool {
	return c.conn.GetUseBinaryMessage()
}

func (c *Channel) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *Channel) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *Channel) initChannel() {
	c.pingChan = make(chan bool, 1)
	c.out = make(chan interface{}, queueBufferSize)
	c.state.Store(ChannelStateConnecting)
}

func (c *Channel) Id() string {
	return c.header.Sid
}

func (c *Channel) ReadBytes() int {
	return c.conn.GetReadBytes()
}

func (c *Channel) WriteBytes() int {
	return c.conn.GetWriteBytes()
}

func (c *Channel) IsConnected() bool {
	return c.state.Load() == ChannelStateConnected
}

func reconnectChannel(c *Channel, m *methods) error {
	if !(c.state.Load() == ChannelStateConnected) {
		return nil
	}

	c.state.Store(ChannelStateConnecting)

	retry := 1
	for {
		utils.Debug(fmt.Sprintf("[reconnect retry] attempt %d", retry))
		delay := time.Duration(0)
		if c.Backoff != nil {
			delay = c.Backoff(retry)
		} else {
			// exponential (^2) backoff, minimum 2 seconds, maximum ∑2n² to ∑n(2n + 4)
			delay = time.Duration(retry*(retry*2000+rand.Intn(4000))) * time.Millisecond
		}
		time.Sleep(delay)
		err := c.conn.Reconnect()
		if err != nil {
			if retry >= 5 {
				return err
			}
			retry++
			continue
		}
		break
	}

	m.callLoopEvent(c, OnReconnection)

	return nil
}

/*
*
Close channel
*/
func closeChannel(c *Channel, m *methods, args ...interface{}) error {
	if c.state.Load() >= ChannelStateClosing {
		return nil
	}

	c.state.Store(ChannelStateClosing)

	var s []interface{}
	closeErr := &websocket.CloseError{}

	if len(args) == 0 {
		closeErr.Code = DefaultCloseCode
		closeErr.Text = DefaultCloseTxt

		s = append(s, closeErr)
	} else {
		s = append(s, args...)
	}

	c.conn.Close()

	//clean outloop
	for len(c.out) > 0 {
		<-c.out
	}

	c.state.Store(ChannelStateClosed)
	m.callLoopEvent(c, OnDisconnection, s...)

	return nil
}

// incoming messages loop, puts incoming messages to In channel
func inLoop(c *Channel, m *methods) error {
	for {
		if c.state.Load() != ChannelStateConnected {
			// prevents too many reads from the failed socket panic during reconnect
			time.Sleep(200 * time.Millisecond)
		}

		msg, err := c.conn.GetMessage()
		if err != nil {
			err := handleReadError(err, c, m)
			if err != nil {
				return closeChannel(c, m, err)
			}
			continue
		}
		prefix := string(msg[0])
		protocolV := c.conn.GetProtocol()

		switch prefix {
		case protocol.OpenMsg:
			if err := utils.Json.UnmarshalFromString(msg[1:], &c.header); err != nil {
				closeErr := &websocket.CloseError{}
				closeErr.Code = websocket.ParseOpenMsgCode
				closeErr.Text = err.Error()

				return closeChannel(c, m, closeErr)
			}

			if protocolV == protocol.Protocol3 {
				c.state.Store(ChannelStateConnected)
				m.callLoopEvent(c, OnConnection)
				// in protocol v3, the client sends a ping, and the server answers with a pong
				go SchedulePing(c)
			}
			if c.conn.GetProtocol() == protocol.Protocol4 {
				params := make(map[string]interface{})
				err := json.Unmarshal([]byte(msg[1:]), &params)
				if err != nil {
					return closeChannel(c, m, err)
				}

				c.header.Sid = params["sid"].(string)
				if v, ok := params["pingInterval"]; ok {
					c.header.PingInterval = int(v.(float64))
				} else {
					c.header.PingInterval = 25000
				}
				if v, ok := params["pingTimeout"]; ok {
					c.header.PingTimeout = int(v.(float64))
				} else {
					c.header.PingTimeout = 20000
				}

				go func() {
					// treat the connection establish as first ping
					c.pingChan <- true
				}()

				// in protocol v4 & binary msg Connection to a namespace
				if c.conn.GetUseBinaryMessage() {
					c.out <- &protocol.MsgPack{
						Type: protocol.CONNECT,
						Nsp:  protocol.DefaultNsp,
						Data: &struct {
						}{},
					}
					// in protocol v4 & text msg Connection to a namespace
				} else {
					c.out <- protocol.CommonMsg + protocol.OpenMsg
				}
			}
		case protocol.CloseMsg:
			reconnectErr := reconnectChannel(c, m)
			if reconnectErr != nil {
				return closeChannel(c, m)
			}
		case protocol.PingMsg:
			go func() {
				c.pingChan <- true
			}()
			// in protocol v4, the server sends a ping, and the client answers with a pong
			c.out <- protocol.PongMsg
		case protocol.PongMsg:
		case protocol.UpgradeMsg:
		case protocol.CommonMsg:
			// in protocol v3 & binary msg  ps: 4{"type":0,"data":null,"nsp":"/","id":0}
			// in protocol v3 & text msg  ps: 40 or 41 or 42["message", ...]
			// in protocol v4 & text msg  ps: 40 or 41 or 42["message", ...]
			go m.processIncomingMessage(c, msg[1:])
		default:
			// in protocol v4 & binary msg ps: {"type":0,"data":{"sid":"HWEr440000:1:R1CHyink:shadiao:101"},"nsp":"/","id":0}
			go m.processIncomingMessage(c, msg)
		}
	}
}

func handleReadError(err error, c *Channel, m *methods) error {
	if strings.Contains(err.Error(), "use of closed network connection") {
		return nil
	}

	if e, ok := err.(*gorillaws.CloseError); ok && e.Code > 1000 {
		reconnectErr := reconnectChannel(c, m)
		if reconnectErr != nil {
			return err
		} else {
			return nil
		}
	} else {
		return err
	}
}

func outLoop(c *Channel, m *methods) error {
	var buffer []interface{}
	for {
		if c.state.Load() >= ChannelStateClosed {
			return nil
		}

		msg := <-c.out

		if msg == protocol.CloseMsg {
			return closeChannel(c, m)
		}

		if msg == protocol.CommonMsg+protocol.OpenMsg {
			err := c.conn.WriteMessage(msg)
			if err != nil {
				return closeChannel(c, m, err)
			}
			continue
		}

		var msgpack *protocol.MsgPack
		if v, ok := msg.(*protocol.MsgPack); ok {
			msgpack = v
		}

		if msgpack != nil && msgpack.Type == protocol.CONNECT {
			err := c.conn.WriteMessage(msg)
			if err != nil {
				return closeChannel(c, m, err)
			}
			continue
		}

		buffer = append(buffer, msg)

		if len(buffer) >= queueBufferSize-1 {
			closeErr := &websocket.CloseError{}
			closeErr.Code = websocket.QueueBufferSizeCode
			closeErr.Text = ErrorSocketOverflood.Error()

			return closeChannel(c, m, closeErr)
		}

		if c.state.Load() != ChannelStateConnected {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		processed := 0
		for _, bm := range buffer {
			err := c.conn.WriteMessage(bm)
			if err == nil {
				processed++
			} else {
				break
			}
			if bm == protocol.CloseMsg {
				return nil
			}
		}

		buffer = buffer[processed:]
	}
}

func pingLoop(c *Channel, m *methods) {
	for {
		state := c.state.Load()
		if state >= ChannelStateClosing {
			return
		}

		if state != ChannelStateConnected {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		select {
		case <-c.pingChan:
			continue
		case <-time.After(time.Duration(c.header.PingInterval+c.header.PingTimeout) * time.Millisecond):
			if c.state.Load() != ChannelStateConnected {
				continue
			}
			utils.Debug("[ping timeout]")
			reconnectErr := reconnectChannel(c, m)
			if reconnectErr != nil {
				closeChannel(c, m, errors.New("ping timeout"))
				return
			}
		}
	}

}

func SchedulePing(c *Channel) {
	interval, _ := c.conn.PingParams()
	ticker := time.NewTicker(interval)
	for {
		<-ticker.C
		if !c.IsConnected() {
			return
		}
		c.out <- protocol.PingMsg
	}
}
