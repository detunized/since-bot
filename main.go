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
	"github.com/hako/durafmt"
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

func formatResponse(name string, date int64, prevDate int64) string {
	prev := time.Unix(prevDate, 0)
	now := time.Unix(date, 0)
	duration := durafmt.ParseShort(now.Sub(prev))
	return fmt.Sprintf("%s since last '%s'", duration, name)
}

type context struct {
	message *tgbotapi.Message
	db      *sqlitex.Pool
	bot     *tgbotapi.BotAPI
}

func (c context) respond(response string) {
	log.Printf("Responding to '%s' with '%s'", c.message.From, response)

	_, err := c.bot.Send(tgbotapi.NewMessage(c.message.Chat.ID, response))
	if err != nil {
		log.Panic(err)
	}
}

// /add command
func (c context) add(text string) {
	connection := c.db.Get(nil)
	defer c.db.Put(connection)

	// Get stuff out the incoming message
	name := text
	date := int64(c.message.Date)

	// Default response
	response := fmt.Sprintf("Fist time for '%s'", name)

	// Get the last event with the same name and format the response
	err := sqlitex.Exec(connection,
		"SELECT date FROM events "+
			"WHERE user = ? AND name = ? "+
			"ORDER BY date "+
			"DESC LIMIT 1",
		func(s *sqlite.Stmt) error {
			response = formatResponse(name, date, s.GetInt64("date"))
			return nil
		},
		c.message.From.ID,
		name)

	if err != nil {
		log.Panic(err)
	}

	go c.respond(response)

	// Store the new item in the database
	err = sqlitex.Exec(
		connection,
		"INSERT INTO events (user, name, date) VALUES (?, ?, ?);",
		nil,
		c.message.From.ID,
		text,
		date)

	if err != nil {
		log.Panic(err)
	}
}

func reply(message *tgbotapi.Message, db *sqlitex.Pool, bot *tgbotapi.BotAPI) {
	// Store all the variables into the context not to pass around all the arguments everywhere
	c := context{message: message, db: db, bot: bot}

	if message.IsCommand() {
		switch command := message.Command(); command {
		case "a":
		case "add":
			c.add(message.CommandArguments())
		default:
			c.respond(fmt.Sprintf("Don't know what to do with '%s'", command))
		}
	} else {
		c.add(message.Text)
	}
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
