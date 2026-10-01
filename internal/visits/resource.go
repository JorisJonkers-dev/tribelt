package visits

import "strings"

// Resource is what a Hit fetched: a Mirror Page, its Markdown twin, an agent-facing file, an image,
// a redirect or a miss.
type Resource string

const (
	ResourcePage     Resource = "page"
	ResourceMarkdown Resource = "markdown"
	ResourceRobots   Resource = "robots"
	ResourceLLMs     Resource = "llms"
	ResourceLLMsFull Resource = "llms_full"
	ResourceSitemap  Resource = "sitemap"
	ResourceImage    Resource = "image"
	ResourceRedirect Resource = "redirect"
	ResourceNotFound Resource = "not_found"
	ResourceOutbound Resource = "outbound"
)

// Resources lists every resource class in reporting order.
func Resources() []Resource {
	return []Resource{
		ResourcePage, ResourceMarkdown, ResourceRobots, ResourceLLMs, ResourceLLMsFull, ResourceSitemap,
		ResourceImage, ResourceRedirect, ResourceNotFound, ResourceOutbound,
	}
}

// ClassifyResource derives the resource class from the request path, the served format and the
// response status. ok is false for requests that are never Hits: the beacon and static assets.
func ClassifyResource(path, format string, status int) (r Resource, ok bool) {
	switch {
	case path == "/b" || strings.HasPrefix(path, "/static/"):
		return "", false
	case path == "/go":
		return ResourceOutbound, true
	case status == 301 || status == 302 || status == 307 || status == 308:
		return ResourceRedirect, true
	case status == 404:
		return ResourceNotFound, true
	case format == "md":
		return ResourceMarkdown, true
	case path == "/robots.txt":
		return ResourceRobots, true
	case path == "/llms.txt":
		return ResourceLLMs, true
	case path == "/llms-full.txt":
		return ResourceLLMsFull, true
	case path == "/sitemap.xml" || format == "xml":
		return ResourceSitemap, true
	case strings.HasPrefix(path, "/images/") || format == "img":
		return ResourceImage, true
	}
	return ResourcePage, true
}
