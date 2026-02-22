package main

import (
	"log"

	"github.com/dylfrancis/revue/db"
	"github.com/dylfrancis/revue/server"
)

func main() {
	database, err := db.Connect("./revue.db")
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Println("error closing db:", err)
		}
	}()

	if err := server.Start("8080"); err != nil {
		log.Fatal(err)
	}
}
