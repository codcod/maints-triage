package jira

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a minimal Jira REST API v3 client.
type Client struct {
	baseURL    string
	authHeader string
	http       *http.Client
}

// NewClient creates a Jira client authenticated with Basic Auth (email + API token).
func NewClient(baseURL, username, apiToken string) *Client {
	creds := base64.StdEncoding.EncodeToString([]byte(username + ":" + apiToken))
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		authHeader: "Basic " + creds,
		http:       &http.Client{Timeout: 30 * time.Second},
	}
}

// FieldMapping maps a human-readable field name to a dot-notation path in the
// Jira JSON response (e.g. "fields.customfield_20320.value").
type FieldMapping struct {
	Field string `json:"field"`
	Path  string `json:"path"`
}

// FieldValue is a resolved field name / value pair extracted via a FieldMapping.
type FieldValue struct {
	Field string
	Value string
}

// Issue holds the fields we extract from a Jira issue for triage.
type Issue struct {
	Key              string
	Summary          string
	Description      string
	Status           string
	Priority         string
	Reporter         string
	Assignee         string
	Components       []string
	AffectedVersions []string
	FixVersions      []string
	Labels           []string
	Comments         []Comment
	// ExtraFields contains values resolved from a FieldMapping slice, in mapping order.
	ExtraFields []FieldValue
}

type Comment struct {
	Author  string
	Created string
	Body    string
}

// issueResponse is the raw JSON structure returned by the Jira REST API.
type issueResponse struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string `json:"summary"`
		Description any    `json:"description"` // Atlassian Document Format (ADF) or plain string
		Status      struct {
			Name string `json:"name"`
		} `json:"status"`
		Priority struct {
			Name string `json:"name"`
		} `json:"priority"`
		Reporter struct {
			DisplayName  string `json:"displayName"`
			EmailAddress string `json:"emailAddress"`
		} `json:"reporter"`
		Assignee struct {
			DisplayName string `json:"displayName"`
		} `json:"assignee"`
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
		Versions []struct {
			Name string `json:"name"`
		} `json:"versions"`
		FixVersions []struct {
			Name string `json:"name"`
		} `json:"fixVersions"`
		Labels  []string `json:"labels"`
		Comment struct {
			Comments []struct {
				Author struct {
					DisplayName string `json:"displayName"`
				} `json:"author"`
				Created string `json:"created"`
				Body    any    `json:"body"` // ADF or plain string
			} `json:"comments"`
		} `json:"comment"`
	} `json:"fields"`
}

// FetchIssue retrieves a Jira issue by key, including all fields and comments.
// mappings is an optional list of extra fields to resolve from the raw JSON; if
// nil or empty the extra-fields section of the returned Issue will be empty.
func (c *Client) FetchIssue(key string, mappings []FieldMapping) (*Issue, error) {
	baseFields := "summary,description,status,priority,reporter,assignee,components,versions,fixVersions,labels,comment"
	if extra := customFieldIDs(mappings); len(extra) > 0 {
		baseFields += "," + strings.Join(extra, ",")
	}
	url := fmt.Sprintf("%s/rest/api/3/issue/%s?expand=renderedFields&fields=%s", c.baseURL, key, baseFields)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira API returned %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	var raw issueResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	issue := &Issue{
		Key:         raw.Key,
		Summary:     raw.Fields.Summary,
		Description: extractText(raw.Fields.Description),
		Status:      raw.Fields.Status.Name,
		Priority:    raw.Fields.Priority.Name,
		Reporter:    raw.Fields.Reporter.DisplayName,
		Assignee:    raw.Fields.Assignee.DisplayName,
		Labels:      raw.Fields.Labels,
	}

	for _, c := range raw.Fields.Components {
		issue.Components = append(issue.Components, c.Name)
	}
	for _, v := range raw.Fields.Versions {
		issue.AffectedVersions = append(issue.AffectedVersions, v.Name)
	}
	for _, v := range raw.Fields.FixVersions {
		issue.FixVersions = append(issue.FixVersions, v.Name)
	}
	for _, c := range raw.Fields.Comment.Comments {
		issue.Comments = append(issue.Comments, Comment{
			Author:  c.Author.DisplayName,
			Created: c.Created,
			Body:    extractText(c.Body),
		})
	}

	if len(mappings) > 0 {
		// Unmarshal into a generic map so we can navigate arbitrary paths.
		var rawMap map[string]any
		if err := json.Unmarshal(body, &rawMap); err == nil {
			for _, m := range mappings {
				issue.ExtraFields = append(issue.ExtraFields, FieldValue{
					Field: m.Field,
					Value: getByPath(rawMap, m.Path),
				})
			}
		}
	}

	return issue, nil
}

// customFieldIDs extracts unique Jira custom-field IDs (e.g. "customfield_20320")
// from the dot-notation paths in a mapping slice.
func customFieldIDs(mappings []FieldMapping) []string {
	seen := map[string]bool{}
	var ids []string
	for _, m := range mappings {
		for _, segment := range strings.Split(m.Path, ".") {
			if strings.HasPrefix(segment, "customfield_") && !seen[segment] {
				seen[segment] = true
				ids = append(ids, segment)
			}
		}
	}
	return ids
}

// getByPath walks a dot-separated path through a nested map[string]any.
// When a JSON array is encountered mid-walk the remaining path is applied to
// every element and the results are joined with ", " — this correctly handles
// multi-value Jira custom fields (e.g. customfield_20945 is an array of option
// objects, each with a "value" key).
func getByPath(data map[string]any, path string) string {
	return walkPath(data, strings.Split(path, "."))
}

func walkPath(current any, segments []string) string {
	if len(segments) == 0 {
		if current == nil {
			return ""
		}
		return fmt.Sprintf("%v", current)
	}
	// Array: apply remaining segments to every element, join non-empty results.
	if arr, ok := current.([]any); ok {
		var parts []string
		for _, elem := range arr {
			if v := walkPath(elem, segments); v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, ", ")
	}
	m, ok := current.(map[string]any)
	if !ok {
		return ""
	}
	return walkPath(m[segments[0]], segments[1:])
}

// extractText converts an Atlassian Document Format (ADF) node or plain string to plain text.
func extractText(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		return extractADF(val)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// extractADF recursively extracts plain text from an ADF document node.
func extractADF(node map[string]any) string {
	var sb strings.Builder
	if text, ok := node["text"].(string); ok {
		sb.WriteString(text)
	}
	if content, ok := node["content"].([]any); ok {
		for _, child := range content {
			if childMap, ok := child.(map[string]any); ok {
				nodeType, _ := childMap["type"].(string)
				sb.WriteString(extractADF(childMap))
				// Add newline after block-level nodes
				switch nodeType {
				case "paragraph", "heading", "bulletList", "orderedList", "listItem",
					"blockquote", "codeBlock", "rule", "panel":
					sb.WriteString("\n")
				}
			}
		}
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
