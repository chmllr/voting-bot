package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	URL                = "https://ic-api.internetcomputer.org/api/v3/proposals?limit=100"
	NNS_POLL_INTERVALL = 5 * time.Minute
	MAX_SUMMARY_LENGTH = 2048
	TOPIC_GOVERNANCE   = "topic_governance"
	LAST_SEEN_PROPOSAL = int64(0)
)

type Proposal struct {
	Title    string `json:"title"`
	Topic    string `json:"topic"`
	Id       int64  `json:"proposal_id"`
	Summary  string `json:"summary"`
	Action   string `json:"action"`
	Proposer string `json:"proposer"`
}

func main() {
	bot, err := tgbotapi.NewBotAPI(os.Getenv("TOKEN"))
	if err != nil {
		log.Panic("Couldn't instantiate the bot API:", err)
	}

	chatId, err := strconv.ParseInt(os.Getenv("CHAT"), 10, 64)
	if err != nil {
		log.Panic("Couldn't parse the chat id:", err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	go fetchProposalsAndNotify(bot, chatId)

	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		id := update.Message.Chat.ID
		if update.Message == nil || id != chatId {
			continue
		}
		if update.Message.Text == "/YES" {
			fmt.Println("voted yes")
		} else {
			fmt.Println("voted no")
		}
		msg := "Voted!"
		bot.Send(tgbotapi.NewMessage(id, msg))
	}
}

func fetchProposalsAndNotify(bot *tgbotapi.BotAPI, id int64) {
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
			if proposal.Id == LAST_SEEN_PROPOSAL { // || proposal.Topic != TOPIC_GOVERNANCE {
				continue
			}
			LAST_SEEN_PROPOSAL = proposal.Id
			log.Println("New governance proposal detected:", proposal)
			summary := proposal.Summary
			if len(summary)+2 > MAX_SUMMARY_LENGTH {
				summary = "[Proposal summary is too long.]"
			}
			if len(summary) > 0 {
				summary = "\n" + summary + "\n"
			}
			text := fmt.Sprintf("<b>%s</b>\n\nProposer: %s\n%s\n#%s\n\nhttps://dashboard.internetcomputer.org/proposal/%d",
				proposal.Title, proposal.Proposer, summary, proposal.Topic, proposal.Id)
			text = text + "\n\n/YES || /NO"

			msg := tgbotapi.NewMessage(id, text)
			msg.ParseMode = tgbotapi.ModeHTML
			msg.DisableWebPagePreview = true
			_, err := bot.Send(msg)
			if err != nil {
				log.Println("Couldn't send message:", err)
			}
		}
	}
}
