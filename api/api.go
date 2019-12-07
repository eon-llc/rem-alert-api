package api

import (
	"encoding/json"
	_ "fmt"
	"github.com/joho/godotenv"
	"github.com/parnurzeal/gorequest"
	"log"
	"net/http"
	"os"
)

var config map[string]string

const (
	api_key        = "API_KEY"
	main_menu      = "main menu"
	add_account    = "add account"
	remove_account = "remove account"
	show_accounts  = "show accounts"
)

type response struct {
	UpdateID json.Number `json:"update_id,Number"`
	Message  message     `json:"message"`
}

type message struct {
	Date int         `json:"date"`
	Chat chat        `json:"chat"`
	ID   json.Number `json:"message_id,Number"`
	Text string      `json:"text"`
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

type keyboard struct {
	Buttons [][]string `json:"keyboard"`
}

func init() {
	config = apiConfig()
}

func Webhook(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)

	var data response
	var text string

	keyboard := [][]string{
		[]string{
			add_account,
			remove_account,
		},
		[]string{
			show_accounts,
		},
	}

	err := decoder.Decode(&data)
	if err != nil {
		panic(err)
	}

	chat_id := string(data.Message.Chat.ID)

	switch message := data.Message.Text; message {
	case main_menu:
		text = "Main menu, what would you like to do next?"
		sendMessage(chat_id, text, keyboard)
	case show_accounts:
		text = "You're monitoring these accounts:"
		sendMessage(chat_id, text, keyboard)
	case add_account:
		text = "Enter the name of a REM account you'd like to monitor."
		keyboard = [][]string{
			[]string{
				main_menu,
			},
		}
		sendMessage(chat_id, text, keyboard)
	case remove_account:
		text = "Enter the name of a REM account you'd like to stop monitoring."
		keyboard = [][]string{
			[]string{
				main_menu,
			},
		}
		sendMessage(chat_id, text, keyboard)
	default:
		// must be an account name user is trying to add
		// check if account exists
		// if exists, check if user already monitors this address
		if len(message) != 12 {
			text = "Invalid account name. Account names are exactly 12 characters long."

		} else {

			account_exists := false

			if account_exists {
				text = "Added account " + message + " to your monitored list."
			} else {
				text = "Could not find an account with that name."
			}
		}

		sendMessage(chat_id, text, keyboard)
	}
}

func sendMessage(chat_id string, text string, keyboard [][]string) {
	url := "https://api.telegram.org/bot" + config[api_key] + "/sendMessage"
	var err error
	var errs []error
	var k []byte
	var body string

	k, err = json.Marshal(keyboard)
	if err != nil {
		panic(err)
	}

	data := `{"chat_id":"` + chat_id + `", "text":"` + text + `", "reply_markup": {"keyboard": ` + string(k) + `}}`

	request := gorequest.New()
	_, body, errs = request.Post(url).Send(data).End()

	if errs != nil {
		log.Print(errs)
	}

	log.Print(body)
}

func apiConfig() map[string]string {
	err := godotenv.Load("/root/rem-alert-api/.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	conf := make(map[string]string)

	conf[api_key] = os.Getenv(api_key)

	return conf
}
