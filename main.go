package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	_ "github.com/mattn/go-sqlite3"
)

// Config represents the structure of the config.json file
type Config struct {
	Token string `json:"token"`
}

func readConfig() Config {
	file, err := os.Open("config.json")
	if err != nil {
		log.Panic(err)
	}

	defer file.Close()
	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		log.Panic(err)
	}

	var config Config
	err = json.Unmarshal(bytes, &config)
	if err != nil {
		log.Panic(err)
	}

	return config
}

func store(message *tgbotapi.Message, db *sql.DB) {
	_, err := db.Exec("INSERT INTO events (user, name, date) VALUES ($1, $2, $3);",
		message.From.ID,
		message.Text,
		message.Date)
	if err != nil {
		log.Panic(err)
	}
}

func reply(message *tgbotapi.Message, db *sql.DB, bot *tgbotapi.BotAPI) {
	store(message, db)

	text := fmt.Sprintf("> %s", message.Text)
	msg := tgbotapi.NewMessage(message.Chat.ID, text)

	bot.Send(msg)
}

func openDatabase() *sql.DB {
	db, err := sql.Open("sqlite3", "./since.db")
	if err != nil {
		log.Panic(err)
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS events (" +
		"id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT, " +
		"user INTEGER, " +
		"name TEXT, " +
		"date INTEGER);")

	if err != nil {
		log.Panic(err)
	}

	return db
}

func main() {
	config := readConfig()

	db := openDatabase()
	defer db.Close()

	bot, err := tgbotapi.NewBotAPI(config.Token)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = false
	log.Printf("Authorized on account %s", bot.Self.UserName)

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60

	updates, err := bot.GetUpdatesChan(updateConfig)
	if err != nil {
		log.Panic(err)
	}

	for update := range updates {
		// Ignore any non-Message Updates
		if update.Message == nil {
			continue
		}

		log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

		go reply(update.Message, db, bot)
	}
}
