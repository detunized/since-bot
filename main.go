package main

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"crawshaw.io/sqlite"

	"crawshaw.io/sqlite/sqlitex"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/hako/durafmt"
	"github.com/wcharczuk/go-chart"
)

const (
	defaultChartDays = 30

	defaultTopCount = 10
	minTopCount     = 3
	maxTopCount     = 25
)

//
// Debug
//

const (
	debugSendPanicToChat = true
	debugChartFilename   = "debug.png"
)

// When `debugChartEnabled` is true, the chars are rendered to the local `debug.png`
// and the process exits. See `make debug-chart`.
var debugChartEnabled = os.Getenv("SINCE_BOT_DEBUG_CHART") == "1"

func savePng(content []byte) {
	if err := ioutil.WriteFile(debugChartFilename, content, 0644); err != nil {
		log.Panic(err)
	}
}

func saveRedPng() {
	// Red PNG 100x100
	pngB64 := "iVBORw0KGgoAAAANSUhEUgAAAGQAAABkCAYAAABw4pVUAAAApElEQVR42u3R" +
		"AQ0AAAjDMO5fNCCDkC5z0HTVrisFCBABASIgQAQEiIAAAQJEQIAICBABASIgQAREQI" +
		"AICBABASIgQAREQIAICBABASIgQAREQIAICBABASIgQAREQIAICBABASIgQAREQIAI" +
		"CBABASIgQAREQIAICBABASIgQAREQIAICBABASIgQAREQIAICBABASIgQAQECBAgAg" +
		"JEQIAIyPcGFY7HnV2aPXoAAAAASUVORK5CYII="
	pngBin, _ := base64.StdEncoding.DecodeString(pngB64)
	savePng(pngBin)
}

//
// Utils
//

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

func buildSinceResponse(name string, now int64, userID int64, connection *sqlite.Conn) string {
	response := ""

	// Get the last event with the same name and format the response
	err := sqlitex.Exec(connection,
		"SELECT date FROM events "+
			"WHERE user = ? AND name = ? "+
			"ORDER BY date "+
			"DESC LIMIT 1",
		func(s *sqlite.Stmt) error {
			response = formatResponse(name, now, s.GetInt64("date"))
			return nil
		},
		userID,
		name)

	if err != nil {
		log.Panic(err)
	}

	return response
}

//
// context
//

type context struct {
	message *tgbotapi.Message
	db      *sqlitex.Pool
	bot     *tgbotapi.BotAPI
}

func (c context) sendResponse(response string, format string) {
	log.Printf("Responding to '%s' in '%s' with '%s'", c.message.From, format, response)

	if debugChartEnabled {
		saveRedPng()
		return
	}

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

func (c context) sendImage(filename string, content []byte) {
	log.Printf("Sending an image named '%s' to '%s'", filename, c.message.From)

	if debugChartEnabled {
		saveRedPng()
		return
	}

	image := tgbotapi.FileBytes{Name: filename, Bytes: content}
	_, err := c.bot.Send(tgbotapi.NewPhotoUpload(c.message.Chat.ID, image))
	if err != nil {
		log.Panic(err)
	}
}

func (c context) sendFile(filename string, content []byte) {
	log.Printf("Sending a file named '%s' to '%s'", filename, c.message.From)

	if debugChartEnabled {
		saveRedPng()
		return
	}

	file := tgbotapi.FileBytes{Name: filename, Bytes: content}
	_, err := c.bot.Send(tgbotapi.NewDocumentUpload(c.message.Chat.ID, file))
	if err != nil {
		log.Panic(err)
	}
}

func (c context) sendChart(ch chart.BarChart) {
	// Render
	buffer := &bytes.Buffer{}
	err := ch.Render(chart.PNG, buffer)
	if err != nil {
		log.Panic(err)
	}

	if debugChartEnabled {
		// Save locally
		savePng(buffer.Bytes())
	} else {
		// Send as photo
		c.sendImage("chart.png", buffer.Bytes())
	}
}

func (c context) sendKeyboard(text string, names ...string) {
	log.Printf("Sending a keyboard %v to '%s'", names, c.message.From)

	if debugChartEnabled {
		saveRedPng()
		return
	}

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

//
// Commands
//

func (c context) add(text string) {
	if text == "" {
		c.sendText("Please provide a name: *name* or /add *name* if you'd like to be formal")
		return
	}

	// DB
	connection := c.db.Get(nil)
	defer c.db.Put(connection)

	// Get stuff out the incoming message
	name := text
	date := int64(c.message.Date)

	// /add is /since + store
	response := buildSinceResponse(name, date, int64(c.message.From.ID), connection)
	if response == "" {
		response = fmt.Sprintf("First time for '%s'", name)
	}

	// Launch this one in parallel with the database access right bellow this
	go c.sendText(response)

	// Store the new item in the database
	err := sqlitex.Exec(
		connection,
		"INSERT INTO events (user, name, date) VALUES (?, ?, ?);",
		nil,
		c.message.From.ID,
		name,
		date)

	if err != nil {
		log.Panic(err)
	}
}

func (c context) chart(name string) {
	if name == "" {
		c.sendMarkdown("Please provide a name: /chart *name*")
		return
	}

	// DB
	connection := c.db.Get(nil)
	defer c.db.Put(connection)

	numDays := defaultChartDays
	now := int64(c.message.Date)
	days := make([]int64, numDays)

	done := errors.New("Done")
	err := sqlitex.Exec(
		connection,
		"SELECT date FROM events "+
			"WHERE user = ? AND name = ? "+
			"ORDER BY date DESC",
		func(s *sqlite.Stmt) error {
			date := s.GetInt64("date")

			daysAgo := int((now - date) / (24 * 60 * 60))
			if daysAgo < 0 {
				daysAgo = 0
			}

			if daysAgo >= numDays {
				return done
			}

			days[daysAgo]++

			return nil
		},
		c.message.From.ID,
		name)

	if err != nil && err != done {
		log.Panic(err)
	}

	maxValue := int64(-1)
	for _, day := range days {
		if day > maxValue {
			maxValue = day
		}
	}

	if maxValue <= 0 {
		c.sendMarkdown(fmt.Sprintf("No events named '%s' have been logged in the last %d days", name, numDays))
		return
	}

	values := make([]chart.Value, len(days))
	for i, day := range days {
		values[len(days)-i-1] = chart.Value{
			Value: float64(day),
			Style: chart.Style{
				Show:        true,
				StrokeWidth: 1,
				StrokeColor: chart.ColorAlternateGreen,
				FillColor:   chart.ColorAlternateGreen,
			},
		}
	}

	// Chart settings
	response := chart.BarChart{
		Title:      fmt.Sprintf("Activity for '%s' in the last %d days", name, numDays),
		TitleStyle: chart.StyleShow(),
		Background: chart.Style{
			Padding: chart.Box{
				Top: 40,
			},
		},
		Width:      numDays*22 + 80,
		Height:     256,
		BarWidth:   20,
		BarSpacing: 2,
		XAxis:      chart.StyleShow(),
		YAxis: chart.YAxis{
			Style:          chart.StyleShow(),
			ValueFormatter: chart.IntValueFormatter,
			Range:          &chart.ContinuousRange{Min: 0, Max: float64(maxValue)},
		},
		Bars: values,
	}

	c.sendChart(response)
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
	c.sendFile("data.csv", buffer.Bytes())
}

func (c context) help() {
	c.sendMarkdown(`
Simply send an event name to log a new event. This is equivalent to the /add command.

Available commands are:

/a, /add *name* - add a new event
/c, /chart *name* - disply some chart of event activity in the last 30 days
/e, /export - get all your data in CSV format
/h, /help - this help message
/s, /since *name* - the time since the last event with a given name was logged
/t, /top *[N]* - top 10 or *N* events
/tc, /topchart *[N]* - chart 10 or *N* events
/test - test if the bot works
`)
}

func (c context) since(name string) {
	if name == "" {
		c.sendMarkdown("Please provide a name: /since *name*")
		return
	}

	// DB
	connection := c.db.Get(nil)
	defer c.db.Put(connection)

	response := buildSinceResponse(name, int64(c.message.Date), int64(c.message.From.ID), connection)
	if response == "" {
		response = fmt.Sprintf("You don't have any events named '%s'", name)
	}

	c.sendText(response)
}

func (c context) test() {
	c.sendText("It works")
}

func (c context) top(args string) {
	num := parseTopArgs(args)

	response := strings.Builder{}
	response.WriteString(fmt.Sprintf("These are your %d most logged events:\n```\n", num))

	for _, e := range c.getTopEvents(num) {
		response.WriteString(fmt.Sprintf("%s: %d\n", e.name, e.count))
	}

	response.WriteString("```\n")

	c.sendMarkdown(response.String())
}

func (c context) topChart(args string) {
	num := parseTopArgs(args)

	// Convert values
	values := make([]chart.Value, 0, num)
	for _, e := range c.getTopEvents(num) {
		values = append(values, chart.Value{Label: e.name, Value: float64(e.count)})
	}

	// Chart settings
	response := chart.BarChart{
		Title:      fmt.Sprintf("Top %d events", num),
		TitleStyle: chart.StyleShow(),
		Background: chart.Style{
			Padding: chart.Box{
				Top: 40,
			},
		},
		Width:    num * 100,
		Height:   512,
		BarWidth: 80,
		XAxis:    chart.StyleShow(),
		YAxis: chart.YAxis{
			Style:          chart.StyleShow(),
			ValueFormatter: chart.IntValueFormatter,
			Range:          &chart.ContinuousRange{Min: 0, Max: values[0].Value},
		},
		Bars: values,
	}

	c.sendChart(response)
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}

	if value > max {
		return max
	}

	return value
}

func parseTopArgs(args string) int {
	num, err := strconv.Atoi(args)
	if err != nil {
		return defaultTopCount
	}

	return clamp(num, minTopCount, maxTopCount)
}

type topEvent struct {
	name  string
	count int64
}

func (c context) getTopEvents(num int) []topEvent {
	// DB
	connection := c.db.Get(nil)
	defer c.db.Put(connection)

	events := make([]topEvent, 0, num)
	err := sqlitex.ExecTransient(
		connection,
		fmt.Sprintf(
			"SELECT name, COUNT(name) freq FROM events "+
				"WHERE user = ? "+
				"GROUP BY name "+
				"ORDER BY freq DESC "+
				"LIMIT %d",
			num),
		func(s *sqlite.Stmt) error {
			events = append(events, topEvent{name: s.GetText("name"), count: s.GetInt64("freq")})
			return nil
		},
		c.message.From.ID)

	if err != nil {
		log.Panic(err)
	}

	return events
}

func reply(message *tgbotapi.Message, db *sqlitex.Pool, bot *tgbotapi.BotAPI) {
	// Store all the variables into the context not to pass around all the arguments everywhere
	c := context{message: message, db: db, bot: bot}

	// TODO: Should we always recover, not only in debug?
	if debugSendPanicToChat {
		defer func() {
			if r := recover(); r != nil {
				// When we recover from panic, the runtime doesn't print the callstack
				debug.PrintStack()

				// Send to the curious user as well
				c.sendMarkdown(fmt.Sprintf("*Internal error:*\n```\n%s\n```\n", r))
			}
		}()
	}

	if message.IsCommand() {
		switch command := message.Command(); command {
		case "a", "add":
			c.add(message.CommandArguments())
		case "c", "chart":
			c.chart(message.CommandArguments())
		case "e", "export":
			c.export()
		case "h", "help":
			c.help()
		case "s", "since":
			c.since(message.CommandArguments())
		case "t", "top":
			c.top(message.CommandArguments())
		case "tc", "topchart":
			c.topChart(message.CommandArguments())
		case "test":
			c.test()
		default:
			c.sendText(fmt.Sprintf("Eh? /%s?", command))
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

	if debugChartEnabled {
		c := context{
			db: db,
			message: &tgbotapi.Message{
				Date: int(time.Now().Unix()),
				From: &tgbotapi.User{ID: 37121672},
			},
		}
		c.chart("yo")
		return
	}

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
