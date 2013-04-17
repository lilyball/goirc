package main

import (
	"fmt"
	"github.com/kballard/goirc/irc"
)

func main() {
	quit := make(chan bool, 1)
	config := irc.Config{
		Host: "chat.freenode.net",

		Nick:     "goirc",
		User:     "goirc",
		RealName: "goirc",

		Init: func(conn *irc.Conn) {
			fmt.Println("init")
			conn.AddHandler(irc.CONNECTED, h_LoggedIn)
			conn.AddHandler(irc.DISCONNECTED, func(*irc.Conn, irc.Line) {
				fmt.Println("disconnected")
				quit <- true
			})
			conn.AddHandler("PRIVMSG", h_PRIVMSG)
		},
		Error: func(err error) {
			fmt.Println("error:", err)
			quit <- true
		},
	}

	fmt.Println("Connecting")
	irc.Connect(config)

	<-quit
	fmt.Println("Goodbye")
}

func h_LoggedIn(conn *irc.Conn, line irc.Line) {
	fmt.Println("Please edit example.go func h_LoggedIn and pick a channel to join")
	//conn.Join([]string{"#channel"}, nil)
}

func h_PRIVMSG(conn *irc.Conn, line irc.Line) {
	fmt.Printf("[%s] %s> %s\n", line.Args[0], line.Sender, line.Args[1])
	if line.Args[1] == "!quit" {
		conn.Quit("")
	}
}
