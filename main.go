package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
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

func (c context) sendResponse(response string, format string) {
	log.Printf("Responding to '%s' in '%s' with '%s'", c.message.From, format, response)

	message := tgbotapi.NewMessage(c.message.Chat.ID, response)
	message.ParseMode = format

	_, err := c.bot.Send(message)
	if err != nil {
		log.Panic(err)
	}
}

func (c context) sendText(response string) {
	c.sendResponse(response, "")
}

func (c context) sendMarkdown(response string) {
	c.sendResponse(response, "Markdown")
}

func (c context) sendFile(filename string, content []byte) {
	log.Printf("Sending a file names '%s' to '%s'", filename, c.message.From)

	file := tgbotapi.FileBytes{Name: filename, Bytes: content}
	_, err := c.bot.Send(tgbotapi.NewDocumentUpload(c.message.Chat.ID, file))
	if err != nil {
		log.Panic(err)
	}
}

func (c context) sendKeyboard(text string, names ...string) {
	log.Printf("Sending a keyboard %v to '%s'", names, c.message.From)

	var markup interface{}
	if len(names) > 0 {
		keys := []tgbotapi.KeyboardButton{}
		for _, n := range names {
			keys = append(keys, tgbotapi.NewKeyboardButton(n))
		}

		keyboard := tgbotapi.NewReplyKeyboard(keys)
		keyboard.OneTimeKeyboard = true

		markup = keyboard
	} else {
		markup = tgbotapi.NewRemoveKeyboard(false)
	}

	message := tgbotapi.NewMessage(c.message.Chat.ID, text)
	message.ReplyMarkup = markup

	_, err := c.bot.Send(message)
	if err != nil {
		log.Panic(err)
	}
}

// /add command
func (c context) add(text string) {
	// DB
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

	// Tell the user about the last event
	go c.sendText(response)

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

func (c context) export() {
	// DB
	connection := c.db.Get(nil)
	defer c.db.Put(connection)

	// CSV writer
	buffer := &bytes.Buffer{}
	csv := csv.NewWriter(buffer)

	// This is most likely a rarely used command, so we use a non caching version
	err := sqlitex.ExecTransient(
		connection,
		"SELECT name, date FROM events WHERE user = ? ORDER BY date",
		func(s *sqlite.Stmt) error {
			err := csv.Write([]string{
				s.GetText("name"),
				time.Unix(s.GetInt64("date"), 0).Format(time.RFC3339),
			})
			if err != nil {
				log.Panic(err)
			}
			return nil
		},
		c.message.From.ID)

	if err != nil {
		log.Panic(err)
	}

	// Never forget to flush when you're done
	csv.Flush()

	// There you go
	go c.sendFile("data.csv", buffer.Bytes())
}

func (c context) top() {
	// DB
	connection := c.db.Get(nil)
	defer c.db.Put(connection)

	response := strings.Builder{}
	response.WriteString("These are your 10 most logged events:\n```\n")

	// This is most likely a rarely used command, so we use a non caching version
	err := sqlitex.ExecTransient(
		connection,
		"SELECT name, COUNT(name) freq FROM events "+
			"WHERE user = ? "+
			"GROUP BY name "+
			"ORDER BY freq DESC "+
			"LIMIT 10",
		func(s *sqlite.Stmt) error {
			response.WriteString(fmt.Sprintf("%s: %d\n", s.GetText("name"), s.GetInt64("freq")))
			return nil
		},
		c.message.From.ID)

	if err != nil {
		log.Panic(err)
	}

	response.WriteString("```\n")

	go c.sendMarkdown(response.String())
}

func (c context) test() {
	go c.sendText("It works")
}

func (c context) help() {
	go c.sendKeyboard("/add", "/export", "/help", "/test", "/top")
}

func reply(message *tgbotapi.Message, db *sqlitex.Pool, bot *tgbotapi.BotAPI) {
	// Store all the variables into the context not to pass around all the arguments everywhere
	c := context{message: message, db: db, bot: bot}

	if message.IsCommand() {
		switch command := message.Command(); command {
		case "a", "add":
			c.add(message.CommandArguments())
		case "e", "export":
			c.export()
		case "h", "help":
			c.help()
		case "t", "top":
			c.top()
		case "test":
			c.test()
		default:
			c.sendText(fmt.Sprintf("Don't know what to do with '%s'", command))
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
