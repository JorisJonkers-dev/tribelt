package visits

import "testing"

func TestClassifyResource(t *testing.T) {
	cases := []struct {
		name, path, format string
		status             int
		want               Resource
		ok                 bool
	}{
		{"html page", "/sectoren", "html", 200, ResourcePage, true},
		{"home", "/", "html", 200, ResourcePage, true},
		{"not modified page", "/sectoren", "html", 304, ResourcePage, true},
		{"markdown suffix", "/sectoren.md", "md", 200, ResourceMarkdown, true},
		{"markdown by accept", "/sectoren", "md", 200, ResourceMarkdown, true},
		{"robots", "/robots.txt", "txt", 200, ResourceRobots, true},
		{"robots not modified", "/robots.txt", "txt", 304, ResourceRobots, true},
		{"llms", "/llms.txt", "txt", 200, ResourceLLMs, true},
		{"llms full", "/llms-full.txt", "txt", 200, ResourceLLMsFull, true},
		{"sitemap", "/sitemap.xml", "xml", 200, ResourceSitemap, true},
		{"other xml", "/sitemap_index.xml", "xml", 200, ResourceSitemap, true},
		{"image", "/images/home-960.webp", "img", 200, ResourceImage, true},
		{"image by path", "/images/logo.svg", "html", 200, ResourceImage, true},
		{"image by format", "/x.webp", "img", 200, ResourceImage, true},
		{"permanent redirect", "/transportbanden", "html", 301, ResourceRedirect, true},
		{"found", "/x", "html", 302, ResourceRedirect, true},
		{"temporary", "/x", "html", 307, ResourceRedirect, true},
		{"permanent 308", "/x", "html", 308, ResourceRedirect, true},
		{"trailing slash redirect", "/sectoren/", "html", 301, ResourceRedirect, true},
		{"not found", "/wp-login.php", "html", 404, ResourceNotFound, true},
		{"missing twin", "/nope.md", "html", 404, ResourceNotFound, true},
		{"missing image", "/images/nope.webp", "img", 404, ResourceNotFound, true},
		{"outbound", "/go", "html", 302, ResourceOutbound, true},
		{"beacon", "/b", "html", 204, "", false},
		{"static", "/static/stats.css", "html", 200, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ClassifyResource(tc.path, tc.format, tc.status)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("ClassifyResource(%q, %q, %d) = %q, %v; want %q, %v", tc.path, tc.format, tc.status, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestResourcesAreDistinct(t *testing.T) {
	seen := map[Resource]bool{}
	for _, r := range Resources() {
		if seen[r] || r == "" {
			t.Fatalf("duplicate or empty resource %q", r)
		}
		seen[r] = true
	}
	if len(seen) != 10 {
		t.Fatalf("want 10 resource classes, got %d", len(seen))
	}
}

func TestOperator(t *testing.T) {
	for bot, want := range map[string]string{
		"GPTBot": "OpenAI", "ChatGPT-User": "OpenAI", "OAI-SearchBot": "OpenAI", "ClaudeBot": "Anthropic",
		"Claude-User": "Anthropic", "anthropic-ai": "Anthropic", "PerplexityBot": "Perplexity", "Googlebot": "Google",
		"Google-InspectionTool": "Google", "Gemini-Deep-Research": "Google", "APIs-Google": "Google", "bingbot": "Microsoft",
		"Applebot": "Apple", "CCBot": "Common Crawl", "AhrefsBot": "Ahrefs", "meta-externalagent": "Meta",
		"curl": "", "": "", "ExampleBot": "",
	} {
		if got := Operator(bot); got != want {
			t.Errorf("Operator(%q) = %q, want %q", bot, got, want)
		}
	}
}
