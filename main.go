package main

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"strings"

	"go.uber.org/zap"
)

type InteractionType int

const (
	_ InteractionType = iota
	Ping
	ApplicationCommand
)

type InteractionResponseType int

const (
	_ InteractionResponseType = iota
	Pong
	Acknowledge
	ChannelMessage
	ChannelMessageWithSource
	ACKWithSource
)

var (
	logger *zap.SugaredLogger
)

func handleRequest(w http.ResponseWriter, r *http.Request) {
	t := struct {
		Type InteractionType
		Data struct{ Options []struct{ Value string } }
	}{}
	err := json.NewDecoder(r.Body).Decode(&t)
	if err != nil {
		logger.Errorw("failed to decode body", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch t.Type {
	case Ping:
		err = json.NewEncoder(w).Encode(struct {
			Type InteractionResponseType `json:"type"`
		}{Pong})
		if err != nil {
			logger.Errorw("failed to write pong", "err", err)
		}
	case ApplicationCommand:
		symbol := t.Data.Options[0].Value

		out, err := exec.Command("go", "doc", symbol).Output()
		if err != nil {
			logger.Errorw("failed to run go doc command", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		err = json.NewEncoder(w).Encode(struct {
			Type InteractionResponseType `json:"type"`
			Data interface{}             `json:"data"`
		}{
			Type: ChannelMessageWithSource,
			Data: map[string]interface{}{"content": FormatMessage(symbol, string(out))},
		})
		if err != nil {
			logger.Errorw("failed to write response", "err", err)
		}
	}
}

type ParserState int

const (
	CodeBlock ParserState = iota
	Text
)

func FormatMessage(symbol, output string) (message string) {
	lines := strings.Split(output, "\n")

	state := CodeBlock
	message += "```go\n"
	for _, line := range lines {
		switch state {
		case CodeBlock:
			if strings.HasPrefix(line, "    ") {
				state = Text
				message += "```\n" + strings.Trim(line, " ") + " "
				break
			}
			message += line + "\n"
		case Text:
			if line != "" && !strings.HasPrefix(line, "    ") {
				state = CodeBlock
				message += "```go\n" + line + "\n"
				break
			}
			if line == "" {
				message += "\n\n"
			} else {
				message += strings.Trim(line, " ") + " "
			}
		}
	}

	if state == CodeBlock {
		message += "```"
	}

	message = strings.TrimRight(message, " \n") + "\n"

	if state != CodeBlock {
		message += "\n"
	}

	re := regexp.MustCompile(`// import "(.+)"`)
	matches := re.FindStringSubmatch(message)

	link := "<https://pkg.go.dev/"
	parts := strings.Split(symbol, ".")
	if len(parts) >= 1 {
		if len(matches) >= 2 {
			link += matches[1]
		} else {
			link += parts[0]
		}
	}
	if len(parts) >= 2 {
		link += "#" + strings.Title(parts[1])
	}
	if len(parts) >= 3 {
		link += "." + strings.Title(parts[2])
	}
	link += ">"

	if len(message)+len(link) > 2000 {
		return "That documentation is too long to send! See: " + link
	}
	return message + link
}

func main() {
	l, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	logger = l.Sugar()

	http.HandleFunc("/", handleRequest)
	http.ListenAndServe(":8080", nil)
}
