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

		Init: func(hr irc.HandlerRegistry) {
			fmt.Println("init")
			hr.AddHandler(irc.CONNECTED, h_LoggedIn)
			hr.AddHandler(irc.DISCONNECTED, func(*irc.Conn, irc.Line) {
				fmt.Println("disconnected")
				quit <- true
			})
			hr.AddHandler("PRIVMSG", h_PRIVMSG)
			hr.AddHandler(irc.ACTION, h_ACTION)
		},
	}

	fmt.Println("Connecting")
	if _, err := irc.Connect(config); err != nil {
		fmt.Println("error:", err)
		quit <- true
	}

	<-quit
	fmt.Println("Goodbye")
}

func h_LoggedIn(conn *irc.Conn, line irc.Line) {
	fmt.Println("Please edit example.go func h_LoggedIn and pick a channel to join")
	//conn.Join([]string{"#channel"}, nil)
}

func h_PRIVMSG(conn *irc.Conn, line irc.Line) {
	fmt.Printf("[%s] %s> %s\n", line.Args[0], line.Src, line.Args[1])
	if line.Args[1] == "!quit" {
		conn.Quit("")
	}
}

func h_ACTION(conn *irc.Conn, line irc.Line) {
	fmt.Printf("[%s] %s %s\n", line.Dst, line.Src, line.Args[0])
}
