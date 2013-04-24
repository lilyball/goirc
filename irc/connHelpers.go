package irc

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// filters out any chars that can't be sent to IRC.
// This includes NUL, CR, and LF.
// This also truncates the message to 510 bytes total.
// Note: this function assumes utf8 text, and will trim to lower than 510 if it
// thinks it broke a utf8 rune in half.
func filterMessage(text string) string {
	// byte-wise iteration
	var bytes []byte
	var i, j int
	for i, j = 0, 0; i < len(text); i++ {
		c := text[i]
		if c == 0 || c == '\r' || c == '\n' {
			if bytes == nil {
				bytes = make([]byte, 0, len(text))
			}
			n := copy(bytes[len(bytes):cap(bytes)], text[j:i])
			bytes = bytes[:len(bytes)+n]
			j = i + 1
		}
	}
	if bytes != nil {
		n := copy(bytes[len(bytes):cap(bytes)], text[j:])
		bytes = bytes[:len(bytes)+n]
		text = string(bytes)
	}
	if len(text) > 510 {
		text = text[:510]
		if r, _ := utf8.DecodeLastRuneInString(text); r == utf8.RuneError {
			// we must have truncated in the middle of a rune
			// Only look UTFMax bytes backwards. If we can't find a rune start, bail.
			for i := len(text) - 1; i >= len(text)-utf8.UTFMax; i-- {
				if utf8.RuneStart(text[i]) {
					// found the start of the broken rune
					text = text[:i]
					break
				}
			}
		}
	}
	return text
}

func firstWord(text string) string {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\r' || c == '\n' {
			return text[:i]
		}
	}
	return text
}

func firstLine(text string) string {
	for i := 0; i < len(text); i++ {
		if text[i] == '\r' || text[i] == '\n' {
			return text[:i]
		}
	}
	return text
}

func composePrivmsg(dst, msg string) string {
	return filterMessage(fmt.Sprintf("PRIVMSG %s :%s", firstWord(dst), firstLine(msg)))
}

func composeNotice(dst, msg string) string {
	return filterMessage(fmt.Sprintf("NOTICE %s :%s", firstWord(dst), firstLine(msg)))
}

func composeCTCP(dst, command, msg string, isReply bool) string {
	prefix := "PRIVMSG"
	if isReply {
		prefix = "NOTICE"
	}
	if msg == "" {
		return filterMessage(fmt.Sprintf("%s %s :\001%s\001", prefix, firstWord(dst), firstWord(command)))
	} else {
		return filterMessage(fmt.Sprintf("%s %s :\001%s %s\001", prefix, firstWord(dst), firstWord(command), firstLine(msg)))
	}
}

func composeQuit(msg string) string {
	if msg == "" {
		return "QUIT"
	} else {
		return filterMessage("QUIT :" + firstLine(msg))
	}
}

func composeNick(nick string) string {
	return filterMessage("NICK :" + firstLine(nick))
}

func composeJoin(channels, keys []string) string {
	newchan, newkey := make([]string, len(channels)), make([]string, len(keys))
	for i, c := range channels {
		newchan[i] = strings.SplitN(firstWord(c), ",", 2)[0]
	}
	for i, k := range keys {
		newkey[i] = strings.SplitN(firstWord(k), ",", 2)[0]
	}
	if len(newkey) > 0 {
		return filterMessage(fmt.Sprintf("JOIN %s %s", strings.Join(newchan, ","), strings.Join(newkey, ",")))
	} else {
		return filterMessage(fmt.Sprintf("JOIN %s", strings.Join(newchan, ",")))
	}
}

func composePart(channels []string, msg string) string {
	newchan := make([]string, len(channels))
	for i, c := range channels {
		newchan[i] = strings.SplitN(firstWord(c), ",", 2)[0]
	}
	if msg != "" {
		return filterMessage(fmt.Sprintf("PART %s :%s", strings.Join(newchan, ","), firstLine(msg)))
	} else {
		return filterMessage(fmt.Sprintf("PART %s", strings.Join(newchan, ",")))
	}
}
