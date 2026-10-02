package analytics

import "strings"

// PathFilter limits dashboard data to paths matching the filter query from
// the dashboard's filter box.
//
// The query is matched case-insensitively anywhere in the path, and may
// contain these keywords:
//
//	at:start      match only at the start of the path
//	at:end        match only at the end of the path
//	is:event      only events
//	is:pageview   only pageviews
//	:not          invert the match
type PathFilter struct {
	query        string
	like         string
	not          bool
	onlyEvent    bool
	onlyPageview bool
}

func (p PathFilter) Empty() bool { return p.query == "" }

// AllPageviews reports if the filter matches every pageview, like "is:pageview"
// alone. Pageview statistics are then the same as without a filter.
func (p PathFilter) AllPageviews() bool {
	return p.Empty() || (p.like == "%%" && !p.not && !p.onlyEvent)
}
func (p PathFilter) String() string { return p.query }

// NewPathFilter parses a filter query.
func NewPathFilter(query string) PathFilter {
	query = strings.TrimSpace(query)
	if query == "" {
		return PathFilter{}
	}
	like, kw := findFilter(query, "at:start", "at:end", "is:event", "is:pageview", "in:path", ":not")
	p := PathFilter{query: query}
	like = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(like)
	var atStart, atEnd bool
	for _, f := range kw {
		switch f {
		case "at:start":
			atStart = true
		case "at:end":
			atEnd = true
		case "is:event":
			p.onlyEvent = true
		case "is:pageview":
			p.onlyPageview = true
		case ":not":
			p.not = true
		}
	}
	if !atStart {
		like = "%" + like
	}
	if !atEnd {
		like += "%"
	}
	p.like = like
	return p
}

// SQL returns a condition on the given path column, and the event name
// column if it's not empty, to put in a query. The column names must be
// constants, never user input; the filter itself is the :filter_like
// parameter.
func (p PathFilter) SQL(pathCol, nameCol string) (string, map[string]any) {
	if p.Empty() {
		return "1=1", map[string]any{}
	}
	cond := pathCol + ` like :filter_like escape '\'`
	if p.not {
		cond = "not (" + cond + ")"
	}
	if nameCol != "" {
		if p.onlyEvent {
			cond += " and " + nameCol + " <> 'pageview'"
		}
		if p.onlyPageview {
			cond += " and " + nameCol + " = 'pageview'"
		}
	} else if p.onlyEvent {
		cond = "0=1"
	}
	return cond, map[string]any{"filter_like": p.like}
}

func findFilter(filter string, find ...string) (string, []string) {
	found := make([]string, 0, 2)
	for _, f := range find {
		if i := strings.Index(filter, f); i > -1 {
			filter = strings.TrimSpace(filter[:i] + filter[i+len(f):])
			found = append(found, f)
		}
	}
	return filter, found
}
