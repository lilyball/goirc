// Package irc implements the IRC protocol and provides callback support.
package irc

import (
	"github.com/kballard/gocallback/callback"
	"net"
	"strings"
	"unicode/utf8"
)

// Special callbacks emitted by the library.
const (
	// Invoked when the connection is established with the server.
	// It is not safe to send messages to the server in response to this. No
	// login has been performed yet.
	// Args: (*Conn)
	INIT = "irc:init"
	// Invoked when the server login has finished. It is now safe to send
	// messages to the server.
	// Args: (*Conn)
	CONNECTED = "irc:connected"
	// Invoked when the connection with the server is terminated.
	// Args: (*Conn)
	DISCONNECTED = "irc:disconnected"
	// Invoked for privmsgs that encode CTCP ACTIONs.
	// Args: (*Conn, Line)
	// The Line will have 1 arg, which is the action text.
	// Line.Dst will contain the original target of the PRIVMSG.
	ACTION = "irc:action"
	// Invoked for privmsgs that encode CTCP messages.
	// Args: (*Conn, Line)
	// The Line will have 1 or 2 args, the first is the CTCP command, the
	// second is the remainder, if any.
	// Line.Dst will contain the original target of the PRIVMSG.
	CTCP = "irc:ctcp"
	// Invoked for notices that encode CTCP messages.
	// Args: (*Conn, Line)
	// The Line will have 1 or 2 args, the first is the CTCP command, the
	// second is the remainder, if any.
	// Line.Dst will contain the original target of the NOTICE.
	CTCPREPLY = "irc:ctcpreply"
)

type HandlerRegistry interface {
	AddHandler(name string, f func(*Conn, Line)) callback.CallbackIdentifier
	RemoveHandler(callback.CallbackIdentifier)
}

// Conn represents a connection to a single IRC server.  The only way to get
// one of these is through a callback.  If a callback wants to pass this to
// another goroutine, it must call the SafeConn() method and use that instead.
type Conn struct {
	me User

	registry      *callback.Registry
	stateRegistry *callback.Registry

	safeConnState *safeConnState

	nickInUse func(string, int) string

	netconn  net.Conn
	writer   chan<- string
	reader   <-chan string
	writeErr <-chan error
	readErr  <-chan error
	invoker  <-chan func(*Conn)
}

// Me returns the User object that represents the client.
// The Nick is guaranteed to be correct. The User/Host portions may not be.
func (c *Conn) Me() User {
	return c.me
}

// Returns the host:port pair for the server.
func (c *Conn) Server() string {
	return c.safeConnState.server
}

// Connected returns whether the Conn is currently connected.
// When the Conn disconnects from the server, it still processes any
// outstanding lines or invokes.
func (c *Conn) Connected() bool {
	return c.netconn != nil
}

// AddHandler adds a handler for an IRC command.
// The return value can be passed to RemoveHandler() later.
func (c *Conn) AddHandler(event string, f func(*Conn, Line)) callback.CallbackIdentifier {
	return c.safeConnState.registry.AddCallback(event, f)
}

// RemoveHandler removes a previously-added handler.
func (c *Conn) RemoveHandler(ident callback.CallbackIdentifier) {
	c.safeConnState.registry.RemoveCallback(ident)
}

// Forcibly terminates the connection.
func (c *Conn) Shutdown() {
	if c.netconn != nil {
		c.netconn.Close()
		c.netconn = nil

		c.safeConnState.Lock()
		close(c.writer)
		c.safeConnState.writer = nil
		c.safeConnState.invoker = nil
		c.safeConnState.Unlock()

		c.safeConnState.registry.Dispatch(DISCONNECTED, c)
	}
}

// Send a raw line to the server.
func (c *Conn) Raw(msg string) {
	c.writer <- filterMessage(firstLine(msg))
}

// Send a PRIVMSG to the server.
func (c *Conn) Privmsg(dst, msg string) {
	c.writer <- composePrivmsg(dst, msg)
}

// Send an action to the server.
func (c *Conn) Action(dst, msg string) {
	c.writer <- composeCTCP(dst, "ACTION", msg, false)
}

// Send a NOTICE to the server.
func (c *Conn) Notice(dst, msg string) {
	c.writer <- composeNotice(dst, msg)
}

// Send a CTCP message to the server.
func (c *Conn) CTCP(dst, command, args string) {
	c.writer <- composeCTCP(dst, command, args, false)
}

// Send a CTCP reply to the server.
func (c *Conn) CTCPReply(dst, command, args string) {
	c.writer <- composeCTCP(dst, command, args, true)
}

// Send a JOIN to the server.
func (c *Conn) Join(channels, keys []string) {
	if len(channels) > 0 {
		c.writer <- composeJoin(channels, keys)
	}
}

// send a PART to the server.
func (c *Conn) Part(channels []string, msg string) {
	if len(channels) > 0 {
		c.writer <- composePart(channels, msg)
	}
}

// Send a QUIT to the server.
func (c *Conn) Quit(msg string) {
	c.writer <- composeQuit(msg)
}

// Send a NICK to the server.
func (c *Conn) Nick(newnick string) {
	c.writer <- composeNick(newnick)
}

// DefaultCTCPHandler processes an incoming CTCP message with some default
// behavior.  For example, it will respond to PING, TIME, and VERSION requests.
// This function is called by default if no handler is registered for CTCP. If
// one is registered for CTCP, you may call this function yourself in order to
// invoke default behavior.
func (c *Conn) DefaultCTCPHandler(line Line) {
	defaultCTCPHandler(c, line)
}

var lastNick string

func (c *Conn) badNick(oldnick string, errCode int) string {
	if oldnick == "" {
		// where's our nick?
		c.Shutdown()
		return ""
	}
	if oldnick != lastNick && strings.HasPrefix(lastNick, oldnick) {
		// must have been too long
		idx := strings.LastIndexFunc(oldnick, func(r rune) bool { return r != '_' })
		if idx == -1 {
			// our entire nick is _'s?
			c.Shutdown()
			return ""
		}
		_, size := utf8.DecodeRuneInString(oldnick[idx:])
		oldnick = oldnick[:idx] + "_" + oldnick[idx+size:]
	} else {
		oldnick += "_"
	}
	lastNick = oldnick
	return oldnick
}

func (c *Conn) logIn(realName string, password string) {
	if password != "" {
		c.Raw("PASS :" + password)
	}
	c.Nick(c.me.Nick)
	user := firstWord(c.me.User)
	if user == "" {
		user = "guest"
	}
	if realName == "" {
		realName = "guest"
	}
	c.Raw("USER " + user + " 8 * :" + realName) // 8 is +i
}

func (c *Conn) runLoop() {
	for {
		select {
		case line, ok := <-c.reader:
			if !ok {
				// read end closed
				c.Shutdown()
				return
			}
			c.processLine(line)
		case _ = <-c.writeErr:
			// write end closed
			c.Shutdown()
			return
		case f := <-c.invoker:
			f(c)
		}
	}
}

func (c *Conn) processLine(input string) {
	line := parseLine(input)
	if line.Command == "" {
		// must be a malformed line. Ignore it
		return
	}
	line.me = c.me

	// detect CTCP and modify the line accordingly
	if line.Command == "PRIVMSG" || line.Command == "NOTICE" {
		if len(line.Args) > 1 && strings.HasPrefix(line.Args[len(line.Args)-1], "\001") {
			// This is a CTCP command or reply
			text := line.Args[len(line.Args)-1][1:]
			if strings.HasSuffix(text, "\001") {
				text = text[:len(text)-1]
			}
			line.Dst = line.Args[0]
			line.Args = strings.SplitN(text, " ", 2)
			switch line.Command {
			case "PRIVMSG":
				if line.Args[0] == "ACTION" {
					line.Command = ACTION
					if len(line.Args) > 1 {
						line.Args = line.Args[1:2]
					} else {
						line.Args = []string{""}
					}
				} else {
					line.Command = CTCP
				}
			case "NOTICE":
				line.Command = CTCPREPLY
			}
		}
	}

	// CTCP gets some special handling
	c.stateRegistry.Dispatch(line.Command, c, line)
	if !c.safeConnState.registry.Dispatch(line.Command, c, line) && line.Command == CTCP {
		c.DefaultCTCPHandler(line)
	}
}
