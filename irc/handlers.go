package irc

import (
	"fmt"
	"time"
)

func (c *Conn) setupStateHandlers() {
	c.stateRegistry.AddCallback("001", h_001)
	c.stateRegistry.AddCallback("004", h_004)

	c.stateRegistry.AddCallback("PING", h_PING)

	c.stateRegistry.AddCallback("MODE", h_MODE)
	c.stateRegistry.AddCallback("NICK", h_NICK)

	c.stateRegistry.AddCallback("431", h_431)
	c.stateRegistry.AddCallback("432", h_432)
	c.stateRegistry.AddCallback("433", h_433)
	c.stateRegistry.AddCallback("436", h_436)
	c.stateRegistry.AddCallback("437", h_437)
}

func h_001(conn *Conn, line Line) {
	// we successfully logged in
	if len(line.Args) > 0 {
		conn.me.Nick = line.Args[0]
	} else {
		// where's our nick?
		conn.Shutdown()
	}
}

func h_004(conn *Conn, line Line) {
	// login sequence complete
	conn.safeConnState.registry.Dispatch(CONNECTED, conn)
}

func h_PING(conn *Conn, line Line) {
	if len(line.Args) > 0 {
		conn.Raw(fmt.Sprintf("PONG :%s", line.Args[0]))
	}
}

func h_MODE(conn *Conn, line Line) {
	if len(line.Args) > 1 {
		if parseUser(line.Args[0]).Nick == conn.me.Nick {
			// TODO: track our own mode
		}
	}
}

func h_NICK(conn *Conn, line Line) {
	if len(line.Args) > 0 {
		if line.SrcIsMe() {
			conn.me.Nick = line.Args[0]
		}
	}
}

// ERR_NONICKNAMEGIVEN
func h_431(conn *Conn, line Line) {
	h_badNick(conn, line, 431)
}

// ERR_ERRONEUSNICKNAME
func h_432(conn *Conn, line Line) {
	h_badNick(conn, line, 432)
}

// ERR_NICKNAMEINUSE
func h_433(conn *Conn, line Line) {
	h_badNick(conn, line, 433)
}

// ERR_NICKCOLLISION
func h_436(conn *Conn, line Line) {
	h_badNick(conn, line, 436)
}

// ERR_UNAVAILRESOURCE
func h_437(conn *Conn, line Line) {
	h_badNick(conn, line, 437)
}

func h_badNick(conn *Conn, line Line, errCode int) {
	var newNick string
	oldnick := ""
	if errCode != 431 && len(line.Args) > 1 {
		oldnick = line.Args[1]
	}
	if conn.nickInUse != nil {
		newNick = conn.nickInUse(oldnick, errCode)
	} else {
		newNick = conn.badNick(oldnick, errCode)
	}
	if !conn.Connected() {
		// badNick probably bailed
		return
	}
	conn.Nick(newNick)
}

func defaultCTCPHandler(conn *Conn, line Line) {
	if line.Command != CTCP {
		return
	}
	if line.Src.Nick == "" {
		// did we get a CTCP from the server?
		return
	}
	switch line.Args[0] {
	case "PING":
		var param string
		if len(line.Args) > 1 {
			param = line.Args[1]
		}
		conn.CTCPReply(line.Src.Nick, "PING", param)
	case "TIME":
		conn.CTCPReply(line.Src.Nick, "TIME", time.Now().Format(time.UnixDate))
	case "VERSION":
		conn.CTCPReply(line.Src.Nick, "VERSION", "go library kballard/goirc")
	}
}
