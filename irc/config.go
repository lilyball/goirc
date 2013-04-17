package irc

import (
	"bufio"
	"github.com/kballard/gocallback/callback"
	"io"
	"net"
	"strconv"
	"time"
)

// Config represents the configuration used to set up a server connection.
// After being passed to Connect(), the Config object can be thrown away.
type Config struct {
	Host     string
	Port     uint // if 0, 6667 is used, or 6697 if SSL
	SSL      bool // set to true to use SSL
	Password string

	Nick     string
	User     string
	RealName string

	Timeout time.Duration // timeout for the Connect. 0 means no timeout.

	AllowFlood   bool          // set to true to disable flood protection
	PingInterval time.Duration // defaults to 3 minutes, set to -1 to disable

	// Init is called before the connection is established, in order to allow
	// you to set up other handlers.
	// Required.
	// This is called on the connection's goroutine.
	// It is an error to try to invoke any methods at this time besides the
	// callback management ones.
	Init func(conn *Conn)
	// Error is called if the connection could not be established.
	// Optional.
	// This is called on the connection's goroutine (before the goroutine
	// terminates).
	Error func(err error)
	// NickInUse is called when the chosen nickname is already in use.
	// Optional.
	// It's also given the 3-digit error code provided by the server,
	// e.g. 432 indicates invalid characters in the nick, and 433 indicates
	// the nickname was already in use.
	// It must return a new nickname.
	// If nil, the default behavior of appending a _ is uesd.
	NickInUse func(oldnick string, errcode int) string
}

// Connect initiates a connection to an IRC server identified by the Config.
// It returns to the caller immediately; the Init function must be used to set
// up any callbacks.
func Connect(config Config) SafeConn {
	if config.Init == nil {
		panic("Config needs an Init function")
	}

	port := config.Port
	if port == 0 {
		if config.SSL {
			port = 6697
		} else {
			port = 6667
		}
	}
	addr := net.JoinHostPort(config.Host, strconv.FormatUint(uint64(port), 10))
	writer, reader := make(chan string), make(chan string)
	writeErr, readErr := make(chan error, 1), make(chan error, 1)
	invoker := make(chan func(*Conn))
	conn := &Conn{
		me: User{
			Nick: config.Nick,
			User: config.User,
		},
		stateRegistry: callback.NewRegistry(callback.DispatchSerial),
		nickInUse:     config.NickInUse,
		writer:        writer,
		reader:        reader,
		writeErr:      writeErr,
		readErr:       readErr,
		invoker:       invoker,
		safeConnState: &safeConnState{
			server:   addr,
			registry: callback.NewRegistry(callback.DispatchSerial),
		},
	}
	safeConn := conn.SafeConn()
	config_ := config // copy the config for the goroutine
	go func() {
		config_.Init(conn)
		var nc net.Conn
		if config_.Timeout != 0 {
			var err error
			if nc, err = net.DialTimeout("tcp", addr, config_.Timeout); err != nil {
				if config_.Error != nil {
					config_.Error(err)
				}
				return
			}
		} else {
			var err error
			if nc, err = net.Dial("tcp", addr); err != nil {
				if config_.Error != nil {
					config_.Error(err)
				}
				return
			}
		}
		conn.netconn = nc
		// set up the writer and reader before we call any callbacks
		go connWriter(nc, writer, writeErr, config_.AllowFlood)
		go connReader(nc, reader, readErr)
		// also set up the invoker infinite queue
		queue := make(chan func(*Conn))
		go invokerQueue(invoker, queue)
		// set up the safeConnState
		conn.safeConnState.Lock()
		conn.safeConnState.writer = conn.writer
		conn.safeConnState.invoker = queue
		conn.safeConnState.Unlock()
		// set up the pinger
		if config_.PingInterval >= 0 {
			delta := config_.PingInterval
			if delta == 0 {
				delta = 3 * time.Minute
			}
			go pinger(safeConn, delta)
		}
		// dispatch the Connected callback
		conn.safeConnState.registry.Dispatch(Connected, conn)
		// set up our state handlers
		conn.setupStateHandlers()
		// fire off the login lines
		conn.logIn(config_.RealName, config_.Password)
		// and start the main loop
		conn.runLoop()
	}()
	return safeConn
}

func connWriter(nc net.Conn, c <-chan string, writeErr chan<- error, allowFlood bool) {
	// set up the infinite queue
	queue := make(chan string)
	go func() {
		var buf []string
	loop:
		for {
			if len(buf) > 0 {
				select {
				case input, ok := <-c:
					if !ok {
						break loop
					}
					buf = append(buf, input)
				case queue <- buf[0]:
					buf = buf[1:]
				}
			} else {
				if input, ok := <-c; ok {
					select {
					case queue <- input:
					default:
						buf = append(buf, input)
					}
				} else {
					break loop
				}
			}
		}
		for _, elt := range buf {
			queue <- elt
		}
		close(queue)
	}()
	// read from the queue and write to the wire
	// implement flood protection unless allowFlood is true.
	// Use the flood protection algorithm from Hybrid IRCd.
	// This is the normal 2-second penalty, plus 1/120th of a second per character.
	const maxTimeDelta = 10 * time.Second
	var floodTime time.Time
	for line := range queue {
		if !allowFlood {
			now := time.Now()
			if now.After(floodTime) {
				floodTime = now
			}
			penalty := 2*time.Second + (time.Second * time.Duration(len(line)) / 120)
			floodTime = floodTime.Add(penalty)
			delta := floodTime.Sub(now)
			if delta > maxTimeDelta {
				// sleep until we're good again
				<-time.After(delta - maxTimeDelta)
			}
		}
		if _, err := io.WriteString(nc, line+"\r\n"); err != nil {
			writeErr <- err
			break
		}
	}
	close(writeErr)
	// exhaust the queue so we don't leak the goroutine
	for _ = range queue {
	}
}

func connReader(nc net.Conn, c chan<- string, readErr chan<- error) {
	// set up the infinite queue
	queue := make(chan string)
	go func() {
		var buf []string
	loop:
		for {
			if len(buf) > 0 {
				select {
				case input, ok := <-queue:
					if !ok {
						break loop
					}
					buf = append(buf, input)
				case c <- buf[0]:
					buf = buf[1:]
				}
			} else {
				if input, ok := <-queue; ok {
					select {
					case c <- input:
					default:
						buf = append(buf, input)
					}
				} else {
					break loop
				}
			}
		}
		for _, elt := range buf {
			c <- elt
		}
		close(c)
	}()
	// read from the wire and write to the queue
	scanner := bufio.NewScanner(nc) // defaults to SplitLines
	for scanner.Scan() {
		queue <- scanner.Text()
	}
	if scanner.Err() != nil {
		readErr <- scanner.Err()
	} else {
		// dump EOF in there, since that's what the scanner got
		readErr <- io.EOF
	}
	close(readErr)
	close(queue)
}

func invokerQueue(output chan<- func(*Conn), input <-chan func(*Conn)) {
	var buf []func(*Conn)
loop:
	for {
		if len(buf) > 0 {
			select {
			case f, ok := <-input:
				if !ok {
					break loop
				}
				buf = append(buf, f)
			case output <- buf[0]:
				buf = buf[1:]
			}
		} else {
			f, ok := <-input
			if !ok {
				break loop
			}
			buf = append(buf, f)
		}
	}
	for _, f := range buf {
		output <- f
	}
	close(output)
}

func pinger(conn SafeConn, delta time.Duration) {
	ticker := time.NewTicker(delta)
	for {
		t := <-ticker.C
		if !conn.Raw("PING " + strconv.FormatInt(t.Unix(), 10)) {
			// connection was shut down
			ticker.Stop()
			break
		}
	}
}
