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
	URL                = "https://cb3bp-ciaaa-aaaai-qkw4q-cai.raw.ic0.app"
	NNS_POLL_INTERVALL = 5 * time.Minute
	MAX_SUMMARY_LENGTH = 2048
	TOPIC_GOVERNANCE   = "Governance"
	LAST_SEEN_PROPOSAL = uint64(0)
	VOTE_YES           = "1"
	VOTE_NO            = "2"
)

type Settings struct {
	Token    string `json:"token"`
	ChatId   int64  `json:"chatId"`
	NeuronId uint64 `json:"neuronId"`
	PemFile  string `json:"pemFile"`
}

type Proposal struct {
	Title    string `json:"title"`
	Topic    string `json:"topic"`
	Id       uint64 `json:"id"`
	Summary  string `json:"summary"`
	Proposer uint64 `json:"proposer"`
	Spam     bool   `json:"spam"`
}

func main() {
	proposalIdStr, err := ioutil.ReadFile("proposal_id.txt")
	if err == nil {
		id, err := strconv.ParseUint(strings.TrimSpace(string(proposalIdStr)), 10, 64)
		if err == nil {
			LAST_SEEN_PROPOSAL = id
		} else {
			log.Fatalln(err)
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

	go fetchProposalsAndNotify(bot, &s)

	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		chatId := update.Message.Chat.ID
		log.Printf("Got message: id=%d, message=%s", chatId, update.Message.Text)
		if update.Message == nil || chatId != s.ChatId {
			continue
		}
		parts := strings.Split(update.Message.Text, "_")
		proposalId := LAST_SEEN_PROPOSAL
		var err error
		if len(parts) == 2 {
			proposalId, err = strconv.ParseUint(parts[1], 10, 64)
			if err != nil {
				log.Println("Couldn't parse the proposal id")
				continue
			}
		}
		vote := VOTE_NO
		switch parts[0] {
		case "/ADOPT":
			vote = VOTE_YES
		case "/REJECT":
		default:
			bot.Send(tgbotapi.NewMessage(chatId, "I'm up and running! 🚀"))
			continue
		}
		sendVote(bot, &s, proposalId, vote)
	}
}

func sendVote(bot *tgbotapi.BotAPI, s *Settings, proposalId uint64, vote string) {
	cmd := exec.Command("sh", "./send.sh", s.PemFile, strconv.FormatUint(s.NeuronId, 10), strconv.FormatUint(proposalId, 10), vote)
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
	bot.Send(tgbotapi.NewMessage(s.ChatId, fmt.Sprintf("VOTE=%s: %s", vote, parts[len(parts)-1])))
}

func fetchProposalsAndNotify(bot *tgbotapi.BotAPI, s *Settings) {
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
		var proposals []Proposal
		if err := json.Unmarshal(body, &proposals); err != nil {
			fmt.Println("Couldn't parse the response as JSON:", err, body)
			continue
		}

		sort.Slice(proposals, func(i, j int) bool { return proposals[i].Id < proposals[j].Id })

		for _, proposal := range proposals {
			if proposal.Id <= LAST_SEEN_PROPOSAL || proposal.Topic != TOPIC_GOVERNANCE {
				continue
			}
			LAST_SEEN_PROPOSAL = proposal.Id
			ioutil.WriteFile("proposal_id.txt", []byte(strconv.FormatUint(LAST_SEEN_PROPOSAL, 10)), 0644)
			log.Println("New governance proposal detected:", proposal)
			summary := proposal.Summary
			if len(summary)+2 > MAX_SUMMARY_LENGTH {
				summary = "[Proposal summary is too long.]"
			}
			if len(summary) > 0 {
				summary = "\n" + summary + "\n"
			}
			var text string
			if proposal.Spam {
				text = fmt.Sprintf("SPAM PROPOSAL DETECTED\n\nhttps://dashboard.internetcomputer.org/proposal/%d", proposal.Id)
			} else {
				text = fmt.Sprintf("<b>%s</b>\n\nProposer: %d\n%s\n#%s\n\nhttps://dashboard.internetcomputer.org/proposal/%d\n\n/REJECT_%d  ↔️  /ADOPT_%d",
					proposal.Title, proposal.Proposer, summary, proposal.Topic, proposal.Id, proposal.Id, proposal.Id)
			}
			msg := tgbotapi.NewMessage(s.ChatId, text)
			msg.ParseMode = tgbotapi.ModeHTML
			msg.DisableWebPagePreview = true
			_, err := bot.Send(msg)
			if err != nil {
				log.Println("Couldn't send message:", err)
			}
			if proposal.Spam {
				sendVote(bot, s, proposal.Id, VOTE_NO)
			}
		}
	}
}
