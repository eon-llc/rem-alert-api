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

var notification_actions_to_watch = map[string][]string{
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

var alert_actions_to_watch = []string{
	"init", "setprice",
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

type voters struct {
	Voters []voter `json:"rows"`
}

type voter struct {
	Owner               string      `json:"owner"`
	Producers           []string    `json:"producers"`
	Staked              json.Number `json:"staked, Number"`
	LastReassertionTime string      `json:"last_reassertion_time"`
}

type swaps struct {
	Swaps []swap `json:"rows"`
}

// status:
// 0 initiated, 1 issued (approved), 2 finished (user got the tokens), -1 canceled
type swap struct {
	Key               int      `json:"key"`
	SwapTimestamp     string   `json:"swap_timestamp"`
	Status            int      `json:"status"`
	ProvidedApprovals []string `json:"provided_approvals"`
}

func Watch() {
	var users []db.User
	var err error

	users, err = db.GetActiveUsers(telegram.NotifyStop, telegram.AlertStop, telegram.RemindStop)

	if err != nil {
		log.Print(err)
	}

	sendNotifications(users)
	sendAlerts(users)
	sendReminders(users)
}

func sendNotifications(users []db.User) {
	today := time.Now()
	epoch := time.Second * -30
	epoch_ago := today.Add(epoch)
	limit := "15000"
	action_names := strings.Join(flatten(notification_actions_to_watch), ",")
	account := ""

	a := getActions(epoch_ago, action_names, limit, account)
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

			// RFC3339 with miliseconds
			lc, err = time.Parse("2006-01-02T15:04:05.9Z07:00", user.LastCheck)
			if err != nil {
				log.Print(err)
			}

			for _, account := range user.Accounts {
				for _, action := range a.Actions {

					notification := user.TelegramID + string(action.GlobalSequence)

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

						message := "_" + time.Now().Format("Mon Jan _2 15:04 2006 UTC") + "_"
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
	var top_21_chosen_time time.Time
	var snooze time.Time
	var bp_chosen_time time.Time
	var most_recent_swap time.Time
	var err error

	p := getProducers()
	s := getSwaps()

	for _, swap := range s.Swaps {
		var ts time.Time

		ts, err = time.Parse("2006-01-02T15:04:05.9", swap.SwapTimestamp)
		if err != nil {
			log.Print(err)
		}

		if ts.After(most_recent_swap) {
			most_recent_swap = ts
		}
	}

	all_producers := []string{}
	missed_init := producers{}
	missed_setprice := producers{}

	today := time.Now()
	actions_cutoff := today.Add(time.Hour * -12)
	limit := "15000"
	action_names := strings.Join(alert_actions_to_watch, ",")

	for _, producer := range p.Producers {
		if !stringInSlice(producer.Owner, all_producers) {
			all_producers = append(all_producers, producer.Owner)
		}
	}

	all_producers_s := strings.Join(all_producers, ",")
	a := getActions(actions_cutoff, action_names, limit, all_producers_s)
	negative_two_hours := time.Hour * -2
	two_hours_ago := today.Add(negative_two_hours)

	for _, producer := range p.Producers {

		bp_chosen_time, err = time.Parse("2006-01-02T15:04:05.9", producer.Top21ChosenTime)
		if err != nil {
			log.Print(err)
		}

		found_init := false
		found_setprice := false

		for _, action := range a.Actions {
			var ts time.Time
			account_match := actorIsInAuth(action.Act.Authorizations, producer.Owner)

			if account_match {

				ts, err = time.Parse("2006-01-02T15:04:05.9", action.Timestamp)
				if err != nil {
					log.Print(err)
				}

				// setprice occurs most frequently
				// check by timestamp within the last 2 hours
				if action.Act.Name == "setprice" && action.Act.Account == "rem.oracle" && ts.After(two_hours_ago) {
					found_setprice = true
				}

				// check if most recent swap happened after this bp was chosen
				// and after our actions_cutoff duration
				if most_recent_swap.After(actions_cutoff) && most_recent_swap.After(bp_chosen_time) {
					// check if init action happened after most recent swap
					if action.Act.Name == "init" && action.Act.Account == "rem.swap" && ts.After(most_recent_swap) {
						found_init = true
					}
				} else {
					found_init = true
				}

			}
		}

		if !found_init {
			missed_init.Producers = append(missed_init.Producers, producer)
		}

		if !found_setprice {
			missed_setprice.Producers = append(missed_setprice.Producers, producer)
		}
	}

	for _, user := range users {

		if user.Settings.Alert.Setting != "Stop all system alerts" {

			last_alert, err = time.Parse("2006-01-02T15:04:05.9Z07:00", user.LastAlert)
			if err != nil {
				log.Print(err)
			}

			snooze, err = time.Parse("2006-01-02T15:04:05.9", user.Settings.Alert.Snooze)
			if err != nil {
				log.Print(err)
			}

			// do not alert more often than every 60 minutes
			time_for_new_alert := time.Now().After(last_alert.Add(time.Minute * 60))
			not_snoozing := time.Now().After(snooze)

			if time_for_new_alert && not_snoozing {

				missed_blocks := producers{}
				filtered_producers := producers{}
				filtered_missed_init := producers{}
				filtered_missed_setprice := producers{}

				if user.Settings.Alert.Setting == "Alert only when my producer fails" {

					for _, producer := range p.Producers {
						if stringInSlice(producer.Owner, user.Accounts) {
							filtered_producers.Producers = append(filtered_producers.Producers, producer)
						}
					}

					for _, producer := range missed_init.Producers {
						if stringInSlice(producer.Owner, user.Accounts) {
							filtered_missed_init.Producers = append(filtered_missed_init.Producers, producer)
						}
					}

					for _, producer := range missed_setprice.Producers {
						if stringInSlice(producer.Owner, user.Accounts) {
							filtered_missed_setprice.Producers = append(filtered_missed_setprice.Producers, producer)
						}
					}

				} else if user.Settings.Alert.Setting == "Alert when any producer fails" {
					filtered_producers = p
					filtered_missed_init = missed_init
					filtered_missed_setprice = missed_setprice
				}

				for _, producer := range filtered_producers.Producers {

					last_block_time, err = time.Parse("2006-01-02T15:04:05.9", producer.LastBlockTime)
					if err != nil {
						log.Print(err)
					}

					top_21_chosen_time, err = time.Parse("2006-01-02T15:04:05.9", producer.Top21ChosenTime)
					if err != nil {
						log.Print(err)
					}

					// one cycle is 126 seconds (21 producers for 6 seconds each)
					// allow two cycles missed in a row before sending an alert
					producer_missed_blocks := time.Since(last_block_time).Seconds() > 252
					chosen_long_enough := time.Since(top_21_chosen_time).Seconds() > 252

					if producer_missed_blocks && chosen_long_enough {
						missed_blocks.Producers = append(missed_blocks.Producers, producer)
					}
				}

				// send messages for every type of failure
				// missed blocks
				if len(missed_blocks.Producers) > 0 {
					block_message := "_" + time.Now().Format("Mon Jan _2 15:04 2006 UTC") + "_"
					block_message += `\n` + "The following block producers are missing blocks:"

					for _, bp := range missed_blocks.Producers {
						last_block_time, err = time.Parse("2006-01-02T15:04:05.9", bp.LastBlockTime)
						if err != nil {
							log.Print(err)
						}

						seconds_since_last := int(time.Since(last_block_time).Seconds())
						missed_blocks := strconv.Itoa(int(seconds_since_last / 120))

						if bp.LastBlockTime == "1970-01-01T00:00:00.000" {
							block_message += `\n` + "*" + bp.Owner + "* has not produced any blocks yet."
						} else {
							block_message += `\n` + "*" + bp.Owner + "* produced " + strconv.Itoa(seconds_since_last) + " seconds ago (missed " + missed_blocks + " blocks)."
						}
					}

					telegram.SendMessage(user, block_message)
				}

				// missed init
				if len(filtered_missed_init.Producers) > 0 {
					init_message := "_" + time.Now().Format("Mon Jan _2 15:04 2006 UTC") + "_"
					init_message += `\n` + "The following block producers are missing `init` action, from last 12 hours:"

					for _, bp := range filtered_missed_init.Producers {
						init_message += `\n` + "*" + bp.Owner + "*"
					}

					telegram.SendMessage(user, init_message)
				}

				// missed setprice
				if len(filtered_missed_setprice.Producers) > 0 {
					setprice_message := "_" + time.Now().Format("Mon Jan _2 15:04 2006 UTC") + "_"
					setprice_message += `\n` + "The following block producers are missing `setprice` action, from last 2 hours:"

					for _, bp := range filtered_missed_setprice.Producers {
						setprice_message += `\n` + "*" + bp.Owner + "*"
					}

					telegram.SendMessage(user, setprice_message)
				}

				user.LastAlert = time.Now().Format("2006-01-02T15:04:05.9Z07:00")
				db.UpdateLastAlert(user.TelegramID, user.LastAlert)
			}
		}
	}
}

func sendReminders(users []db.User) {
	var lr time.Time
	var err error

	v := getVoters()

	for _, user := range users {
		// RFC3339 with miliseconds
		lr, err = time.Parse("2006-01-02T15:04:05.9Z07:00", user.LastReminder)
		if err != nil {
			log.Print(err)
		}

		if user.Settings.Reminder.Setting != "Stop all reminders" {
			for _, voter := range v.Voters {
				if stringInSlice(voter.Owner, user.Accounts) {
					var last_vote time.Time

					last_vote, err = time.Parse("2006-01-02T15:04:05.9", voter.LastReassertionTime)
					if err != nil {
						log.Print(err)
					}

					days_since_vote := time.Since(last_vote).Hours() / 24
					days_since_vote_s := strconv.Itoa(int(days_since_vote))
					wants_weekly_reminder := strings.Contains(strings.ToLower(user.Settings.Reminder.Setting), "weekly")
					wants_monthly_reminder := strings.Contains(strings.ToLower(user.Settings.Reminder.Setting), "monthly")
					time_for_weekly := wants_weekly_reminder && days_since_vote >= 7
					time_for_monthly := wants_monthly_reminder && days_since_vote >= 30
					time_for_reminder := time.Since(lr).Hours() > 24

					if time_for_reminder && (time_for_weekly || time_for_monthly) {

						message := "_" + time.Now().Format("Mon Jan _2 15:04 2006 UTC") + "_"

						if time_for_monthly {
							message += `\n` + "Account *" + voter.Owner + "* needs to vote or it will lose guardian status."
						} else if time_for_weekly {
							message += `\n` + "Account *" + voter.Owner + "* should vote again; " + days_since_vote_s + " days since last vote."
						}

						telegram.SendMessage(user, message)

						user.LastReminder = time.Now().Format("2006-01-02T15:04:05.9Z07:00")
						db.UpdateLastReminder(user.TelegramID, user.LastReminder)
					}
				}
			}
		}
	}
}

func getActions(epoch_ago time.Time, action_names string, limit string, account string) actions {
	var body string
	var err error
	// convert timestamp to ISO8601 for Hyperion
	after := epoch_ago.Format("2006-01-02T15:04:05")

	// account is optional
	if len(account) > 0 {
		account = "act.authorization.actor=" + account + "&"
	}

	url := "https://rem.eon.llc/v2/history/get_actions?" + account + "act.name=" + action_names + "&limit=" + limit + "&sort=asc&after=" + after

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
		if producer.IsActive == 1 && producer.Top21ChosenTime != "1970-01-01T00:00:00.000" {
			relevant.Producers = append(relevant.Producers, producer)
		}
	}

	return relevant
}

func getVoters() voters {
	var body string
	var err error
	var staked uint64

	url := "http://rem.eon.llc/v1/chain/get_table_rows"
	data := `{"table":"voters","scope":"rem","code":"rem","limit": 1000,"json":true}`

	request := gorequest.New()
	_, body, errs := request.Post(url).Send(data).End()

	if errs != nil {
		log.Print(errs)
	}

	all := voters{}
	active := voters{}

	err = json.Unmarshal([]byte(body), &all)
	if err != nil {
		log.Print(err)
	}

	// those who have never produced a block
	// are of no concern, remove them
	for _, voter := range all.Voters {
		staked, _ = strconv.ParseUint(string(voter.Staked), 10, 64)

		if len(voter.Producers) > 0 && staked >= 2500000000 {
			active.Voters = append(active.Voters, voter)
		}
	}

	return active
}

func getSwaps() swaps {
	var body string
	var err error

	url := "http://rem.eon.llc/v1/chain/get_table_rows"
	data := `{"table":"swaps","scope":"rem.swap","code":"rem.swap","reverse":true,"limit":10,"json":true}`

	request := gorequest.New()
	_, body, errs := request.Post(url).Send(data).End()

	if errs != nil {
		log.Print(errs)
	}

	all := swaps{}
	valid := swaps{}

	err = json.Unmarshal([]byte(body), &all)
	if err != nil {
		log.Print(err)
	}

	// those who have never produced a block
	// are of no concern, remove them
	for _, swap := range all.Swaps {
		if len(swap.ProvidedApprovals) > 3 {
			valid.Swaps = append(valid.Swaps, swap)
		}
	}

	return valid
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
	} else if preference == telegram.NotifyTransfers && stringInSlice(action_name, notification_actions_to_watch[telegram.NotifyTransfers]) {
		return true
	} else if preference == telegram.NotifyChanges && stringInSlice(action_name, notification_actions_to_watch[telegram.NotifyChanges]) {
		return true
	}

	return false
}

func parseData(data map[string]interface{}, action_name string) string {
	var output string
	var err error

	if stringInSlice(action_name, notification_actions_to_watch[telegram.NotifyTransfers]) {

		jsonString, _ := json.Marshal(data)

		t := transfer{}

		err = json.Unmarshal(jsonString, &t)
		if err != nil {
			log.Print(err)
		}

		output = "From: *" + t.From + "*"
		output += `\n` + "To: *" + t.To + "*"
		output += `\n` + "Quantity: *" + t.Quantity + "*"

	} else if stringInSlice(action_name, notification_actions_to_watch[telegram.NotifyChanges]) {
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
