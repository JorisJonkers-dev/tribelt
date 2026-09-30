package visits

import (
	"errors"
	"net/netip"
	"testing"
	"testing/fstest"
)

// Addresses inside each operator's bundled ranges, and one outside all of them.
const (
	googleIP     = "66.249.66.1"
	googleIPv6   = "2001:4860:4801:10::1"
	bingIP       = "157.55.39.10"
	openaiBotIP  = "132.196.86.5"
	openaiUserIP = "104.210.139.200"
	openaiSrchIP = "104.210.140.130"
	anthropicIP  = "216.73.216.10"
	perplexityIP = "107.20.236.150"
	pplxUserIP   = "44.208.221.197"
	ccbotIP      = "18.97.9.170"
	appleIP      = "17.241.208.170"
	strangerIP   = "203.0.113.7"
)

func mustRanges(t *testing.T) *Ranges {
	t.Helper()
	r, err := BundledRanges()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestClassifyCorpus(t *testing.T) {
	r := mustRanges(t)
	cases := []struct {
		name, ua, ip string
		kind         Kind
		bot          string
		verified     bool
	}{
		// Browsers.
		{"chrome mac", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36", strangerIP, KindHumanUnconfirmed, "", false},
		{"chrome windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36", strangerIP, KindHumanUnconfirmed, "", false},
		{"edge", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36 Edg/139.0.0.0", strangerIP, KindHumanUnconfirmed, "", false},
		{"firefox", "Mozilla/5.0 (X11; Linux x86_64; rv:142.0) Gecko/20100101 Firefox/142.0", strangerIP, KindHumanUnconfirmed, "", false},
		{"safari iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Mobile/15E148 Safari/604.1", strangerIP, KindHumanUnconfirmed, "", false},
		{"samsung", "Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/28.0 Chrome/130.0.0.0 Mobile Safari/537.36", strangerIP, KindHumanUnconfirmed, "", false},
		{"linkedin in-app", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [LinkedInApp]/9.30.1", strangerIP, KindHumanUnconfirmed, "", false},
		{"ie11", "Mozilla/5.0 (Windows NT 10.0; Trident/7.0; rv:11.0) like Gecko", strangerIP, KindHumanUnconfirmed, "", false},
		{"chatgpt atlas browser", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36", openaiUserIP, KindHumanUnconfirmed, "", false},

		// Search crawlers, verified and spoofed.
		{"googlebot desktop", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/140.0.7339.127 Safari/537.36", googleIP, KindSearchCrawler, "Googlebot", true},
		{"googlebot smartphone", "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.7339.127 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", googleIPv6, KindSearchCrawler, "Googlebot", true},
		{"googlebot image", "Googlebot-Image/1.0", googleIP, KindSearchCrawler, "Googlebot-Image", true},
		{"google inspection", "Mozilla/5.0 (compatible; Google-InspectionTool/1.0;)", googleIP, KindSearchCrawler, "Google-InspectionTool", true},
		{"googleother", "GoogleOther", googleIP, KindSearchCrawler, "GoogleOther", true},
		{"storebot", "Mozilla/5.0 (X11; Linux x86_64; Storebot-Google/1.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/79.0.3945.88 Safari/537.36", googleIP, KindSearchCrawler, "Storebot-Google", true},
		{"spoofed googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", strangerIP, KindOtherBot, "Googlebot", false},
		{"googlebot without ip", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "", KindOtherBot, "Googlebot", false},
		{"bingbot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm) Chrome/116.0.1938.76 Safari/537.36", bingIP, KindSearchCrawler, "bingbot", true},
		{"spoofed bingbot", "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", googleIP, KindOtherBot, "bingbot", false},
		{"applebot", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.1.1 Safari/605.1.15 (Applebot/0.1; +http://www.apple.com/go/applebot)", appleIP, KindSearchCrawler, "Applebot", true},
		{"duckduckbot", "DuckDuckBot/1.1; (+http://duckduckgo.com/duckduckbot.html)", strangerIP, KindSearchCrawler, "DuckDuckBot", false},
		{"yandex", "Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)", strangerIP, KindSearchCrawler, "YandexBot", false},
		{"baidu", "Mozilla/5.0 (compatible; Baiduspider/2.0; +http://www.baidu.com/search/spider.html)", strangerIP, KindSearchCrawler, "Baiduspider", false},
		{"seznam", "Mozilla/5.0 (compatible; SeznamBot/4.0; +https://o-seznam.cz/napoveda/vyhledavani/en/seznambot-crawler/)", strangerIP, KindSearchCrawler, "SeznamBot", false},
		{"petalbot", "Mozilla/5.0 (Linux; Android 7.0;) AppleWebKit/537.36 (KHTML, like Gecko) Mobile Safari/537.36 (compatible; PetalBot;+https://webmaster.petalsearch.com/site/petalbot)", strangerIP, KindSearchCrawler, "PetalBot", false},

		// AI crawlers.
		{"gptbot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.2; +https://openai.com/gptbot)", openaiBotIP, KindAICrawler, "GPTBot", true},
		{"gptbot spoofed", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.2; +https://openai.com/gptbot)", strangerIP, KindOtherBot, "GPTBot", false},
		{"oai-searchbot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; OAI-SearchBot/1.0; +https://openai.com/searchbot", openaiSrchIP, KindAICrawler, "OAI-SearchBot", true},
		{"claudebot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0; +claudebot@anthropic.com)", anthropicIP, KindAICrawler, "ClaudeBot", true},
		{"claude-searchbot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Claude-SearchBot/1.0; +Claude-SearchBot@anthropic.com)", anthropicIP, KindAICrawler, "Claude-SearchBot", true},
		{"perplexitybot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; PerplexityBot/1.0; +https://perplexity.ai/perplexitybot)", perplexityIP, KindAICrawler, "PerplexityBot", true},
		{"ccbot", "CCBot/2.0 (https://commoncrawl.org/faq/)", ccbotIP, KindAICrawler, "CCBot", true},
		{"ccbot spoofed", "CCBot/2.0 (https://commoncrawl.org/faq/)", anthropicIP, KindOtherBot, "CCBot", false},
		{"bytespider", "Mozilla/5.0 (Linux; Android 5.0) AppleWebKit/537.36 (KHTML, like Gecko) Mobile Safari/537.36 (compatible; Bytespider; spider-feedback@bytedance.com)", strangerIP, KindAICrawler, "Bytespider", false},
		{"amazonbot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Amazonbot/0.1; +https://developer.amazon.com/support/amazonbot) Chrome/119.0.6045.214 Safari/537.36", strangerIP, KindAICrawler, "Amazonbot", false},
		{"meta-externalagent", "meta-externalagent/1.1 (+https://developers.facebook.com/docs/sharing/webmasters/crawler)", strangerIP, KindAICrawler, "meta-externalagent", false},
		{"cohere", "cohere-ai", strangerIP, KindAICrawler, "cohere-ai", false},
		{"diffbot", "Mozilla/5.0 (X11; U; Linux i686; en-US; rv:1.9.1.2) Gecko/20090729 Firefox/3.5.2 (.NET CLR 3.5.30729; Diffbot/0.1; +http://www.diffbot.com)", strangerIP, KindAICrawler, "Diffbot", false},

		// AI fetchers.
		{"chatgpt-user", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot", openaiUserIP, KindAIFetcher, "ChatGPT-User", true},
		{"chatgpt-user spoofed", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot", strangerIP, KindOtherBot, "ChatGPT-User", false},
		{"claude-user", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Claude-User/1.0; +Claude-User@anthropic.com)", anthropicIP, KindAIFetcher, "Claude-User", true},
		{"perplexity-user", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Perplexity-User/1.0; +https://perplexity.ai/perplexity-user)", pplxUserIP, KindAIFetcher, "Perplexity-User", true},
		{"mistral", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; MistralAI-User/1.0; +https://docs.mistral.ai/robots)", strangerIP, KindAIFetcher, "MistralAI-User", false},
		{"duckassist", "DuckAssistBot/1.2; (+http://duckduckgo.com/duckassistbot.html)", strangerIP, KindAIFetcher, "DuckAssistBot", false},

		// SEO tools.
		{"ahrefs", "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)", strangerIP, KindSEOTool, "AhrefsBot", false},
		{"semrush", "Mozilla/5.0 (compatible; SemrushBot/7~bl; +http://www.semrush.com/bot.html)", strangerIP, KindSEOTool, "SemrushBot", false},
		{"mj12", "Mozilla/5.0 (compatible; MJ12bot/v1.4.8; http://mj12bot.com/)", strangerIP, KindSEOTool, "MJ12bot", false},
		{"dotbot", "Mozilla/5.0 (compatible; DotBot/1.2; +https://opensiteexplorer.org/dotbot; help@moz.com)", strangerIP, KindSEOTool, "DotBot", false},
		{"screaming frog", "Screaming Frog SEO Spider/21.0", strangerIP, KindSEOTool, "Screaming Frog SEO Spider", false},
		{"lighthouse", "Mozilla/5.0 (Linux; Android 11; moto g power (2022)) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Mobile Safari/537.36 Chrome-Lighthouse", strangerIP, KindSEOTool, "Chrome-Lighthouse", false},
		{"dataforseo", "Mozilla/5.0 (compatible; DataForSeoBot/1.0; +https://dataforseo.com/dataforseo-bot)", strangerIP, KindSEOTool, "DataForSeoBot", false},

		// Other bots.
		{"facebook preview", "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)", strangerIP, KindOtherBot, "facebookexternalhit", false},
		{"linkedin preview", "LinkedInBot/1.0 (compatible; Mozilla/5.0; Apache-HttpClient +http://www.linkedin.com)", strangerIP, KindOtherBot, "LinkedInBot", false},
		{"slack", "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)", strangerIP, KindOtherBot, "Slackbot", false},
		{"whatsapp", "WhatsApp/2.23.20.0", strangerIP, KindOtherBot, "WhatsApp", false},
		{"headless", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/140.0.0.0 Safari/537.36", strangerIP, KindOtherBot, "HeadlessChrome", false},
		{"curl", "curl/8.7.1", strangerIP, KindOtherBot, "curl", false},
		{"wget", "Wget/1.21.4", strangerIP, KindOtherBot, "Wget", false},
		{"python requests", "python-requests/2.32.3", strangerIP, KindOtherBot, "python-requests", false},
		{"go client", "Go-http-client/2.0", strangerIP, KindOtherBot, "Go-http-client", false},
		{"node fetch", "node-fetch/1.0 (+https://github.com/bitinn/node-fetch)", strangerIP, KindOtherBot, "node-fetch", false},
		{"unknown bot", "Mozilla/5.0 (compatible; ExampleCrawler/1.0; +https://example.com/crawler)", strangerIP, KindOtherBot, "ExampleCrawler", false},
		{"unknown fetcher no token", "Mozilla/5.0 (compatible; Fetcher 1.0)", strangerIP, KindOtherBot, "Mozilla", false},
		{"url in ua", "SiteChecker (+https://example.com/about)", strangerIP, KindOtherBot, "SiteChecker", false},
		{"mozilla without engine", "Mozilla/5.0 (compatible; MSIE 9.0; Windows NT 6.1)", strangerIP, KindOtherBot, "Mozilla", false},
		{"garbage", "abc", strangerIP, KindOtherBot, "abc", false},
		{"empty", "", strangerIP, KindOtherBot, "(empty)", false},
		{"whitespace", "   ", strangerIP, KindOtherBot, "(empty)", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ip netip.Addr
			if tc.ip != "" {
				ip = netip.MustParseAddr(tc.ip)
			}
			got := Classify(tc.ua, ip, r)
			if got.Kind != tc.kind || got.BotName != tc.bot || got.Verified != tc.verified {
				t.Fatalf("Classify(%q, %s) = %+v, want kind=%s bot=%q verified=%v", tc.ua, tc.ip, got, tc.kind, tc.bot, tc.verified)
			}
		})
	}
}

func TestKinds(t *testing.T) {
	if len(Kinds()) != 7 {
		t.Fatal("seven Visitor Kinds")
	}
	for _, k := range Kinds() {
		if k.IsHuman() != (k == KindHuman || k == KindHumanUnconfirmed) {
			t.Fatalf("IsHuman(%s)", k)
		}
	}
}

func TestRangesContains(t *testing.T) {
	r := mustRanges(t)
	if len(r.Operators()) != 7 {
		t.Fatalf("operators = %v", r.Operators())
	}
	if !r.Contains("google", netip.MustParseAddr("::ffff:"+googleIP)) {
		t.Fatal("IPv4-mapped IPv6 should unmap")
	}
	if r.Contains("google", netip.Addr{}) {
		t.Fatal("invalid address never matches")
	}
	var nilRanges *Ranges
	if nilRanges.Contains("google", netip.MustParseAddr(googleIP)) {
		t.Fatal("nil ranges never match")
	}
	if r.Contains("nobody", netip.MustParseAddr(googleIP)) {
		t.Fatal("unknown operator never matches")
	}
}

func TestLoadRangesErrors(t *testing.T) {
	bad := []fstest.MapFS{
		{"x-a.json": {Data: []byte("{")}},
		{"x-a.json": {Data: []byte(`{"prefixes":[{"ipv4Prefix":"nope"}]}`)}},
	}
	for i, fsys := range bad {
		if _, err := LoadRanges(fsys); err == nil {
			t.Fatalf("case %d: want error", i)
		}
	}
	if _, err := LoadRanges(unreadable{MapFS: fstest.MapFS{"x-a.json": {Data: []byte("{}")}}}); err == nil {
		t.Fatal("read error must surface")
	}
	r, err := LoadRanges(fstest.MapFS{"acme-bot.json": {Data: []byte(`{"prefixes":[{"ipv6Prefix":"2001:db8::1/32"}]}`)}})
	if err != nil || !r.Contains("acme", netip.MustParseAddr("2001:db8:ffff::1")) {
		t.Fatalf("masked prefix: %v", err)
	}
}

type unreadable struct {
	fstest.MapFS
}

func (u unreadable) ReadFile(string) ([]byte, error) { return nil, errors.New("boom") }
