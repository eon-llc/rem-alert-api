package watchman

import (
	"../db"
	"../telegram"
	"encoding/json"
	"github.com/parnurzeal/gorequest"
	"log"
	"strconv"
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
	Act            act         `json:"act"`
	Timestamp      string      `json:"@timestamp"`
	BlockNum       json.Number `json:"block_num, Number"`
	TrxID          string      `json:"trx_id"`
	GlobalSequence json.Number `json:"global_sequence, Number"`
}

type act struct {
	Authorizations []authorization        `json:"authorization"`
	Account        string                 `json:"account"`
	Name           string                 `json:"name"`
	Data           map[string]interface{} `json:"data"`
	Scheduled      bool                   `json:"scheduled"`
}

type transfer struct {
	From     string  `json:"from"`
	To       string  `json:"to"`
	Amount   float64 `json:"amount"`
	Symbol   string  `json:"symbol"`
	Quantity string  `json:"quantity"`
}

type linkauth struct {
	Account     string `json:"account"`
	Code        string `json:"code"`
	Type        string `json:"type"`
	Requirement string `json:"requirement"`
}

type updateauth struct {
	Permission string `json:"permission"`
	Parent     string `json:"parent"`
	Account    string `json:"account"`
}

type authorization struct {
	Actor      string `json:"actor"`
	Permission string `json:"permission"`
}

type transactions struct {
	Transactions []transaction `json:"transactions"`
}

type transaction struct {
	DelayUntil string  `json:"delay_until"`
	Expiration string  `json:"expiration"`
	Published  string  `json:"published"`
	TxData     tx_data `json:"transaction"`
}

type tx_data struct {
	DelaySec int   `json:"delay_sec"`
	Acts     []act `json:"actions"`
}

type producers struct {
	Producers []producer `json:"rows"`
}

type producer struct {
	Owner                            string `json:"owner"`
	TotalVotes                       string `json:"total_votes"`
	ProducerKey                      string `json:"producer_key"`
	IsActive                         int    `json:"is_active"`
	Url                              string `json:"url"`
	CurrentRoundUnpaidBlocks         int    `json:"current_round_unpaid_blocks"`
	UnpaidBlocks                     int    `json:"unpaid_blocks"`
	ExpectedProducedBlocks           int    `json:"expected_produced_blocks"`
	LastExpectedProducedBlocksUpdate string `json:"last_expected_produced_blocks_update"`
	PendingPervoteReward             int    `json:"pending_pervote_reward"`
	LastClaimTime                    string `json:"last_claim_time"`
	LastBlockTime                    string `json:"last_block_time"`
	Top21ChosenTime                  string `json:"top21_chosen_time"`
	PunishedUntil                    string `json:"punished_until"`
}

func Watch() {
	var users []db.User
	var err error

	users, err = db.GetActiveUsers(telegram.NotifyStop, telegram.AlertStop)

	if err != nil {
		log.Print(err)
	}

	//sendNotifications(users)
	sendAlerts(users)
}

func sendNotifications(users []db.User) {
	today := time.Now()
	epoch := time.Second * -30
	epoch_ago := today.Add(epoch)

	a := getActions(epoch_ago)
	scheduled_txs := getScheduledTxs(epoch_ago)

	for _, tx := range scheduled_txs.Transactions {
		for _, act_data := range tx.TxData.Acts {
			new_action := action{}
			new_action.Timestamp = tx.Published
			new_action.Act = act_data
			new_action.Act.Scheduled = true

			a.Actions = append(a.Actions, new_action)
		}
	}

	if len(a.Actions) > 0 {

		// avoid duplicate notifications
		// by saving signatures of [user_id + trx_id]
		notifications := []string{}
		var lc time.Time
		var ts time.Time
		var err error

		for _, user := range users {
			for _, account := range user.Accounts {
				for _, action := range a.Actions {

					notification := user.TelegramID + string(action.GlobalSequence)

					// RFC3339 with miliseconds
					lc, err = time.Parse("2006-01-02T15:04:05.9Z07:00", user.LastCheck)
					if err != nil {
						log.Print(err)
					}

					ts, err = time.Parse("2006-01-02T15:04:05.9", action.Timestamp)
					if err != nil {
						log.Print(err)
					}

					within_preference := matchesPreference(user.Settings.Notification.Setting, action.Act.Name)
					is_new_tx := (ts.After(lc) && !stringInSlice(notification, notifications))
					account_match := actorIsInAuth(action.Act.Authorizations, account)

					if account_match && is_new_tx && within_preference {
						var action_name string

						if action.Act.Scheduled {
							action_name = "scheduled " + action.Act.Name
						} else {
							action_name = action.Act.Name
						}

						message := time.Now().Format("Mon Jan _2 15:04 2006 UTC")
						message += `\n` + "Account *" + account + "* has a new *" + action_name + "* transaction:"
						message += `\n\n` + parseData(action.Act.Data, action.Act.Name)
						message += `\n\n` + "[View on Remme Explorer](https://remchain.remme.io/transaction/" + action.TrxID + ")"

						notifications = append(notifications, notification)
						telegram.SendMessage(user, message)

					}
				}
			}

			user.LastCheck = ts.Format("2006-01-02T15:04:05.9Z07:00")
			db.UpdateLastCheck(user.TelegramID, user.LastCheck)
		}
	}
}

func sendAlerts(users []db.User) {
	var last_block_time time.Time
	var last_alert time.Time
	var snooze time.Time
	var err error

	p := getProducers()

	for _, user := range users {

		if user.Settings.Alert.Setting != "Stop all system alerts" {

			filtered := producers{}

			last_alert, err = time.Parse("2006-01-02T15:04:05.9Z07:00", user.LastAlert)
			if err != nil {
				log.Print(err)
			}

			snooze, err = time.Parse("2006-01-02T15:04:05.9", user.Settings.Alert.Snooze)
			if err != nil {
				log.Print(err)
			}

			if user.Settings.Alert.Setting == "Alert only when my producer fails" {
				for _, producer := range p.Producers {
					if stringInSlice(producer.Owner, user.Accounts) {
						filtered.Producers = append(filtered.Producers, producer)
					}
				}
			} else if user.Settings.Alert.Setting == "Alert when any producer fails" {
				filtered = p
			}

			// do not alert more often than every 5 minutes
			five_mins_since_last_alert := time.Now().After(last_alert.Add(time.Minute * 5))
			not_snoozing := time.Now().After(snooze)

			if five_mins_since_last_alert && not_snoozing {

				bad := producers{}

				for _, producer := range filtered.Producers {

					last_block_time, err = time.Parse("2006-01-02T15:04:05.9", producer.LastBlockTime)
					if err != nil {
						log.Print(err)
					}

					// one cycle is 126 seconds (21 producers for 6 seconds each)
					// allow two cycles missed in a row before sending an alert
					producer_missed_blocks := time.Since(last_block_time).Seconds() > 252

					if producer_missed_blocks {
						log.Print(time.Since(last_block_time).Seconds(), " - ", producer.Owner, user.TelegramID)
						bad.Producers = append(bad.Producers, producer)
					}
				}

				if len(bad.Producers) > 0 {
					message := time.Now().Format("Mon Jan _2 15:04 2006 UTC")
					message += `\n` + "The following block producers are currently missing blocks:"

					for _, bp := range bad.Producers {
						seconds_since_last := int(time.Since(last_block_time).Seconds())
						missed_blocks := strconv.Itoa(int(seconds_since_last / 120))
						message += "*" + bp.Owner + "* produced " + strconv.Itoa(seconds_since_last) + " seconds ago (missed " + missed_blocks + " blocks)."
					}

					telegram.SendMessage(user, message)
				}

				user.LastAlert = time.Now().Format("2006-01-02T15:04:05.9Z07:00")
				db.UpdateLastAlert(user.TelegramID, user.LastAlert)
			}
		}
	}
}

func getActions(epoch_ago time.Time) actions {
	var body string
	var err error
	// convert timestamp to ISO8601 for Hyperion
	after := epoch_ago.Format("2006-01-02T15:04:05")
	action_names := strings.Join(flatten(actions_to_watch), ",")

	url := "https://rem.eon.llc/v2/history/get_actions?act.name=" + action_names + "&limit=15000&sort=asc&after=" + after

	request := gorequest.New()
	_, body, errs := request.Get(url).End()

	if errs != nil {
		log.Print(errs)
	}

	a := actions{}

	err = json.Unmarshal([]byte(body), &a)
	if err != nil {
		log.Print(err)
	}

	return a
}

func getProducers() producers {
	var body string
	var err error
	// convert timestamp to ISO8601 for Hyperion
	url := "http://rem.eon.llc/v1/chain/get_table_rows"
	data := `{"table":"producers","scope":"rem","code":"rem", "limit": 1000, "json":true}`

	request := gorequest.New()
	_, body, errs := request.Post(url).Send(data).End()

	if errs != nil {
		log.Print(errs)
	}

	all := producers{}
	relevant := producers{}

	err = json.Unmarshal([]byte(body), &all)
	if err != nil {
		log.Print(err)
	}

	// those who have never produced a block
	// are of no concern, remove them
	for _, producer := range all.Producers {
		if producer.IsActive == 1 && producer.LastBlockTime != "1970-01-01T00:00:00.000" && producer.Top21ChosenTime != "1970-01-01T00:00:00.000" {
			relevant.Producers = append(relevant.Producers, producer)
		}
	}

	return relevant
}

func getScheduledTxs(epoch_ago time.Time) transactions {
	var body string
	var err error
	// convert timestamp to ISO8601 for Hyperion
	after := epoch_ago.Format("2006-01-02T15:04:05")
	url := "https://rem.eon.llc/v1/chain/get_scheduled_transactions"
	data := `{"json":"true", "limit": 1000, "lower_bound": "` + after + `"}`

	request := gorequest.New()
	_, body, errs := request.Post(url).Send(data).End()

	if errs != nil {
		log.Print(errs)
	}

	t := transactions{}

	err = json.Unmarshal([]byte(body), &t)
	if err != nil {
		log.Print(err)
	}

	return t
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
