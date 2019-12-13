package watchman

import (
	"../db"
	"../telegram"
	"encoding/json"
	"github.com/parnurzeal/gorequest"
	"log"
	"strings"
	"time"
)

const (
	transfer_s   = "transfer"
	linkauth_s   = "linkauth"
	unlinkauth_s = "unlinkauth"
	updateauth_s = "updateauth"
	deleteauth_s = "deleteauth"
)

var actions_to_watch = map[string][]string{
	telegram.NotifyTransfers: []string{
		transfer_s,
	},
	telegram.NotifyChanges: []string{
		linkauth_s,
		unlinkauth_s,
		updateauth_s,
		deleteauth_s,
	},
}

type actions struct {
	QueryTime int      `json:"query_time, Number"`
	Actions   []action `json:"actions"`
	Total     total    `json:"total"`
	Code      int      `json:"statusCode"`
}

type total struct {
	Value int `json:"value"`
}

type action struct {
	Act       act         `json:"act"`
	Timestamp string      `json:"@timestamp"`
	BlockNum  json.Number `json:"block_num, Number"`
	TrxID     string      `json:"trx_id"`
}

type act struct {
	Authorizations []authorization        `json:"authorization"`
	Account        string                 `json:"account"`
	Name           string                 `json:"name"`
	Data           map[string]interface{} `json:"data"`
}

type transfer struct {
	From     string  `json:"from"`
	To       string  `json:"to"`
	Amount   float64 `json:"amount"`
	Symbol   string  `json:"symbol"`
	Quantity string  `json:"quantity"`
}

type linkauth struct {
	Account     string `json:"data"`
	Code        string `json:"data"`
	Type        string `json:"data"`
	Requirement string `json:"data"`
}

type updateauth struct {
	Permission string `json:"quantity"`
	Parent     string `json:"quantity"`
	Account    string `json:"quantity"`
}

type authorization struct {
	Actor      string `json:"actor"`
	Permission string `json:"permission"`
}

func Process() {
	var users []db.User
	var err error

	users, err = db.GetActiveUsers(telegram.NotifyStop)

	if err != nil {
		log.Fatal(err)
	}

	getActions(users)
}

func getActions(users []db.User) {
	var body string
	var err error

	today := time.Now()
	epoch := time.Second * -30
	epoch_ago := today.Add(epoch)

	// convert timestamp to ISO8601 for Hyperion
	after := epoch_ago.Format("2006-01-02T15:04:05")
	action_names := strings.Join(flatten(actions_to_watch), ",")

	url := "https://rem.eon.llc/v2/history/get_actions?act.name=" + action_names + "&limit=15000&sort=asc&after=" + after

	request := gorequest.New()
	_, body, errs := request.Get(url).End()

	if errs != nil {
		log.Fatal(errs)
	}

	a := actions{}

	err = json.Unmarshal([]byte(body), &a)
	if err != nil {
		log.Print(err)
	}

	if a.Code == 0 && a.Total.Value > 0 {

		for _, user := range users {
			for _, account := range user.Accounts {
				for _, action := range a.Actions {
					// RFC3339 with miliseconds
					lc, err := time.Parse("2006-01-02T15:04:05.9Z07:00", user.LastCheck)
					if err != nil {
						log.Fatal(err)
					}

					ts, err := time.Parse("2006-01-02T15:04:05.9", action.Timestamp)
					if err != nil {
						log.Fatal(err)
					}

					within_preference := matchesPreference(user.Settings.Notification, action.Act.Name)
					after_last_check := ts.After(lc)
					account_match := actorIsInAuth(action.Act.Authorizations, account)

					if account_match && after_last_check && within_preference {

						message := ts.Format("Mon Jan _2 15:04 2006 UTC")
						message += `\n` + "Account *" + account + "* has a new *" + action.Act.Name + "* transaction:"
						message += `\n\n` + parseData(action.Act.Data, action.Act.Name)
						message += `\n\n` + "[View on Remme Explorer](https://testchain.remme.io/transaction/" + action.TrxID + ")"

						user.LastCheck = ts.Format("2006-01-02T15:04:05.9Z07:00")

						db.UpdateLastCheck(user.TelegramID, user.LastCheck)
						telegram.SendMessage(user, message)

					}
				}
			}
		}
	}
}

func actorIsInAuth(authorizations []authorization, actor string) bool {
	for _, authorization := range authorizations {
		if authorization.Actor == actor {
			return true
		}
	}
	return false
}

func matchesPreference(preference string, action_name string) bool {
	if preference == telegram.NotifyAll {
		return true
	} else if preference == telegram.NotifyTransfers && stringInSlice(action_name, actions_to_watch[telegram.NotifyTransfers]) {
		return true
	} else if preference == telegram.NotifyChanges && stringInSlice(action_name, actions_to_watch[telegram.NotifyChanges]) {
		return true
	}

	return false
}

func parseData(data map[string]interface{}, action_name string) string {
	var output string
	var err error

	if stringInSlice(action_name, actions_to_watch[telegram.NotifyTransfers]) {

		jsonString, _ := json.Marshal(data)

		t := transfer{}

		err = json.Unmarshal(jsonString, &t)
		if err != nil {
			log.Print(err)
		}

		output = "From: *" + t.From + "*"
		output += `\n` + "To: *" + t.To + "*"
		output += `\n` + "Quantity: *" + t.Quantity + "*"

	} else if stringInSlice(action_name, actions_to_watch[telegram.NotifyChanges]) {
		if action_name == linkauth_s || action_name == unlinkauth_s {

			jsonString, _ := json.Marshal(data)

			l := linkauth{}

			err = json.Unmarshal(jsonString, &l)
			if err != nil {
				log.Print(err)
			}

			output = "Account: *" + l.Account + "*"
			output += `\n` + "Code: *" + l.Code + "*"
			output += `\n` + "Type: *" + l.Type + "*"
			output += `\n` + "Requirement: *" + l.Requirement + "*"

		} else if action_name == updateauth_s || action_name == deleteauth_s {

			jsonString, _ := json.Marshal(data)

			u := updateauth{}

			err = json.Unmarshal(jsonString, &u)
			if err != nil {
				log.Print(err)
			}

			output = "Permission: *" + u.Permission + "*"
			output += `\n` + "Parent: *" + u.Parent + "*"

		}
	}

	return output
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func intInSlice(a int, list []int) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func flatten(list map[string][]string) []string {
	output := []string{}
	for _, l := range list {
		output = append(output, l...)
	}
	return output
}
