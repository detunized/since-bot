package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"

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

	statement := connection.Prep("INSERT INTO events (user, name, date) VALUES ($user, $name, $date);")
	statement.SetInt64("$user", int64(message.From.ID))
	statement.SetText("$name", message.Text)
	statement.SetInt64("$date", int64(message.Date))

	row, err := statement.Step()
	if err != nil {
		log.Panic(err)
	}
	log.Print("row ", row)
}

func reply(message *tgbotapi.Message, db *sqlitex.Pool, bot *tgbotapi.BotAPI) {
	store(message, db)

	text := fmt.Sprintf("> %s", message.Text)
	msg := tgbotapi.NewMessage(message.Chat.ID, text)

	bot.Send(msg)
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
	conn := db.Get(nil)
	defer conn.Close()

	s := conn.Prep(sql)
	s.Step()
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
