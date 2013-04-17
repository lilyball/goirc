package irc

import (
	"github.com/kballard/gocallback/callback"
	"sync"
)

// SafeConn is a set of methods that may be called from any goroutine. They
// mostly mirror methods from Conn directly, but with a bool return value. This
// return value is false if the connection was already closed, or true if the
// write succeeded (note: this does not mean the server successfully received
// the message).
type SafeConn interface {
	// Me returns the user at the time the SafeConn was created
	Me() User
	// Server returns the host:port pair that identifies the server
	Server() string

	// Connected returns whether the connection is still connected
	Connected() bool

	// Invoke runs the given function on the connection's goroutine
	Invoke(func(*Conn)) bool

	// AddHandler is the same as Conn.AddHandler
	AddHandler(name string, f func(*Conn, Line)) callback.CallbackIdentifier

	// RemoveHandler is the same as Conn.RemoveHandler
	RemoveHandler(callback.CallbackIdentifier)

	// Conn methods
	Raw(line string) bool
	Privmsg(dst, msg string) bool
	Action(dst, msg string) bool
	CTCP(dst, command, args string) bool
	CTCPReply(dst, command, args string) bool
	Quit(msg string) bool
	Nick(newnick string) bool
	Join(channels, keys []string) bool
	Part(channels []string, msg string) bool
}

type safeConn struct {
	me    User
	state *safeConnState
}

type safeConnState struct {
	sync.RWMutex
	writer  chan<- string
	invoker chan<- func(*Conn)

	server   string
	registry *callback.Registry
}

// SafeConn returns a SafeConn object that can be passed to another goroutine.
// Note, despite the SafeConn object itself being thread-safe, this method may
// only be called from the connection's goroutine.
func (c *Conn) SafeConn() SafeConn {
	return &safeConn{
		me:    c.me,
		state: c.safeConnState,
	}
}

func (c *safeConn) Me() User {
	return c.me
}

func (c *safeConn) Server() string {
	return c.state.server
}

func (c *safeConn) exec(f func()) bool {
	c.state.RLock()
	defer c.state.RUnlock()
	if c.state.writer != nil {
		f()
		return true
	}
	return false
}

func (c *safeConn) Connected() bool {
	return c.exec(func() {})
}

func (c *safeConn) Invoke(f func(*Conn)) bool {
	return c.exec(func() {
		c.state.invoker <- f
	})
}

func (c *safeConn) AddHandler(name string, f func(*Conn, Line)) callback.CallbackIdentifier {
	return c.state.registry.AddCallback(name, f)
}

func (c *safeConn) RemoveHandler(ident callback.CallbackIdentifier) {
	c.state.registry.RemoveCallback(ident)
}

func (c *safeConn) Raw(msg string) bool {
	return c.exec(func() {
		c.state.writer <- filterMessage(firstLine(msg))
	})
}

func (c *safeConn) Privmsg(dst, msg string) bool {
	return c.exec(func() {
		c.state.writer <- composePrivmsg(dst, msg)
	})
}

func (c *safeConn) Action(dst, msg string) bool {
	return c.exec(func() {
		c.state.writer <- composeCTCP(dst, "ACTION", msg, false)
	})
}

func (c *safeConn) CTCP(dst, command, args string) bool {
	return c.exec(func() {
		c.state.writer <- composeCTCP(dst, command, args, false)
	})
}

func (c *safeConn) CTCPReply(dst, command, args string) bool {
	return c.exec(func() {
		c.state.writer <- composeCTCP(dst, command, args, true)
	})
}

func (c *safeConn) Quit(msg string) bool {
	return c.exec(func() {
		c.state.writer <- composeQuit(msg)
	})
}

func (c *safeConn) Nick(newnick string) bool {
	return c.exec(func() {
		c.state.writer <- composeNick(newnick)
	})
}

func (c *safeConn) Join(channels, keys []string) bool {
	return c.exec(func() {
		if len(channels) > 0 {
			c.state.writer <- composeJoin(channels, keys)
		}
	})
}

func (c *safeConn) Part(channels []string, msg string) bool {
	return c.exec(func() {
		if len(channels) > 0 {
			c.state.writer <- composePart(channels, msg)
		}
	})
}
