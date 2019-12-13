package telegram

import (
	"../db"
	_ "bytes"
	"encoding/json"
	_ "fmt"
	"github.com/joho/godotenv"
	"github.com/parnurzeal/gorequest"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var config map[string]string

const (
	api_key        = "API_KEY"
	webhook_key    = "TELEGRAM_WEBHOOK_KEY"
	start          = "/start"
	main_menu      = "main menu"
	add_account    = "add account"
	remove_account = "remove account"
	show_accounts  = "show accounts"
	settings       = "settings"
	cancel         = "cancel"

	NotifyAll       = "Send me all alerts"
	NotifyTransfers = "Alert only about token transfers"
	NotifyChanges   = "Alert only about account changes"
	NotifyStop      = "Stop all notifications"
)

type response struct {
	ID              json.Number `json:"id,Number"`
	UpdateID        json.Number `json:"update_id,Number"`
	Callback        *response   `json:"callback_query"`
	Message         message     `json:"message"`
	ChatInstance    string      `json:"chat_instance"`
	Data            string      `json:"data"`
	InlineMessageID string      `json:"inline_message_id"`
	GameShortName   string      `json:"game_short_name"`
}

type confirmation struct {
	Ok      bool    `json:"ok"`
	Message message `json:"result"`
}

type message struct {
	Date    int         `json:"date"`
	Chat    chat        `json:"chat"`
	ID      json.Number `json:"message_id,Number"`
	Text    string      `json:"text"`
	ReplyTo *message    `json:"reply_to_message"`
}

type chat struct {
	ID        json.Number `json:"id,Number"`
	Type      string      `json:"type"`
	FirstName string      `json:"first_name"`
	LastName  string      `json:"last_name"`
	Username  string      `json:"username"`
}

type user struct {
	ID        json.Number `json:"id,Number"`
	FirstName string      `json:"first_name"`
	LastName  string      `json:"last_name"`
	Username  string      `json:"username"`
}

type account struct {
	Code int    `json:"code"`
	Name string `json:"account_name"`
}

type Button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

var default_keyboard = [][]Button{
	[]Button{
		Button{
			Text: add_account,
		},
		Button{
			Text: remove_account,
		},
	},
	[]Button{
		Button{
			Text: show_accounts,
		},
		Button{
			Text: settings,
		},
	},
}

var cancel_keyboard = [][]Button{
	[]Button{
		Button{
			Text: cancel,
		},
	},
}

func init() {
	config = apiConfig()
}

func Webhook(w http.ResponseWriter, r *http.Request) {
	// buf := new(bytes.Buffer)
	// buf.ReadFrom(r.Body)
	// newStr := buf.String()
	// log.Print(newStr)

	key := r.URL.Query().Get("key")

	if key != config[webhook_key] {

		http.Error(w, "Invalid key provided", 500)

	} else {

		var user db.User
		var data response
		var err error

		err = json.NewDecoder(r.Body).Decode(&data)
		if err != nil {
			log.Print(err)
		}

		if data.Callback != nil {

			chat_id := string(data.Callback.Message.Chat.ID)
			user, err = db.GetUser(chat_id)
			user.Settings.Notification = data.Callback.Data

			updateInlineKeyboard(user)
			answerCallback(string(data.Callback.ID))

		} else if (data.Message != message{}) {

			message := strings.TrimSpace(data.Message.Text)
			chat_id := string(data.Message.Chat.ID)
			user, err = db.GetUser(chat_id)

			// must be first interaction
			// save user to db
			if err != nil && user.ID == 0 {
				user.TelegramID = chat_id
				user.Accounts = []string{}
				user.Editing = false
				user.Adding = true
				user.Settings = db.Settings{Notification: NotifyAll}
				user.LastCheck = time.Now().Format(time.RFC3339)
				db.InsertUser(user)
			}

			if user.Editing { // bot expects an answer till cancelation is called

				if message == "cancel" {

					cancelEditing(user)

				} else {

					message = strings.ToLower(message)
					processEditing(user, message)
				}

			} else { // bot does not expect an answer, open interaction

				switch message {
				case start:

					greet(user)

				case main_menu:

					mainMenu(user)

				case show_accounts:

					showAccounts(user)

				case add_account:

					addAccount(user)

				case remove_account:

					removeAccount(user)

				case settings:

					openSettings(user)

				default:

					unknownCommand(user)

				}
			}
		}
	}
}

func greet(user db.User) {
	text := "Hi, it's nice to meet you!"
	inline := false

	sendMessageWithKeyboard(user, text, default_keyboard, inline)
}

func mainMenu(user db.User) {
	text := "Main menu, what would you like to do next?"
	inline := false

	sendMessageWithKeyboard(user, text, default_keyboard, inline)
}

func showAccounts(user db.User) {
	var text string
	inline := false

	if len(user.Accounts) > 0 {

		text = "You're monitoring these accounts:"

		for _, account := range user.Accounts {
			text += `\n` + "*" + account + "*"
		}

	} else {
		text = "You aren't monitoring any accounts."
	}

	sendMessageWithKeyboard(user, text, default_keyboard, inline)
}

func addAccount(user db.User) {
	editing := true
	adding := true
	inline := false

	db.UpdateUserEditing(user.TelegramID, editing, adding)

	text := "Enter the name of a REM account you'd like to monitor."

	sendMessageWithKeyboard(user, text, cancel_keyboard, inline)
}

func removeAccount(user db.User) {
	var text string
	var keyboard [][]Button
	inline := false

	if len(user.Accounts) > 0 {

		editing := true
		adding := false

		db.UpdateUserEditing(user.TelegramID, editing, adding)

		text = "Enter the name of a REM account you'd like to stop monitoring."
		keyboard = cancel_keyboard

	} else {
		text = "You aren't monitoring any accounts."
		keyboard = default_keyboard
	}

	sendMessageWithKeyboard(user, text, keyboard, inline)
}

func cancelEditing(user db.User) {
	editing := false
	adding := true
	inline := false

	db.UpdateUserEditing(user.TelegramID, editing, adding)

	text := "Ok, back to main menu."
	sendMessageWithKeyboard(user, text, default_keyboard, inline)
}

func processEditing(user db.User, message string) {
	var text string
	keyboard := cancel_keyboard
	inline := false

	if len(message) != 12 {
		text = "Invalid account name. REM account names are 12 characters long."
	} else {

		switch adding := user.Adding; adding {
		case true: // adding an account
			if accountExists(message) {
				if stringInSlice(message, user.Accounts) {
					text = "You are already monitoring this account."
				} else {
					text = "Added *" + message + "* account to your monitored list."
					user.Accounts = append(user.Accounts, message)
					db.UpdateUserAccounts(user.TelegramID, user.Accounts)
				}
			} else {
				text = "An account with that name does not exist."
			}
		default: // removing an account
			if stringInSlice(message, user.Accounts) {
				text = "Removed *" + message + "* account from your monitored list."
				user.Accounts = removeStringFromSlice(user.Accounts, message)
				db.UpdateUserAccounts(user.TelegramID, user.Accounts)
			} else {
				text = "This account is not on your monitored list."
			}
		}
	}

	sendMessageWithKeyboard(user, text, keyboard, inline)
}

func openSettings(user db.User) {
	text := "Please select the level of notification you would like to receive."
	inline := true

	keyboard := [][]Button{
		[]Button{
			Button{
				Text:         markSelectedButton(user.Settings.Notification, NotifyAll),
				CallbackData: NotifyAll,
			},
		},
		[]Button{
			Button{
				Text:         markSelectedButton(user.Settings.Notification, NotifyTransfers),
				CallbackData: NotifyTransfers,
			},
		},
		[]Button{
			Button{
				Text:         markSelectedButton(user.Settings.Notification, NotifyChanges),
				CallbackData: NotifyChanges,
			},
		},
		[]Button{
			Button{
				Text:         markSelectedButton(user.Settings.Notification, NotifyStop),
				CallbackData: NotifyStop,
			},
		},
	}

	sendMessageWithKeyboard(user, text, keyboard, inline)
}

func unknownCommand(user db.User) {
	text := "Unknown command."
	inline := false

	sendMessageWithKeyboard(user, text, default_keyboard, inline)
}

func SendMessage(user db.User, text string) {
	url := "https://api.telegram.org/bot" + config[api_key] + "/sendMessage"
	var errs []error

	data := `{"chat_id":"` + user.TelegramID + `", "text":"` + text + `", "parse_mode": "Markdown"}`

	request := gorequest.New()
	_, _, errs = request.Post(url).Send(data).End()

	if errs != nil {
		log.Print(errs)
	}
}

func sendMessageWithKeyboard(user db.User, text string, keyboard [][]Button, inline bool) {
	url := "https://api.telegram.org/bot" + config[api_key] + "/sendMessage"
	var errs []error
	var markup string
	var body string

	k, err := json.Marshal(keyboard)
	if err != nil {
		panic(err)
	}

	if inline {
		markup = `"inline_keyboard": ` + string(k)
	} else {
		markup = `"keyboard": ` + string(k)
	}

	data := `{"chat_id":"` + user.TelegramID + `", "text":"` + text + `", "parse_mode": "Markdown", "reply_markup": {` + markup + `, "resize_keyboard": true}}`

	request := gorequest.New()
	_, body, errs = request.Post(url).Send(data).End()

	if errs != nil {
		log.Print(errs)
	}

	if inline {
		c := confirmation{}

		err = json.Unmarshal([]byte(body), &c)
		if err != nil {
			log.Print(err)
		}

		user.Settings.MessageID = c.Message.ID

		db.UpdateSettings(user.TelegramID, user.Settings)
	}
}

func updateInlineKeyboard(user db.User) {
	url := "https://api.telegram.org/bot" + config[api_key] + "/editMessageReplyMarkup"
	var errs []error
	var markup string

	keyboard := [][]Button{
		[]Button{
			Button{
				Text:         markSelectedButton(user.Settings.Notification, NotifyAll),
				CallbackData: NotifyAll,
			},
		},
		[]Button{
			Button{
				Text:         markSelectedButton(user.Settings.Notification, NotifyTransfers),
				CallbackData: NotifyTransfers,
			},
		},
		[]Button{
			Button{
				Text:         markSelectedButton(user.Settings.Notification, NotifyChanges),
				CallbackData: NotifyChanges,
			},
		},
		[]Button{
			Button{
				Text:         markSelectedButton(user.Settings.Notification, NotifyStop),
				CallbackData: NotifyStop,
			},
		},
	}

	k, err := json.Marshal(keyboard)
	if err != nil {
		panic(err)
	}

	markup = `"inline_keyboard": ` + string(k)

	data := `{"chat_id":"` + user.TelegramID + `", "message_id":"` + string(user.Settings.MessageID) + `", "reply_markup": {` + markup + `}}`

	request := gorequest.New()
	_, _, errs = request.Post(url).Send(data).End()

	if errs != nil {
		log.Print(errs)
	}

	db.UpdateSettings(user.TelegramID, user.Settings)
}

func accountExists(name string) bool {
	url := "https://rem.eon.llc/v1/chain/get_account"
	var errs []error
	var err error
	var body string

	data := `{"account_name":"` + name + `"}`

	request := gorequest.New()
	_, body, errs = request.Post(url).Send(data).End()

	if errs != nil {
		log.Print(errs)
	}

	a := account{}

	err = json.Unmarshal([]byte(body), &a)
	if err != nil {
		log.Print(err)
	}

	if a.Code == 500 {
		return false
	} else {
		return true
	}

}

func answerCallback(callback_query_id string) {
	url := "https://api.telegram.org/bot" + config[api_key] + "/answerCallbackQuery"
	var errs []error

	data := `{"callback_query_id":"` + callback_query_id + `"}`

	request := gorequest.New()
	_, _, errs = request.Post(url).Send(data).End()

	if errs != nil {
		log.Print(errs)
	}
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func markSelectedButton(current string, text string) string {
	// currently uses ‣ character
	if current == text {
		return "\xe2\x80\xa3 " + text
	} else {
		return text
	}
}

func removeStringFromSlice(s []string, r string) []string {
	for i, v := range s {
		if v == r {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func apiConfig() map[string]string {
	err := godotenv.Load("/root/rem-alert-api/.env")
	if err != nil {
		log.Print("Error loading .env file")
	}

	conf := make(map[string]string)

	conf[api_key] = os.Getenv(api_key)
	conf[webhook_key] = os.Getenv(webhook_key)

	return conf
}
