.PHONY: default
default:
	go build && ./since-bot

.PHONY: debug-chart
debug-chart:
	watchman-make -p '**/*.go' -r 'SINCE_BOT_DEBUG_CHART=1 make default && open debug.png'