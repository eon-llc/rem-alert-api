package api

import (
	"encoding/json"
	_ "fmt"
	"github.com/joho/godotenv"
	_ "github.com/parnurzeal/gorequest"
	"log"
	"net/http"
	"os"
)

const (
	api_key = "API_KEY"
)

type response struct {
	UpdateID int     `json:"update_id"`
	Message  message `json:"message"`
}

type message struct {
	Date int    `json:"date"`
	Chat chat   `json:"chat"`
	ID   int    `json:"message_id"`
	Text string `json:"text"`
}

type chat struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

type user struct {
	ID        int    `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

func Webhook(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)

	var data response
	err := decoder.Decode(&data)
	if err != nil {
		panic(err)
	}
	log.Print(data)

	// config := apiConfig()
	// url := "https://api.telegram.org/bot" + config[api_key] + "/sendMessage"
	// log.Print(url)
	// data := `{"chat_id":"379613121", "text":"Great success!"}`

	// request := gorequest.New()
	// _, body, err := request.Post(url).Send(data).End()

	// if err != nil {
	// 	log.Print(err)
	// }

	// log.Print(body)

}

func apiConfig() map[string]string {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	conf := make(map[string]string)

	conf[api_key] = os.Getenv(api_key)

	return conf
}
