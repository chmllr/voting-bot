package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	URL                        = "https://ic-api.internetcomputer.org/api/v3/proposals?limit=100"
	STATE_PATH                 = "state.json"
	NNS_POLL_INTERVALL         = 5 * time.Minute
	STATE_PERSISTENCE_INTERVAL = 5 * time.Minute
	MAX_TOPIC_LENGTH           = 50
	MAX_BLOCKED_TOPICS         = 30
	MAX_SUMMARY_LENGTH         = 2048
)

type Proposal struct {
	Title    string `json:"title"`
	Topic    string `json:"topic"`
	Id       int64  `json:"proposal_id"`
	Summary  string `json:"summary"`
	Action   string `json:"action"`
	Proposer string `json:"proposer"`
}

type State struct {
	LastSeenProposal int64                     `json:"last_seen_proposal"`
	ChatIds          map[int64]map[string]bool `json:"chat_ids"`
	lock             sync.RWMutex
}

func (s *State) persist() {
	s.lock.RLock()
	data, err := json.Marshal(s)
	s.lock.RUnlock()
	if err != nil {
		log.Println("Couldn't serialize state:", err)
		return
	}
	tmpFile, err := ioutil.TempFile(".", STATE_PATH+"_tmp_")
	if err != nil {
		log.Fatal(err)
	}
	err = os.WriteFile(tmpFile.Name(), data, 0644)
	if err != nil {
		log.Println("Couldn't write to state file", STATE_PATH, " :", err)
	}
	os.Rename(tmpFile.Name(), STATE_PATH)
	log.Println(len(data), "bytes persisted to", STATE_PATH)
}

func (s *State) restore() {
	data, err := os.ReadFile(STATE_PATH)
	if err != nil {
		log.Println("Couldn't read file", STATE_PATH)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		log.Println("Couldn't deserialize the state file", STATE_PATH, ":", err)
	}
	if s.ChatIds == nil {
		s.ChatIds = map[int64]map[string]bool{}
	}
	fmt.Println("Deserialized the state with", len(s.ChatIds), "users, last proposal id:", s.LastSeenProposal)
}

func (s *State) setNewLastSeenId(id int64) (updated bool) {
	s.lock.Lock()
	if s.LastSeenProposal < id {
		s.LastSeenProposal = id
		updated = true
	}
	s.lock.Unlock()
	return
}

func (s *State) removeChatId(id int64) {
	s.lock.Lock()
	delete(s.ChatIds, id)
	s.lock.Unlock()
	log.Println("Removed user", id, "from subscribers")
}

func (s *State) addChatId(id int64) {
	s.lock.Lock()
	s.ChatIds[id] = map[string]bool{"topic_exchange_rate": true}
	s.lock.Unlock()
	log.Println("Added user", id, "to subscribers")
}

func (s *State) blockTopic(id int64, topic string) {
	if len(topic) > MAX_TOPIC_LENGTH {
		return
	}
	s.lock.Lock()
	blacklist := s.ChatIds[id]
	if blacklist != nil && len(blacklist) < MAX_BLOCKED_TOPICS {
		blacklist[topic] = true
	}
	s.lock.Unlock()
}

func (s *State) unblockTopic(id int64, topic string) {
	s.lock.Lock()
	blacklist := s.ChatIds[id]
	if blacklist != nil {
		delete(blacklist, topic)
	}
	s.lock.Unlock()
}

func (s *State) chatIds(topic string) (res []int64) {
	s.lock.RLock()
	for id, blacklist := range s.ChatIds {
		if blacklist != nil && !blacklist[topic] {
			res = append(res, id)
		}
	}
	s.lock.RUnlock()
	return
}

func (s *State) blockedTopics(id int64) string {
	s.lock.RLock()
	defer s.lock.RUnlock()
	m := s.ChatIds[id]
	if m == nil || len(m) == 0 {
		return "Your list of blocked topics is empty."
	}
	var res []string
	for topic, enabled := range m {
		if enabled {
			res = append(res, topic)
		}
	}
	return fmt.Sprintf("You've blocked these topics: %s.", strings.Join(res, ", "))
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
	go persist(&state)

	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		if update.Message == nil {
			continue
		}
		var msg string
		id := update.Message.Chat.ID
		words := strings.Split(strings.ToLower(update.Message.Text), " ")
		if len(words) == 0 {
			continue
		}
		cmd := words[0]
		switch cmd {
		case "/start", "/stop":
			if cmd == "/start" {
				state.addChatId(id)
				msg = "Subscribed." + "\n\n" + getHelpMessage()
			} else {
				state.removeChatId(id)
				msg = "Unsubscribed."
			}
		case "/block", "/unblock":
			if len(words) != 2 {
				msg = fmt.Sprintf("Please specify the topic")
			} else {
				topic := strings.Replace(words[1], "#", "", -1)
				if cmd == "/block" {
					state.blockTopic(id, topic)
				} else {
					state.unblockTopic(id, topic)
				}
				msg = state.blockedTopics(id)
			}
		case "/blacklist":
			msg = state.blockedTopics(id)
		default:
			msg = getHelpMessage()
		}
		bot.Send(tgbotapi.NewMessage(id, msg))
	}
}

func getHelpMessage() string {
	return "Enter /stop to stop the notifications. " +
		"Use /block or /unblock to block or unblock proposals with a certain a topic; " +
		"use /blacklist to display the list of blocked topics."
}

func persist(state *State) {
	ticker := time.NewTicker(STATE_PERSISTENCE_INTERVAL)
	for range ticker.C {
		state.persist()
	}
}

func fetchProposalsAndNotify(bot *tgbotapi.BotAPI, state *State) {
	ticker := time.NewTicker(NNS_POLL_INTERVALL)
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
			continue
		}

		proposals := jsonResp.Data
		sort.Slice(proposals, func(i, j int) bool { return proposals[i].Id < proposals[j].Id })

		for _, proposal := range jsonResp.Data {
			if !state.setNewLastSeenId(proposal.Id) {
				continue
			}
			log.Println("New proposal detected:", proposal)
			summary := proposal.Summary
			if len(summary)+2 > MAX_SUMMARY_LENGTH {
				summary = "[Proposal summary is too long.]"
			}
			if len(summary) > 0 {
				summary = "\n" + summary + "\n"
			}
			text := fmt.Sprintf("<b>%s</b>\n\nProposer: %s\n%s\n#%s\n\nhttps://dashboard.internetcomputer.org/proposal/%d",
				proposal.Title, proposal.Proposer, summary, proposal.Topic, proposal.Id)

			ids := state.chatIds(strings.ToLower(proposal.Topic))
			for _, id := range ids {
				msg := tgbotapi.NewMessage(id, text)
				msg.ParseMode = tgbotapi.ModeHTML
				msg.DisableWebPagePreview = true
				_, err := bot.Send(msg)
				if err != nil {
					log.Println("Couldn't send message:", err)
					if strings.Contains(err.Error(), "bot was blocked by the user") {
						state.removeChatId(id)
					}
				}
			}
			if len(ids) > 0 {
				log.Println("Successfully notified", len(ids), "users")
			}
		}
	}
}
