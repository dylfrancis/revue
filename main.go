package main

import (
	"log"
	"os"

	"github.com/dylfrancis/revue/db"
	"github.com/dylfrancis/revue/server"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	database, err := db.Connect("./revue.db")
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Println("error closing db:", err)
		}
	}()

	slackBotToken := os.Getenv("SLACK_BOT_TOKEN")
	if slackBotToken == "" {
		log.Fatal("SLACK_BOT_TOKEN is required")
	}

	if err := server.Start("8080", slackBotToken, database); err != nil {
		log.Fatal(err)
	}
}
