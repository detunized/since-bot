.PHONY: default
default:
	go build && ./since-bot
	sqlite3 since.db .dump
