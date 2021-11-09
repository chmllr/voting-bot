package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var URL = "https://ic-api.internetcomputer.org/api/v3/proposals?limit=1"
var statePath = "state.json"
var checkInterval = 5 * time.Minute

type Proposal struct {
	Title   string `json:"title"`
	Topic   string `json:"topic"`
	Id      int64  `json:"proposal_id"`
	Summary string `json:"summary"`
	Action  string `json:"action"`
}

type State struct {
	LastSeenProposal int64                     `json:"last_seen_proposal"`
	ChatIds          map[int64]map[string]bool `json:"chat_ids"`
	lock             sync.RWMutex
}

func (s *State) persist() {
	data, err := json.Marshal(s)
	if err != nil {
		log.Println("Couldn't serialize state:", err)
		return
	}
	err = os.WriteFile(statePath, data, 0644)
	if err != nil {
		log.Println("Couldn't write to state file", statePath, " :", err)
	}
	log.Println(len(data), "bytes persisted to", statePath)
}

func (s *State) restore() {
	data, err := os.ReadFile(statePath)
	if err != nil {
		log.Println("Couldn't read file", statePath)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		log.Println("Couldn't deserialize the state file", statePath, ":", err)
	}
	if s.ChatIds == nil {
		s.ChatIds = map[int64]map[string]bool{}
	}
	fmt.Println("Deserialized the state with", len(s.ChatIds), "users, last proposal id:", s.LastSeenProposal)
}

func main() {
	bot, err := tgbotapi.NewBotAPI(os.Getenv("TOKEN"))
	if err != nil {
		log.Panic("Couldn't instantiate the bot API:", err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	var state State
	state.restore()

	go fetchProposalsAndNotify(bot, &state)

	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		if update.Message == nil {
			continue
		}
		var msg tgbotapi.MessageConfig
		id := update.Message.Chat.ID
		userMsg := strings.ToLower(update.Message.Text)
		subscription := userMsg == "/start"
		block := strings.Contains(userMsg, "/block")
		if subscription || userMsg == "/stop" {
			var text string
			state.lock.Lock()
			if subscription {
				state.ChatIds[id] = map[string]bool{}
				log.Println("Added user", id, "to subscribers")
				text = "Subscribed." + "\n\n" + getHelpMessage()
			} else {
				delete(state.ChatIds, id)
				log.Println("Removed user", id, "from subscribers")
				text = "Unsubscribed."
			}
			state.persist()
			state.lock.Unlock()
			msg = tgbotapi.NewMessage(id, text)
		} else if block || strings.Contains(userMsg, "/unblock") {
			words := strings.Split(userMsg, " ")
			var text string
			if len(words) != 2 {
				text = fmt.Sprintf("Please specify the topic")
			} else {
				state.lock.Lock()
				topic := strings.Replace(words[1], "#", "", -1)
				if block {
					blacklist := state.ChatIds[id]
					if blacklist != nil {
						blacklist[topic] = true
					}
				} else {
					delete(state.ChatIds[id], topic)
				}
				text = blockedTopicsMsg(state.ChatIds[id])
				state.persist()
				state.lock.Unlock()
			}
			msg = tgbotapi.NewMessage(id, text)
		} else if userMsg == "/blacklist" {
			state.lock.RLock()
			text := blockedTopicsMsg(state.ChatIds[id])
			state.lock.RUnlock()
			msg = tgbotapi.NewMessage(id, text)
		} else {
			msg = tgbotapi.NewMessage(id, getHelpMessage())
		}
		bot.Send(msg)
	}
}

func getHelpMessage() string {
	return "Enter /stop to stop the notifications. " +
		"Use /block or /unblock to block or unblock proposals with a certain a topic; " +
		"use /blacklist to display the list of blocked topics."
}

func fetchProposalsAndNotify(bot *tgbotapi.BotAPI, state *State) {
	ticker := time.NewTicker(checkInterval)
	for range ticker.C {
		resp, err := http.Get(URL)
		if err != nil {
			log.Println("GET request failed from", URL, ":", err)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println("Couldn't read the response body:", err)
		}
		var jsonResp struct {
			Data []Proposal `json:"data"`
		}
		if err := json.Unmarshal(body, &jsonResp); err != nil {
			fmt.Println("Couldn't parse the response as JSON:", err)
		} else {
			proposal := jsonResp.Data[0]
			var lastSeenProposal int64
			state.lock.RLock()
			lastSeenProposal = state.LastSeenProposal
			state.lock.RUnlock()

			if lastSeenProposal == proposal.Id {
				continue
			}
			log.Println("New proposal detected:", proposal)
			summary := proposal.Summary
			if len(summary) > 0 {
				summary = "\n" + summary + "\n"
			}
			text := fmt.Sprintf("*%s*\n%s\n#%s\n\nhttps://dashboard.internetcomputer.org/proposal/%d",
				proposal.Title, summary, tgbotapi.EscapeText(tgbotapi.ModeMarkdown, proposal.Topic), proposal.Id)

			state.lock.Lock()
			state.LastSeenProposal = proposal.Id
			state.persist()
			state.lock.Unlock()

			state.lock.RLock()
		USERS:
			for id, blacklist := range state.ChatIds {
				if blacklist[strings.ToLower(proposal.Topic)] {
					continue USERS
				}
				msg := tgbotapi.NewMessage(id, text)
				msg.ParseMode = tgbotapi.ModeMarkdown
				msg.DisableWebPagePreview = true
				bot.Send(msg)
			}
			log.Println("Successfully notified", len(state.ChatIds), "users")
			state.lock.RUnlock()
		}
	}
}

func blockedTopicsMsg(m map[string]bool) string {
	if len(m) == 0 {
		return "You did not block any topics."
	}
	var res []string
	for topic, enabled := range m {
		if enabled {
			res = append(res, topic)
		}
	}
	return fmt.Sprintf("You've blocked these topics: %s", strings.Join(res, ", "))
}
