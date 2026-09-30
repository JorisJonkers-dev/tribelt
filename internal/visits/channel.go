package visits

import (
	"net/url"
	"strings"
)

// Channel is the Arrival Channel of a human Hit.
type Channel string

const (
	ChannelSearch   Channel = "search"
	ChannelAIChat   Channel = "ai-chat"
	ChannelSocial   Channel = "social"
	ChannelCampaign Channel = "campaign"
	ChannelReferral Channel = "referral"
	ChannelDirect   Channel = "direct"
	ChannelInternal Channel = "internal"
)

// Channels lists every Arrival Channel in reporting order.
func Channels() []Channel {
	return []Channel{ChannelSearch, ChannelAIChat, ChannelSocial, ChannelCampaign, ChannelReferral, ChannelDirect, ChannelInternal}
}

// UTM holds the campaign parameters of a request.
type UTM struct {
	Source, Medium, Campaign, Term, Content string
}

func (u UTM) empty() bool {
	return u.Source == "" && u.Medium == "" && u.Campaign == "" && u.Term == "" && u.Content == ""
}

// Arrival describes where a request came from. Only the referrer's host is kept.
type Arrival struct {
	Channel      Channel
	ReferrerHost string
	// ReferrerName is the search engine, AI assistant or social network, when recognised.
	ReferrerName string
	UTM          UTM
}

// ParseUTM reads the utm_* parameters, trimmed and capped.
func ParseUTM(q url.Values) UTM {
	get := func(k string) string { return clip(strings.TrimSpace(q.Get(k)), 200) }
	return UTM{Source: get("utm_source"), Medium: get("utm_medium"), Campaign: get("utm_campaign"), Term: get("utm_term"), Content: get("utm_content")}
}

// ClassifyArrival derives the Arrival Channel from the Referer header, the query and this site's host.
func ClassifyArrival(referer string, q url.Values, ownHost string) Arrival {
	a := Arrival{UTM: ParseUTM(q)}
	refURL, _ := url.Parse(strings.TrimSpace(referer))
	var refPath string
	if refURL != nil && refURL.Host != "" {
		a.ReferrerHost = clip(strings.TrimPrefix(strings.ToLower(refURL.Hostname()), "www."), 253)
		refPath = strings.ToLower(refURL.Path)
	}
	if a.ReferrerHost != "" && a.ReferrerHost == strings.TrimPrefix(strings.ToLower(ownHost), "www.") {
		a.Channel = ChannelInternal
		return a
	}
	if name, ok := aiChat(strings.ToLower(a.UTM.Source), ""); ok {
		a.Channel, a.ReferrerName = ChannelAIChat, name
		return a
	}
	if a.ReferrerHost != "" {
		if name, ok := aiChat(a.ReferrerHost, refPath); ok {
			a.Channel, a.ReferrerName = ChannelAIChat, name
			return a
		}
	}
	switch medium := strings.ToLower(a.UTM.Medium); {
	case medium == "organic":
		a.Channel = ChannelSearch
		a.ReferrerName, _ = searchEngine(a.ReferrerHost)
		return a
	case medium == "social" || medium == "social-network" || medium == "social-media" || medium == "sm":
		a.Channel = ChannelSocial
		a.ReferrerName, _ = social(a.ReferrerHost)
		return a
	case !a.UTM.empty():
		a.Channel = ChannelCampaign
		return a
	}
	if a.ReferrerHost == "" {
		a.Channel = ChannelDirect
		return a
	}
	if name, ok := searchEngine(a.ReferrerHost); ok {
		a.Channel, a.ReferrerName = ChannelSearch, name
		return a
	}
	if name, ok := social(a.ReferrerHost); ok {
		a.Channel, a.ReferrerName = ChannelSocial, name
		return a
	}
	a.Channel = ChannelReferral
	return a
}

type hostRule struct {
	match string // host or registrable-domain suffix; a trailing "." matches any TLD
	name  string
}

func aiChatHosts() []hostRule {
	return []hostRule{
		{"chatgpt.com", "chatgpt"},
		{"chat.openai.com", "chatgpt"},
		{"openai.com", "chatgpt"},
		{"openai", "chatgpt"},
		{"perplexity.ai", "perplexity"},
		{"perplexity", "perplexity"},
		{"claude.ai", "claude"},
		{"claude.com", "claude"},
		{"claude", "claude"},
		{"gemini.google.com", "gemini"},
		{"bard.google.com", "gemini"},
		{"gemini", "gemini"},
		{"copilot.microsoft.com", "copilot"},
		{"copilot.cloud.microsoft", "copilot"},
		{"copilot", "copilot"},
		{"you.com", "you"},
		{"phind.com", "phind"},
		{"meta.ai", "meta-ai"},
		{"grok.com", "grok"},
		{"x.ai", "grok"},
		{"chat.deepseek.com", "deepseek"},
		{"deepseek.com", "deepseek"},
		{"chat.mistral.ai", "mistral"},
		{"poe.com", "poe"},
		{"kagi.com/assistant", "kagi-assistant"},
		{"duck.ai", "duck-ai"},
		{"notebooklm.google.com", "notebooklm"},
		{"chat.qwen.ai", "qwen"},
		{"kimi.com", "kimi"},
		{"kimi.moonshot.cn", "kimi"},
	}
}

// aiChat matches an AI assistant by host (with optional path) or by a utm_source value.
func aiChat(host, refPath string) (string, bool) {
	if host == "" {
		return "", false
	}
	if (host == "bing.com" || strings.HasSuffix(host, ".bing.com")) && strings.HasPrefix(refPath, "/chat") {
		return "copilot", true
	}
	for _, r := range aiChatHosts() {
		h, p, hasPath := strings.Cut(r.match, "/")
		if hasPath {
			if host == h && strings.HasPrefix(refPath, "/"+p) {
				return r.name, true
			}
			continue
		}
		if matchHost(host, h) {
			return r.name, true
		}
	}
	return "", false
}

func searchEngines() []hostRule {
	return []hostRule{
		{"google.", "google"},
		{"bing.com", "bing"},
		{"duckduckgo.com", "duckduckgo"},
		{"search.yahoo.", "yahoo"},
		{"yahoo.", "yahoo"},
		{"ecosia.org", "ecosia"},
		{"yandex.", "yandex"},
		{"ya.ru", "yandex"},
		{"baidu.com", "baidu"},
		{"qwant.com", "qwant"},
		{"startpage.com", "startpage"},
		{"search.brave.com", "brave"},
		{"ask.com", "ask"},
		{"aol.", "aol"},
		{"seznam.cz", "seznam"},
		{"naver.com", "naver"},
		{"sogou.com", "sogou"},
		{"mojeek.com", "mojeek"},
		{"kagi.com", "kagi"},
		{"so.com", "so"},
		{"search.lilo.org", "lilo"},
		{"metager.org", "metager"},
		{"metager.de", "metager"},
	}
}

func searchEngine(host string) (string, bool) {
	for _, r := range searchEngines() {
		if matchHost(host, r.match) {
			return r.name, true
		}
	}
	return "", false
}

func socialNetworks() []hostRule {
	return []hostRule{
		{"facebook.com", "facebook"},
		{"fb.com", "facebook"},
		{"fb.me", "facebook"},
		{"messenger.com", "facebook"},
		{"t.co", "x"},
		{"twitter.com", "x"},
		{"x.com", "x"},
		{"linkedin.com", "linkedin"},
		{"lnkd.in", "linkedin"},
		{"instagram.com", "instagram"},
		{"youtube.com", "youtube"},
		{"youtu.be", "youtube"},
		{"reddit.com", "reddit"},
		{"pinterest.", "pinterest"},
		{"tiktok.com", "tiktok"},
		{"whatsapp.com", "whatsapp"},
		{"wa.me", "whatsapp"},
		{"t.me", "telegram"},
		{"telegram.org", "telegram"},
		{"threads.net", "threads"},
		{"threads.com", "threads"},
		{"bsky.app", "bluesky"},
		{"mastodon.social", "mastodon"},
		{"xing.com", "xing"},
		{"news.ycombinator.com", "hacker-news"},
		{"discord.com", "discord"},
		{"slack.com", "slack"},
	}
}

func social(host string) (string, bool) {
	for _, r := range socialNetworks() {
		if matchHost(host, r.match) {
			return r.name, true
		}
	}
	return "", false
}

// matchHost matches exact hosts and subdomains; a rule without a dot matches a utm_source word,
// and a rule ending in "." matches the name under any TLD (google.nl, google.co.uk).
func matchHost(host, rule string) bool {
	switch {
	case !strings.Contains(rule, "."):
		return host == rule
	case strings.HasSuffix(rule, "."):
		return strings.HasPrefix(host, rule) || strings.Contains(host, "."+rule)
	default:
		return host == rule || strings.HasSuffix(host, "."+rule)
	}
}

func clip(s string, n int) string { return Clean(s, n) }

// Clean makes s safe to store: valid UTF-8, no NUL bytes, at most n bytes.
func Clean(s string, n int) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if len(s) > n {
		s = s[:n]
	}
	return strings.ToValidUTF8(s, "")
}
