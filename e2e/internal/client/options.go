package client

import (
	"encoding/json"
	"net/url"
	"strconv"
)

// ListOptions for listing torrents with filters, sorting, and pagination.
type ListOptions struct {
	Page    int
	Limit   int
	Sort    string
	Order   string
	Search  string
	Hashes  []string
	Filters *FilterOptions
}

// Encode converts ListOptions to URL query parameters.
func (o ListOptions) Encode() string {
	v := url.Values{}

	if o.Page > 0 {
		v.Set("page", strconv.Itoa(o.Page))
	}
	if o.Limit > 0 {
		v.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Sort != "" {
		v.Set("sort", o.Sort)
	}
	if o.Order != "" {
		v.Set("order", o.Order)
	}
	if o.Search != "" {
		v.Set("search", o.Search)
	}
	if len(o.Hashes) > 0 {
		for _, h := range o.Hashes {
			v.Add("hash", h)
		}
	}
	if o.Filters != nil {
		filterJSON, err := json.Marshal(o.Filters)
		if err == nil {
			v.Set("filters", string(filterJSON))
		}
	}

	return v.Encode()
}
