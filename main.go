package main

import (
	"./telegram"
	"./watchman"
	"github.com/gorilla/mux"
	"github.com/jasonlvhit/gocron"
	"log"
	"net/http"
	"time"
)

func main() {
	startParser()
	startServer()
}

func startParser() {
	go func() {
		process := gocron.NewScheduler()
		process.Every(1).Seconds().Do(watchman.Watch)
		<-process.Start()
	}()
}

func startServer() {
	router := mux.NewRouter().StrictSlash(true)

	router.HandleFunc("/alerts/telegram", telegram.Webhook).Methods("POST")

	srv := &http.Server{
		Handler:      router,
		Addr:         "127.0.0.1:8090",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())
}
