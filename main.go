package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"crawshaw.io/sqlite"

	"crawshaw.io/sqlite/sqlitex"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
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

func store(message *tgbotapi.Message, db *sqlitex.Pool) {
	connection := db.Get(nil)
	defer db.Put(connection)

	err := sqlitex.Exec(
		connection,
		"INSERT INTO events (user, name, date) VALUES (?, ?, ?);",
		nil,
		message.From.ID,
		message.Text,
		message.Date)

	if err != nil {
		log.Panic(err)
	}
}

func formatResponse(date int64, name string) string {
	last := time.Unix(date, 0)
	return fmt.Sprintf("Previous '%s' happened on '%v'", name, last)
}

func reply(message *tgbotapi.Message, db *sqlitex.Pool, bot *tgbotapi.BotAPI) {
	connection := db.Get(nil)
	defer db.Put(connection)

	// Default response
	name := message.Text
	response := fmt.Sprintf("Fist time for '%s'", name)

	// Get the last event with the same name and format the response
	err := sqlitex.Exec(connection,
		"SELECT date FROM events "+
			"WHERE user = ? AND name = ? "+
			"ORDER BY date "+
			"DESC LIMIT 1",
		func(s *sqlite.Stmt) error {
			response = formatResponse(s.GetInt64("date"), name)
			return nil
		},
		message.From.ID,
		name)

	if err != nil {
		log.Panic(err)
	}

	// Send the message
	go func() {
		bot.Send(tgbotapi.NewMessage(message.Chat.ID, response))
	}()

	store(message, db)
}

func openDB() *sqlitex.Pool {
	db, err := sqlitex.Open("./since.db", 0, 16)
	if err != nil {
		log.Panic(err)
	}

	execSQL(db,
		"CREATE TABLE IF NOT EXISTS events ("+
			"id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT, "+
			"user INTEGER, "+
			"name TEXT, "+
			"date INTEGER);")

	return db
}

func execSQL(db *sqlitex.Pool, sql string) {
	connection := db.Get(nil)
	defer db.Put(connection)

	err := sqlitex.Exec(connection, sql, nil)
	if err != nil {
		log.Panic(err)
	}
}

func main() {
	config := readConfig()

	db := openDB()
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
