package visits

import (
	"net/url"
	"strings"
	"testing"
)

const own = "tribelt.jorisjonkers.dev"

func TestClassifyArrivalCorpus(t *testing.T) {
	cases := []struct {
		name, referer, query string
		channel              Channel
		host, refName        string
	}{
		{"direct", "", "", ChannelDirect, "", ""},
		{"direct garbage referer", "::not a url", "", ChannelDirect, "", ""},
		{"internal", "https://tribelt.jorisjonkers.dev/sectoren", "", ChannelInternal, own, ""},
		{"internal beats utm", "https://tribelt.jorisjonkers.dev/", "utm_source=chatgpt.com", ChannelInternal, own, ""},
		{"google nl", "https://www.google.nl/", "", ChannelSearch, "google.nl", "google"},
		{"google com path", "https://www.google.com/search?q=draadogenbanden", "", ChannelSearch, "google.com", "google"},
		{"google co.uk", "https://www.google.co.uk/", "", ChannelSearch, "google.co.uk", "google"},
		{"google android app", "android-app://com.google.android.googlequicksearchbox/", "", ChannelSearch, "com.google.android.googlequicksearchbox", "google"},
		{"bing", "https://www.bing.com/", "", ChannelSearch, "bing.com", "bing"},
		{"bing chat", "https://www.bing.com/chat?q=x", "", ChannelAIChat, "bing.com", "copilot"},
		{"duckduckgo", "https://duckduckgo.com/", "", ChannelSearch, "duckduckgo.com", "duckduckgo"},
		{"ecosia", "https://www.ecosia.org/", "", ChannelSearch, "ecosia.org", "ecosia"},
		{"yahoo", "https://search.yahoo.com/", "", ChannelSearch, "search.yahoo.com", "yahoo"},
		{"yandex", "https://yandex.ru/", "", ChannelSearch, "yandex.ru", "yandex"},
		{"brave", "https://search.brave.com/", "", ChannelSearch, "search.brave.com", "brave"},
		{"startpage", "https://www.startpage.com/", "", ChannelSearch, "startpage.com", "startpage"},
		{"kagi search", "https://kagi.com/search?q=x", "", ChannelSearch, "kagi.com", "kagi"},
		{"kagi assistant", "https://kagi.com/assistant/abc", "", ChannelAIChat, "kagi.com", "kagi-assistant"},
		{"chatgpt", "https://chatgpt.com/", "", ChannelAIChat, "chatgpt.com", "chatgpt"},
		{"chat.openai", "https://chat.openai.com/c/123", "", ChannelAIChat, "chat.openai.com", "chatgpt"},
		{"chatgpt utm only", "", "utm_source=chatgpt.com", ChannelAIChat, "", "chatgpt"},
		{"openai utm word", "", "utm_source=openai", ChannelAIChat, "", "chatgpt"},
		{"perplexity", "https://www.perplexity.ai/", "", ChannelAIChat, "perplexity.ai", "perplexity"},
		{"perplexity utm", "", "utm_source=perplexity", ChannelAIChat, "", "perplexity"},
		{"claude", "https://claude.ai/", "", ChannelAIChat, "claude.ai", "claude"},
		{"gemini", "https://gemini.google.com/", "", ChannelAIChat, "gemini.google.com", "gemini"},
		{"copilot", "https://copilot.microsoft.com/", "", ChannelAIChat, "copilot.microsoft.com", "copilot"},
		{"you", "https://you.com/", "", ChannelAIChat, "you.com", "you"},
		{"phind", "https://www.phind.com/", "", ChannelAIChat, "phind.com", "phind"},
		{"meta ai", "https://www.meta.ai/", "", ChannelAIChat, "meta.ai", "meta-ai"},
		{"grok", "https://grok.com/", "", ChannelAIChat, "grok.com", "grok"},
		{"deepseek", "https://chat.deepseek.com/", "", ChannelAIChat, "chat.deepseek.com", "deepseek"},
		{"mistral", "https://chat.mistral.ai/", "", ChannelAIChat, "chat.mistral.ai", "mistral"},
		{"notebooklm", "https://notebooklm.google.com/", "", ChannelAIChat, "notebooklm.google.com", "notebooklm"},
		{"linkedin", "https://www.linkedin.com/", "", ChannelSocial, "linkedin.com", "linkedin"},
		{"lnkd", "https://lnkd.in/abc", "", ChannelSocial, "lnkd.in", "linkedin"},
		{"facebook mobile", "https://m.facebook.com/", "", ChannelSocial, "m.facebook.com", "facebook"},
		{"facebook link shim", "https://l.facebook.com/", "", ChannelSocial, "l.facebook.com", "facebook"},
		{"t.co", "https://t.co/xyz", "", ChannelSocial, "t.co", "x"},
		{"reddit", "https://old.reddit.com/r/x", "", ChannelSocial, "old.reddit.com", "reddit"},
		{"pinterest nl", "https://nl.pinterest.com/", "", ChannelSocial, "nl.pinterest.com", "pinterest"},
		{"youtube", "https://www.youtube.com/", "", ChannelSocial, "youtube.com", "youtube"},
		{"utm social", "", "utm_source=newsletter&utm_medium=social", ChannelSocial, "", ""},
		{"utm social with host", "https://www.linkedin.com/", "utm_medium=social", ChannelSocial, "linkedin.com", "linkedin"},
		{"utm organic", "https://www.google.nl/", "utm_medium=organic", ChannelSearch, "google.nl", "google"},
		{"utm campaign", "", "utm_source=mail&utm_medium=email&utm_campaign=launch", ChannelCampaign, "", ""},
		{"utm campaign beats referral", "https://www.utwente.nl/", "utm_campaign=course", ChannelCampaign, "utwente.nl", ""},
		{"referral", "https://www.utwente.nl/en/", "", ChannelReferral, "utwente.nl", ""},
		{"official site referral", "https://www.tribelt.nl/", "", ChannelReferral, "tribelt.nl", ""},
		{"lookalike host", "https://notgoogle.example/", "", ChannelReferral, "notgoogle.example", ""},
		{"x.com without www", "https://x.com/i/status/1", "", ChannelSocial, "x.com", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := url.ParseQuery(tc.query)
			got := ClassifyArrival(tc.referer, q, own)
			if got.Channel != tc.channel || got.ReferrerHost != tc.host || got.ReferrerName != tc.refName {
				t.Fatalf("ClassifyArrival(%q, %q) = %+v, want %s host=%q name=%q", tc.referer, tc.query, got, tc.channel, tc.host, tc.refName)
			}
		})
	}
}

func TestParseUTM(t *testing.T) {
	q := url.Values{"utm_source": {" chatgpt.com "}, "utm_medium": {"ai"}, "utm_campaign": {"c"}, "utm_term": {"t"}, "utm_content": {strings.Repeat("x", 300)}}
	u := ParseUTM(q)
	if u.Source != "chatgpt.com" || u.Medium != "ai" || u.Campaign != "c" || u.Term != "t" || len(u.Content) != 200 {
		t.Fatalf("%+v", u)
	}
}

func TestChannels(t *testing.T) {
	if len(Channels()) != 7 {
		t.Fatal("seven Arrival Channels")
	}
}

func TestClean(t *testing.T) {
	if got := Clean("a\x00b", 10); got != "ab" {
		t.Fatalf("NUL: %q", got)
	}
	if got := Clean("é", 1); got != "" {
		t.Fatalf("cut rune: %q", got)
	}
	if got := Clean("\xffok", 10); got != "ok" {
		t.Fatalf("invalid: %q", got)
	}
}
