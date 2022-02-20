package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	URL                = "https://ic-api.internetcomputer.org/api/v3/proposals?limit=100"
	NNS_POLL_INTERVALL = 5 * time.Minute
	MAX_SUMMARY_LENGTH = 2048
	TOPIC_GOVERNANCE   = "TOPIC_GOVERNANCE"
	LAST_SEEN_PROPOSAL = int64(0)
)

type Settings struct {
	Token    string `json:"token"`
	ChatId   int64  `json:"chatId"`
	NeuronId int64  `json:"neuronId"`
	PemFile  string `json:"pemFile"`
}

type Proposal struct {
	Title    string `json:"title"`
	Topic    string `json:"topic"`
	Id       int64  `json:"proposal_id"`
	Summary  string `json:"summary"`
	Action   string `json:"action"`
	Proposer string `json:"proposer"`
}

func main() {
	proposalIdStr, err := ioutil.ReadFile("proposal_id.txt")
	if err == nil {
		id, err := strconv.ParseInt(string(proposalIdStr), 10, 64)
		if err == nil {
			LAST_SEEN_PROPOSAL = id
		}
	}
	log.Println("Last seen proposal is", LAST_SEEN_PROPOSAL)
	data, err := os.ReadFile("settings.json")
	if err != nil {
		log.Println("Couldn't read settings file:", err)
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		log.Println("Couldn't deserialize the settings file:", err)
	}

	bot, err := tgbotapi.NewBotAPI(s.Token)
	if err != nil {
		log.Panic("Couldn't instantiate the bot API:", err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	go fetchProposalsAndNotify(bot, s.ChatId)

	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		id := update.Message.Chat.ID
		log.Printf("Got message: id=%d, message=%s", id, update.Message.Text)
		if update.Message == nil || id != s.ChatId {
			continue
		}
		vote := "0"
		if update.Message.Text == "/ADOPT" {
			vote = "1"
		} else if update.Message.Text == "/REJECT" {
		} else {
			bot.Send(tgbotapi.NewMessage(id, "I'm up and running! 🚀"))
			continue
		}
		cmd := exec.Command("sh", "./send.sh", s.PemFile, strconv.FormatInt(s.NeuronId, 10), strconv.FormatInt(LAST_SEEN_PROPOSAL, 10), vote)
		var out bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		log.Println("Sending command...")
		err := cmd.Run()
		log.Println("Done.")
		if err != nil {
			log.Println(fmt.Sprint(err) + ": " + stderr.String())
		}
		parts := strings.Split(out.String(), "The request is being processed...")
		bot.Send(tgbotapi.NewMessage(id, parts[len(parts)-1]))
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
			if proposal.Id < LAST_SEEN_PROPOSAL || proposal.Topic != TOPIC_GOVERNANCE {
				continue
			}
			LAST_SEEN_PROPOSAL = proposal.Id
			ioutil.WriteFile("proposal_id.txt", []byte(strconv.FormatInt(LAST_SEEN_PROPOSAL, 10)), 0644)
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
			text = text + "\n\n/REJECT  ↔️  /ADOPT"

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
