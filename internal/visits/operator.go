package visits

import "strings"

// Operator names the company behind a known bot, or "" when the bot is unknown.
func Operator(botName string) string {
	n := strings.ToLower(botName)
	for _, o := range []struct{ prefix, name string }{
		{"chatgpt", "OpenAI"},
		{"gptbot", "OpenAI"},
		{"oai-", "OpenAI"},
		{"claude", "Anthropic"},
		{"anthropic", "Anthropic"},
		{"perplexity", "Perplexity"},
		{"google", "Google"},
		{"gemini", "Google"},
		{"apis-google", "Google"},
		{"adsbot-google", "Google"},
		{"mediapartners-google", "Google"},
		{"storebot-google", "Google"},
		{"feedfetcher-google", "Google"},
		{"bingbot", "Microsoft"},
		{"bingpreview", "Microsoft"},
		{"adidxbot", "Microsoft"},
		{"microsoftpreview", "Microsoft"},
		{"applebot", "Apple"},
		{"ccbot", "Common Crawl"},
		{"bytespider", "ByteDance"},
		{"tiktokspider", "ByteDance"},
		{"amazonbot", "Amazon"},
		{"meta-", "Meta"},
		{"facebook", "Meta"},
		{"cohere", "Cohere"},
		{"mistralai", "Mistral"},
		{"duckassistbot", "DuckDuckGo"},
		{"duckduckbot", "DuckDuckGo"},
		{"yandex", "Yandex"},
		{"baiduspider", "Baidu"},
		{"deepseekbot", "DeepSeek"},
		{"diffbot", "Diffbot"},
		{"youbot", "You.com"},
		{"ai2bot", "Allen Institute"},
		{"petalbot", "Huawei"},
		{"pangubot", "Huawei"},
		{"ahrefs", "Ahrefs"},
		{"semrushbot", "Semrush"},
		{"siteauditbot", "Semrush"},
		{"mj12bot", "Majestic"},
		{"dotbot", "Moz"},
		{"rogerbot", "Moz"},
		{"screaming frog", "Screaming Frog"},
		{"dataforseobot", "DataForSEO"},
		{"chrome-lighthouse", "Google"},
		{"linkedinbot", "LinkedIn"},
		{"twitterbot", "X"},
		{"slackbot", "Slack"},
		{"discordbot", "Discord"},
		{"telegrambot", "Telegram"},
		{"whatsapp", "Meta"},
		{"uptimerobot", "UptimeRobot"},
	} {
		if strings.HasPrefix(n, o.prefix) {
			return o.name
		}
	}
	return ""
}
