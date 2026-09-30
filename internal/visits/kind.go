// Package visits classifies Hits without I/O: Visitor Kind, Verified Crawler, Arrival Channel and
// the identities a Hit is counted under.
package visits

import (
	"net/netip"
	"strings"
)

// Kind is the Visitor Kind of a Hit.
type Kind string

const (
	KindHuman            Kind = "human"
	KindHumanUnconfirmed Kind = "human-unconfirmed"
	KindSearchCrawler    Kind = "search-crawler"
	KindAICrawler        Kind = "ai-crawler"
	KindAIFetcher        Kind = "ai-fetcher"
	KindSEOTool          Kind = "seo-tool"
	KindOtherBot         Kind = "other-bot"
)

// Kinds lists every Visitor Kind in reporting order.
func Kinds() []Kind {
	return []Kind{KindHuman, KindHumanUnconfirmed, KindSearchCrawler, KindAICrawler, KindAIFetcher, KindSEOTool, KindOtherBot}
}

// IsHuman reports whether the kind counts as a person (confirmed or not).
func (k Kind) IsHuman() bool { return k == KindHuman || k == KindHumanUnconfirmed }

// Classification is who made a request, as far as the user-agent and address tell.
type Classification struct {
	Kind     Kind
	BotName  string
	Operator string
	// Verified is set only for claimed crawlers whose operator publishes address ranges.
	Verified bool
}

// Classify derives the Visitor Kind from a user-agent and client address. A claimed crawler whose
// operator publishes ranges but whose address is outside them becomes other-bot.
func Classify(ua string, ip netip.Addr, ranges *Ranges) Classification {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return Classification{Kind: KindOtherBot, BotName: "(empty)"}
	}
	if b, ok := identifyBot(ua); ok {
		c := Classification{Kind: b.kind, BotName: b.name, Operator: b.operator}
		if b.operator != "" {
			c.Verified = ranges.Contains(b.operator, ip)
			if !c.Verified {
				c.Kind = KindOtherBot
			}
		}
		return c
	}
	if name, ok := genericBot(ua); ok {
		return Classification{Kind: KindOtherBot, BotName: name}
	}
	if looksLikeBrowser(ua) {
		return Classification{Kind: KindHumanUnconfirmed}
	}
	return Classification{Kind: KindOtherBot, BotName: firstProduct(ua)}
}

type bot struct {
	token    string
	name     string
	kind     Kind
	operator string
}

// Order matters: the first token found wins, so specific tokens precede their prefixes.
func knownBots() []bot {
	return []bot{
		// AI fetchers act on a person's live question.
		{"chatgpt-user", "ChatGPT-User", KindAIFetcher, "openai"},
		{"perplexity-user", "Perplexity-User", KindAIFetcher, "perplexity"},
		{"claude-user", "Claude-User", KindAIFetcher, "anthropic"},
		{"claude-web", "Claude-Web", KindAIFetcher, "anthropic"},
		{"mistralai-user", "MistralAI-User", KindAIFetcher, ""},
		{"duckassistbot", "DuckAssistBot", KindAIFetcher, ""},
		{"meta-externalfetcher", "meta-externalfetcher", KindAIFetcher, ""},
		{"google-notebooklm", "Google-NotebookLM", KindAIFetcher, "google"},
		{"gemini-deep-research", "Gemini-Deep-Research", KindAIFetcher, "google"},
		// AI crawlers collect for training or AI search indexes.
		{"gptbot", "GPTBot", KindAICrawler, "openai"},
		{"oai-searchbot", "OAI-SearchBot", KindAICrawler, "openai"},
		{"claude-searchbot", "Claude-SearchBot", KindAICrawler, "anthropic"},
		{"claudebot", "ClaudeBot", KindAICrawler, "anthropic"},
		{"anthropic-ai", "anthropic-ai", KindAICrawler, "anthropic"},
		{"perplexitybot", "PerplexityBot", KindAICrawler, "perplexity"},
		{"ccbot", "CCBot", KindAICrawler, "commoncrawl"},
		{"google-cloudvertexbot", "Google-CloudVertexBot", KindAICrawler, "google"},
		{"bytespider", "Bytespider", KindAICrawler, ""},
		{"amazonbot", "Amazonbot", KindAICrawler, ""},
		{"meta-externalagent", "meta-externalagent", KindAICrawler, ""},
		{"facebookbot", "FacebookBot", KindAICrawler, ""},
		{"cohere-training-data-crawler", "cohere-training-data-crawler", KindAICrawler, ""},
		{"cohere-ai", "cohere-ai", KindAICrawler, ""},
		{"diffbot", "Diffbot", KindAICrawler, ""},
		{"youbot", "YouBot", KindAICrawler, ""},
		{"timpibot", "Timpibot", KindAICrawler, ""},
		{"imagesiftbot", "ImagesiftBot", KindAICrawler, ""},
		{"ai2bot", "AI2Bot", KindAICrawler, ""},
		{"pangubot", "PanguBot", KindAICrawler, ""},
		{"kangaroo bot", "Kangaroo Bot", KindAICrawler, ""},
		{"omgilibot", "omgilibot", KindAICrawler, ""},
		{"img2dataset", "img2dataset", KindAICrawler, ""},
		{"deepseekbot", "DeepSeekBot", KindAICrawler, ""},
		{"mistralai", "MistralAI", KindAICrawler, ""},
		{"tiktokspider", "TikTokSpider", KindAICrawler, ""},
		// Search engines.
		{"googlebot-image", "Googlebot-Image", KindSearchCrawler, "google"},
		{"googlebot-news", "Googlebot-News", KindSearchCrawler, "google"},
		{"googlebot-video", "Googlebot-Video", KindSearchCrawler, "google"},
		{"googlebot", "Googlebot", KindSearchCrawler, "google"},
		{"storebot-google", "Storebot-Google", KindSearchCrawler, "google"},
		{"google-inspectiontool", "Google-InspectionTool", KindSearchCrawler, "google"},
		{"googleother", "GoogleOther", KindSearchCrawler, "google"},
		{"adsbot-google", "AdsBot-Google", KindSearchCrawler, "google"},
		{"mediapartners-google", "Mediapartners-Google", KindSearchCrawler, "google"},
		{"apis-google", "APIs-Google", KindSearchCrawler, "google"},
		{"google-site-verification", "Google-Site-Verification", KindSearchCrawler, "google"},
		{"feedfetcher-google", "FeedFetcher-Google", KindSearchCrawler, "google"},
		{"bingbot", "bingbot", KindSearchCrawler, "bing"},
		{"bingpreview", "BingPreview", KindSearchCrawler, "bing"},
		{"adidxbot", "adidxbot", KindSearchCrawler, "bing"},
		{"microsoftpreview", "MicrosoftPreview", KindSearchCrawler, "bing"},
		{"applebot", "Applebot", KindSearchCrawler, "apple"},
		{"duckduckbot", "DuckDuckBot", KindSearchCrawler, ""},
		{"yandex", "YandexBot", KindSearchCrawler, ""},
		{"baiduspider", "Baiduspider", KindSearchCrawler, ""},
		{"yahoo! slurp", "Yahoo! Slurp", KindSearchCrawler, ""},
		{"seznambot", "SeznamBot", KindSearchCrawler, ""},
		{"qwantbot", "Qwantbot", KindSearchCrawler, ""},
		{"qwant-news", "Qwantbot", KindSearchCrawler, ""},
		{"petalbot", "PetalBot", KindSearchCrawler, ""},
		{"sogou", "Sogou", KindSearchCrawler, ""},
		{"exabot", "Exabot", KindSearchCrawler, ""},
		{"mojeekbot", "MojeekBot", KindSearchCrawler, ""},
		{"coccocbot", "coccocbot", KindSearchCrawler, ""},
		{"yeti/", "Yeti", KindSearchCrawler, ""},
		{"ecosiabot", "EcosiaBot", KindSearchCrawler, ""},
		{"braveBot", "BraveBot", KindSearchCrawler, ""},
		// SEO tools.
		{"ahrefssiteaudit", "AhrefsSiteAudit", KindSEOTool, ""},
		{"ahrefsbot", "AhrefsBot", KindSEOTool, ""},
		{"semrushbot", "SemrushBot", KindSEOTool, ""},
		{"siteauditbot", "SiteAuditBot", KindSEOTool, ""},
		{"mj12bot", "MJ12bot", KindSEOTool, ""},
		{"dotbot", "DotBot", KindSEOTool, ""},
		{"rogerbot", "rogerbot", KindSEOTool, ""},
		{"screaming frog", "Screaming Frog SEO Spider", KindSEOTool, ""},
		{"serpstatbot", "serpstatbot", KindSEOTool, ""},
		{"dataforseobot", "DataForSeoBot", KindSEOTool, ""},
		{"blexbot", "BLEXBot", KindSEOTool, ""},
		{"barkrowler", "Barkrowler", KindSEOTool, ""},
		{"seokicks", "SEOkicks", KindSEOTool, ""},
		{"sitebulb", "Sitebulb", KindSEOTool, ""},
		{"seobilitybot", "SeobilityBot", KindSEOTool, ""},
		{"chrome-lighthouse", "Chrome-Lighthouse", KindSEOTool, ""},
		{"siteimprove", "Siteimprove", KindSEOTool, ""},
		{"contentking", "ContentKing", KindSEOTool, ""},
		{"oncrawl", "OnCrawl", KindSEOTool, ""},
		{"jetoctopus", "JetOctopus", KindSEOTool, ""},
		{"seranking", "SE Ranking", KindSEOTool, ""},
		// Link previews, monitors and HTTP libraries.
		{"facebookexternalhit", "facebookexternalhit", KindOtherBot, ""},
		{"twitterbot", "Twitterbot", KindOtherBot, ""},
		{"linkedinbot", "LinkedInBot", KindOtherBot, ""},
		{"slackbot", "Slackbot", KindOtherBot, ""},
		{"discordbot", "Discordbot", KindOtherBot, ""},
		{"telegrambot", "TelegramBot", KindOtherBot, ""},
		{"whatsapp/", "WhatsApp", KindOtherBot, ""},
		{"pinterestbot", "Pinterestbot", KindOtherBot, ""},
		{"redditbot", "redditbot", KindOtherBot, ""},
		{"skypeuripreview", "SkypeUriPreview", KindOtherBot, ""},
		{"embedly", "Embedly", KindOtherBot, ""},
		{"uptimerobot", "UptimeRobot", KindOtherBot, ""},
		{"pingdom", "Pingdom", KindOtherBot, ""},
		{"statuscake", "StatusCake", KindOtherBot, ""},
		{"headlesschrome", "HeadlessChrome", KindOtherBot, ""},
		{"phantomjs", "PhantomJS", KindOtherBot, ""},
	}
}

func identifyBot(ua string) (bot, bool) {
	lower := strings.ToLower(ua)
	for _, b := range knownBots() {
		if strings.Contains(lower, strings.ToLower(b.token)) {
			return b, true
		}
	}
	return bot{}, false
}

func libraries() []string {
	return []string{
		"curl", "wget", "python-requests", "python-urllib", "python-httpx", "aiohttp", "go-http-client",
		"okhttp", "axios", "node-fetch", "undici", "java", "apache-httpclient", "libwww-perl", "ruby", "scrapy", "httpie", "postmanruntime",
	}
}

func genericBot(ua string) (string, bool) {
	lower := strings.ToLower(ua)
	first := strings.ToLower(firstProduct(ua))
	for _, lib := range libraries() {
		if first == lib {
			return firstProduct(ua), true
		}
	}
	for _, marker := range []string{"bot", "crawl", "spider", "slurp", "fetch", "scan", "monitor", "http://", "https://"} {
		if strings.Contains(lower, marker) {
			return botToken(ua), true
		}
	}
	return "", false
}

// botToken picks the product token that names the bot, e.g. "ExampleBot" in a Mozilla-compatible UA.
func botToken(ua string) string {
	for _, f := range strings.FieldsFunc(ua, func(r rune) bool { return r == ' ' || r == ';' || r == '(' || r == ')' }) {
		name, _, _ := strings.Cut(f, "/")
		l := strings.ToLower(name)
		if strings.Contains(l, "bot") || strings.Contains(l, "crawl") || strings.Contains(l, "spider") {
			return name
		}
	}
	return firstProduct(ua)
}

func firstProduct(ua string) string {
	f := strings.Fields(ua)
	name, _, _ := strings.Cut(f[0], "/")
	return name
}

func looksLikeBrowser(ua string) bool {
	if !strings.HasPrefix(ua, "Mozilla/5.0 (") {
		return false
	}
	return strings.Contains(ua, "AppleWebKit/") || strings.Contains(ua, "Gecko/") || strings.Contains(ua, "Trident/")
}
