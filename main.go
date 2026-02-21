package main

import (
	"log"

	"github.com/dylfrancis/revue/db"
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
}
