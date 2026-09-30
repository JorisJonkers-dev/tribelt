// Command update-ranges refreshes internal/visits/ranges from the crawler operators' published lists.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func sources() map[string]string {
	return map[string]string{
		"google-googlebot.json":                      "https://developers.google.com/static/search/apis/ipranges/googlebot.json",
		"google-special-crawlers.json":               "https://developers.google.com/static/search/apis/ipranges/special-crawlers.json",
		"google-user-triggered-fetchers.json":        "https://developers.google.com/static/search/apis/ipranges/user-triggered-fetchers.json",
		"google-user-triggered-fetchers-google.json": "https://developers.google.com/static/search/apis/ipranges/user-triggered-fetchers-google.json",
		"bing-bingbot.json":                          "https://www.bing.com/toolbox/bingbot.json",
		"openai-gptbot.json":                         "https://openai.com/gptbot.json",
		"openai-searchbot.json":                      "https://openai.com/searchbot.json",
		"openai-chatgpt-user.json":                   "https://openai.com/chatgpt-user.json",
		"anthropic-bots.json":                        "https://claude.com/crawling/bots.json",
		"perplexity-perplexitybot.json":              "https://www.perplexity.ai/perplexitybot.json",
		"perplexity-perplexity-user.json":            "https://www.perplexity.ai/perplexity-user.json",
		"commoncrawl-ccbot.json":                     "https://index.commoncrawl.org/ccbot.json",
		"apple-applebot.json":                        "https://search.developer.apple.com/applebot.json",
	}
}

func main() {
	dir := "internal/visits/ranges"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	client := &http.Client{Timeout: 30 * time.Second}
	failed := false
	for file, url := range sources() {
		n, err := fetch(client, url, filepath.Join(dir, file))
		if err != nil {
			failed = true
			fmt.Fprintf(os.Stderr, "FAIL %s: %v (kept the previous file)\n", file, err)
			continue
		}
		fmt.Printf("ok   %s: %d prefixes\n", file, n)
	}
	if failed {
		os.Exit(1)
	}
}

func fetch(client *http.Client, url, out string) (int, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "tribelt-update-ranges (+https://tribelt.jorisjonkers.dev)")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, err
	}
	var doc struct {
		Prefixes []map[string]string `json:"prefixes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0, err
	}
	if len(doc.Prefixes) == 0 {
		return 0, fmt.Errorf("no prefixes")
	}
	return len(doc.Prefixes), os.WriteFile(out, raw, 0o644) //nolint:gosec // developer command writing tracked source files
}
