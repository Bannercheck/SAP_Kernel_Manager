package swdc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Result is one catalogue entry (an archive).
type Result struct {
	Title       string         `json:"title"`       // SAPEXE_403-80007807.SAR
	Description string         `json:"description"` // SAP KERNEL 7.93 64-BIT UNICODE ...
	Info        string         `json:"info"`        // extra text (info type, platform, package)
	Fastkey     string         `json:"fastkey"`
	Link        string         `json:"link"` // direct download link
	SizeKB      int64          `json:"size_kb"`
	Date        string         `json:"date"`
	Raw         map[string]any `json:"-"`
}

// Search runs a free-text query against the Software Center catalogue.
func (s *Session) Search(ctx context.Context, query string) ([]Result, error) {
	if err := s.Login(ctx); err != nil {
		return nil, err
	}
	q := url.Values{"SEARCH_MAX_RESULT": {"500"}, "RESULT_PER_PAGE": {"500"}, "SEARCH_STRING": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Launchpad+searchPath+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	s.Log("search %q → %d (%d bytes)", query, resp.StatusCode, len(b))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search %q: HTTP %s", query, resp.Status)
	}
	results, err := ParseSearch(b)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	return results, nil
}

// ParseSearch reads the OData answer without depending on exact field
// names: the well-known ones are mapped, everything stays in Raw.
func ParseSearch(b []byte) ([]Result, error) {
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("not JSON: %w", err)
	}
	rows := findResults(doc)
	if rows == nil {
		return nil, fmt.Errorf("no result list in the answer")
	}
	var out []Result
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		r := Result{Raw: m}
		var infos []string
		for k, v := range m {
			str := fmt.Sprint(v)
			if v == nil || str == "" || str == "<nil>" {
				continue
			}
			switch lk := strings.ToLower(k); {
			case lk == "title" || lk == "filename" || lk == "file_name":
				r.Title = str
			case lk == "description" || lk == "desc":
				r.Description = str
			case lk == "fastkey" || lk == "objectkey":
				r.Fastkey = str
			case strings.Contains(lk, "directlink") || lk == "downloadlink" || lk == "link" || lk == "url":
				r.Link = str
			case strings.Contains(lk, "filesize") || lk == "size":
				r.SizeKB = parseSizeKB(str)
			case strings.Contains(lk, "date"):
				if r.Date == "" {
					r.Date = str
				}
			case lk == "__metadata":
			case strings.HasPrefix(lk, "info") || strings.Contains(lk, "platform") || strings.Contains(lk, "component") ||
				strings.Contains(lk, "package") || strings.Contains(lk, "category") || strings.Contains(lk, "type"):
				infos = append(infos, str)
			}
		}
		r.Info = strings.Join(infos, " | ")
		if r.Link == "" && r.Fastkey != "" {
			r.Link = "https://softwaredownloads.sap.com/file/" + r.Fastkey
		}
		if r.Title != "" {
			out = append(out, r)
		}
	}
	return out, nil
}

// findResults locates the array of rows in OData V2 ({"d":{"results":[...]}}),
// OData V4 ({"value":[...]}) or a bare array.
func findResults(doc any) []any {
	switch v := doc.(type) {
	case []any:
		return v
	case map[string]any:
		for _, key := range []string{"results", "value", "d"} {
			inner, ok := v[key]
			if !ok {
				continue
			}
			if inner == nil { // "results": null = an empty list
				return []any{}
			}
			if rows := findResults(inner); rows != nil {
				return rows
			}
		}
	}
	return nil
}

func parseSizeKB(s string) int64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "GB"):
		mult = 1 << 20
		s = strings.TrimSpace(strings.TrimSuffix(s, "GB"))
	case strings.HasSuffix(s, "MB"):
		mult = 1 << 10
		s = strings.TrimSpace(strings.TrimSuffix(s, "MB"))
	case strings.HasSuffix(s, "KB"):
		s = strings.TrimSpace(strings.TrimSuffix(s, "KB"))
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	if err != nil {
		return 0
	}
	return int64(f * float64(mult))
}
