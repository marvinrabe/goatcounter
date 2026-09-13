package handlers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/cron"
	"github.com/marvinrabe/goatcounter/internal/widgets"
	"zgo.at/errors"
	"zgo.at/guru"
	"zgo.at/zdb"
	"zgo.at/zstd/zbool"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/ztime"
)

const mcpProtocol = "2026-07-28"

type apiRequest struct {
	Action    string          `json:"action"`
	Arguments json.RawMessage `json:"arguments"`
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

var apiTools = []tool{
	{Name: "sites", Description: "List configured sites and their data metadata.", InputSchema: objectSchema(nil, nil)},
	{Name: "dashboard", Description: "Get the same aggregate data used by the dashboard widgets.", InputSchema: objectSchema(map[string]any{
		"site":   map[string]any{"type": "string", "description": "Configured site name; defaults to the first site."},
		"period": map[string]any{"type": "string", "description": "day, week, month, quarter, half-year, year, or a number of days."},
		"start":  map[string]any{"type": "string", "description": "Optional start date (YYYY-MM-DD or RFC3339)."},
		"end":    map[string]any{"type": "string", "description": "Optional end date (YYYY-MM-DD or RFC3339)."},
		"group":  map[string]any{"type": "string", "enum": []string{"hour", "day", "week", "month"}},
		"filter": map[string]any{"type": "string", "description": "Optional dashboard path filter."},
		"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
	}, nil)},
	{Name: "pageviews", Description: "Search pageview/event paths before deleting or merging them. SQL LIKE wildcards % and _ are supported.", InputSchema: objectSchema(map[string]any{
		"site": map[string]any{"type": "string"}, "search": map[string]any{"type": "string"},
		"match_case": map[string]any{"type": "boolean"},
	}, []string{"search"})},
	{Name: "delete_pageviews", Description: "Permanently delete pageviews and aggregate statistics for path IDs.", InputSchema: pathIDsSchema(false)},
	{Name: "merge_pageviews", Description: "Permanently merge source path IDs into one target path ID.", InputSchema: pathIDsSchema(true)},
	{Name: "import_raw", Description: "Import raw historical pageviews/events and update dashboard aggregates. Set replace=true to clear the sites present in the import first.", InputSchema: objectSchema(map[string]any{
		"replace": map[string]any{"type": "boolean"},
		"hits": map[string]any{"type": "array", "items": map[string]any{"type": "object", "required": []string{"site", "path"}, "properties": map[string]any{
			"site": map[string]any{"type": "string"}, "path": map[string]any{"type": "string"},
			"event": map[string]any{"type": "boolean"}, "ref": map[string]any{"type": "string"}, "ref_scheme": map[string]any{"type": "string", "enum": []string{"h", "g", "c", "o"}},
			"campaign": map[string]any{"type": "string"}, "browser": map[string]any{"type": "string"}, "browser_version": map[string]any{"type": "string"},
			"system": map[string]any{"type": "string"}, "system_version": map[string]any{"type": "string"}, "location": map[string]any{"type": "string"},
			"language": map[string]any{"type": "string"}, "width": map[string]any{"type": "integer"}, "first_visit": map[string]any{"type": "boolean"},
			"session":         map[string]any{"type": "string", "description": "32 hexadecimal characters."},
			"created_at":      map[string]any{"type": "string", "format": "date-time", "description": "RFC3339 time; use created_at_unix as an alternative."},
			"created_at_unix": map[string]any{"type": "integer", "description": "Unix timestamp in seconds; alternative to created_at."},
		}}},
	}, []string{"hits"})},
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	r := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		r["required"] = required
	}
	return r
}

func pathIDsSchema(merge bool) map[string]any {
	p := map[string]any{
		"site":     map[string]any{"type": "string"},
		"path_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "minItems": 1},
	}
	req := []string{"path_ids"}
	if merge {
		p["target_path_id"] = map[string]any{"type": "integer"}
		req = append(req, "target_path_id")
	}
	return objectSchema(p, req)
}

func (h backend) bearerAuth(next http.Handler) http.Handler {
	want := sha256.Sum256([]byte("Bearer " + h.apiToken))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		have := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(have[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="GoatCounter API"`)
			apiJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid or missing bearer token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h backend) api(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, r.Host) {
			apiJSON(w, http.StatusForbidden, map[string]any{"error": "origin does not match request host"})
			return
		}
	}

	if r.Method == http.MethodGet {
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			apiJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "this stateless MCP server does not expose an event stream"})
			return
		}
		apiJSON(w, http.StatusOK, map[string]any{
			"name": "GoatCounter API/MCP", "endpoint": "/api", "mcp_protocol": mcpProtocol,
			"request": map[string]any{"action": "sites", "arguments": map[string]any{}}, "actions": apiTools,
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		apiJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
		return
	}

	var probe struct {
		JSONRPC string `json:"jsonrpc"`
	}
	_ = json.Unmarshal(raw, &probe)
	if probe.JSONRPC == "2.0" {
		h.apiMCP(w, r, raw)
		return
	}

	var req apiRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.Action == "" {
		apiJSON(w, http.StatusBadRequest, map[string]any{"error": "request must contain action and optional arguments"})
		return
	}
	result, err := h.apiAction(r.Context(), req.Action, req.Arguments)
	if err != nil {
		status, msg := apiError(err)
		apiJSON(w, status, map[string]any{"error": msg})
		return
	}
	apiJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (h backend) apiMCP(w http.ResponseWriter, r *http.Request, raw json.RawMessage) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.Method == "" {
		apiRPCError(w, nil, -32600, "invalid JSON-RPC request")
		return
	}
	if v := r.Header.Get("MCP-Protocol-Version"); v == mcpProtocol {
		if r.Header.Get("Mcp-Method") != req.Method {
			apiRPCError(w, req.ID, -32020, "Mcp-Method header does not match request")
			return
		}
	}

	var result any
	switch req.Method {
	case "server/discover":
		result = map[string]any{"protocolVersion": mcpProtocol, "serverInfo": map[string]string{"name": "goatcounter", "version": goatcounter.Version}, "capabilities": map[string]any{"tools": map[string]any{}}}
	case "initialize": // Compatibility with pre-2026 MCP clients.
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-11-25"
		}
		result = map[string]any{"protocolVersion": p.ProtocolVersion, "serverInfo": map[string]string{"name": "goatcounter", "version": goatcounter.Version}, "capabilities": map[string]any{"tools": map[string]any{}}}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": apiTools, "ttlMs": 300000, "cacheScope": "public"}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			apiRPCError(w, req.ID, -32602, "tools/call requires a name and arguments")
			return
		}
		if v := r.Header.Get("MCP-Protocol-Version"); v == mcpProtocol && r.Header.Get("Mcp-Name") != p.Name {
			apiRPCError(w, req.ID, -32020, "Mcp-Name header does not match request")
			return
		}
		data, err := h.apiAction(r.Context(), p.Name, p.Arguments)
		if err != nil {
			_, msg := apiError(err)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": msg}}, "isError": true}
		} else {
			text, _ := json.Marshal(data)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}, "structuredContent": data}
		}
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
		return
	default:
		apiRPCError(w, req.ID, -32601, "method not found")
		return
	}

	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	apiJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func apiRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	apiJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
}

func apiJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
}

func apiError(err error) (int, string) {
	if code := guru.Code(err); code >= 400 && code <= 599 {
		return code, err.Error()
	}
	return http.StatusInternalServerError, err.Error()
}

func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return guru.Errorf(http.StatusBadRequest, "invalid arguments: %v", err)
	}
	return nil
}

func (h backend) apiAction(ctx context.Context, action string, raw json.RawMessage) (any, error) {
	switch action {
	case "sites":
		var args struct{}
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		return apiSites(ctx)
	case "dashboard":
		var args dashboardArgs
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		return h.apiDashboard(ctx, args)
	case "pageviews":
		var args struct {
			Site      string `json:"site"`
			Search    string `json:"search"`
			MatchCase bool   `json:"match_case"`
		}
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		if strings.TrimSpace(args.Search) == "" {
			return nil, guru.New(400, "search is required")
		}
		ctx, _, err := apiSiteContext(ctx, args.Site)
		if err != nil {
			return nil, err
		}
		var list goatcounter.HitLists
		if err := list.ListPathsLike(ctx, args.Search, args.MatchCase); err != nil {
			return nil, err
		}
		return map[string]any{"pageviews": list}, nil
	case "delete_pageviews":
		var args pathActionArgs
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		ctx, _, err := apiSiteContext(ctx, args.Site)
		if err != nil {
			return nil, err
		}
		if len(args.PathIDs) == 0 {
			return nil, guru.New(400, "path_ids must not be empty")
		}
		for _, id := range args.PathIDs {
			var p goatcounter.Path
			if err := p.ByID(ctx, id); err != nil {
				return nil, guru.Errorf(400, "unknown path_id %d", id)
			}
		}
		var hits goatcounter.Hits
		if err := hits.Purge(ctx, args.PathIDs); err != nil {
			return nil, err
		}
		return map[string]any{"deleted_path_ids": args.PathIDs}, nil
	case "merge_pageviews":
		var args pathActionArgs
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		ctx, _, err := apiSiteContext(ctx, args.Site)
		if err != nil {
			return nil, err
		}
		var target goatcounter.Path
		if args.TargetPathID == 0 || target.ByID(ctx, args.TargetPathID) != nil {
			return nil, guru.New(400, "target_path_id is missing or unknown")
		}
		args.PathIDs = slices.DeleteFunc(args.PathIDs, func(id goatcounter.PathID) bool { return id == target.ID })
		if len(args.PathIDs) == 0 {
			return nil, guru.New(400, "path_ids must include at least one path other than the target")
		}
		paths := make(goatcounter.Paths, len(args.PathIDs))
		for i, id := range args.PathIDs {
			if err := paths[i].ByID(ctx, id); err != nil {
				return nil, guru.Errorf(400, "unknown path_id %d", id)
			}
		}
		if err := target.Merge(ctx, paths); err != nil {
			return nil, err
		}
		return map[string]any{"target_path_id": target.ID, "merged_path_ids": args.PathIDs}, nil
	case "import_raw":
		var args importArgs
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		return apiImport(ctx, args)
	default:
		return nil, guru.Errorf(404, "unknown action %q", action)
	}
}

type pathActionArgs struct {
	Site         string               `json:"site"`
	PathIDs      []goatcounter.PathID `json:"path_ids"`
	TargetPathID goatcounter.PathID   `json:"target_path_id"`
}

func apiSiteContext(ctx context.Context, name string) (context.Context, goatcounter.Site, error) {
	if name == "" {
		name = goatcounter.Config(ctx).Sites[0].Key
	}
	site, ok := goatcounter.Config(ctx).Site(name)
	if !ok {
		return ctx, site, guru.Errorf(400, "unknown site %q", name)
	}
	site.Defaults()
	return goatcounter.WithSite(ctx, &site), site, nil
}

func apiSites(ctx context.Context) (any, error) {
	result := make([]map[string]any, 0, len(goatcounter.Config(ctx).Sites))
	for _, configured := range goatcounter.Config(ctx).Sites {
		ctx, site, err := apiSiteContext(ctx, configured.Key)
		if err != nil {
			return nil, err
		}
		var pageviews int
		if err := zdb.Get(ctx, &pageviews, `select count(*) from hits where site=?`, site.Key); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"site": site.Key, "link_domain": site.LinkDomain, "raw_pageviews": pageviews,
		})
	}
	return map[string]any{"sites": result, "timezone": goatcounter.Config(ctx).Timezone.String()}, nil
}

type dashboardArgs struct {
	Site   string `json:"site"`
	Period string `json:"period"`
	Start  string `json:"start"`
	End    string `json:"end"`
	Group  string `json:"group"`
	Filter string `json:"filter"`
	Limit  int    `json:"limit"`
}

func (h backend) apiDashboard(ctx context.Context, in dashboardArgs) (any, error) {
	ctx, site, err := apiSiteContext(ctx, in.Site)
	if err != nil {
		return nil, err
	}
	tz := goatcounter.Config(ctx).Timezone.Loc()
	period := in.Period
	if period == "" {
		period = defaultPeriod
	}
	rng := timeRange(ctx, period, tz, false)
	if in.Start != "" || in.End != "" {
		if in.Start == "" || in.End == "" {
			return nil, guru.New(400, "start and end must be supplied together")
		}
		start, err := apiTime(in.Start, tz, false)
		if err != nil {
			return nil, guru.Errorf(400, "invalid start: %v", err)
		}
		end, err := apiTime(in.End, tz, true)
		if err != nil {
			return nil, guru.Errorf(400, "invalid end: %v", err)
		}
		rng = ztime.NewRange(start.UTC()).To(end.UTC())
	}
	if rng.End.Before(rng.Start) {
		return nil, guru.New(400, "end must not be before start")
	}
	group := goatcounter.GroupDaily
	switch in.Group {
	case "", "day":
	case "hour":
		group = goatcounter.GroupHourly
	case "week":
		group = goatcounter.GroupWeekly
	case "month":
		group = goatcounter.GroupMonthly
	default:
		return nil, guru.New(400, "group must be hour, day, week, or month")
	}
	var filter goatcounter.PathFilter
	if in.Filter != "" {
		filter, err = goatcounter.PathFilterFromQuery(ctx, in.Filter)
		if err != nil {
			return nil, err
		}
	}
	limit := in.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 {
		return nil, guru.New(400, "limit must be between 1 and 1000")
	}
	args := widgets.NewArgs(ctx, rng, group, goatcounter.Groups{goatcounter.GroupHourly, goatcounter.GroupDaily, goatcounter.GroupWeekly, goatcounter.GroupMonthly}, 0)
	args.PathFilter = filter
	list := widgets.NewList(ctx)
	data := make(map[string]any, len(list))
	for _, w := range list {
		switch v := w.(type) {
		case *widgets.Pages:
			v.Limit, v.LimitRefs = limit, limit
			v.WithStats = true
		case *widgets.TopRefs:
			v.Limit = limit
		case *widgets.Campaigns:
			v.Limit = limit
		case *widgets.Browsers:
			v.Limit = limit
		case *widgets.Systems:
			v.Limit = limit
		case *widgets.Locations:
			v.Limit = limit
		case *widgets.Languages:
			v.Limit = limit
		}
		if _, err := w.GetData(ctx, args); err != nil {
			return nil, errors.Wrapf(err, "dashboard %s", w.Name())
		}
		switch v := w.(type) {
		case *widgets.TotalCount:
			data[w.Name()] = map[string]any{
				"visits": v.Total, "events": v.TotalEvents, "visits_utc": v.TotalUTC,
				"pageviews": v.Metrics.Pageviews, "bounce_rate": v.Metrics.BounceRate,
				"views_per_visit": v.Metrics.ViewsPerVisit(), "visit_duration_seconds": v.Metrics.VisitDurationSeconds,
			}
		case *widgets.TotalPages:
			data[w.Name()] = v.Total
		case *widgets.Pages:
			data[w.Name()] = map[string]any{"items": v.Pages, "display": v.Display, "more": v.More}
		case *widgets.TopRefs:
			data[w.Name()] = v.TopRefs
		case *widgets.Campaigns:
			data[w.Name()] = v.Stats
		case *widgets.Browsers:
			data[w.Name()] = v.Stats
		case *widgets.Systems:
			data[w.Name()] = v.Stats
		case *widgets.Locations:
			data[w.Name()] = v.Stats
		case *widgets.Languages:
			data[w.Name()] = v.Stats
		case *widgets.Sizes:
			data[w.Name()] = v.Stats
		}
	}
	return map[string]any{"site": site.Key, "start": rng.Start, "end": rng.End, "group": group.String(), "data": data}, nil
}

func apiTime(v string, loc *time.Location, end bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation("2006-01-02", v, loc)
	if err != nil {
		return time.Time{}, err
	}
	if end {
		t = t.AddDate(0, 0, 1).Add(-time.Nanosecond)
	}
	return t, nil
}

type importHit struct {
	Site           string `json:"site"`
	Path           string `json:"path"`
	Event          bool   `json:"event"`
	Ref            string `json:"ref"`
	RefScheme      string `json:"ref_scheme"`
	Campaign       string `json:"campaign"`
	Browser        string `json:"browser"`
	BrowserVersion string `json:"browser_version"`
	System         string `json:"system"`
	SystemVersion  string `json:"system_version"`
	Location       string `json:"location"`
	Language       string `json:"language"`
	Width          *int16 `json:"width"`
	FirstVisit     bool   `json:"first_visit"`
	Session        string `json:"session"`
	CreatedAt      string `json:"created_at"`
	CreatedAtUnix  int64  `json:"created_at_unix"`
}

type importArgs struct {
	Replace bool        `json:"replace"`
	Hits    []importHit `json:"hits"`
}

func apiImport(ctx context.Context, in importArgs) (any, error) {
	if len(in.Hits) == 0 {
		return nil, guru.New(400, "hits must not be empty")
	}
	if len(in.Hits) > 100000 {
		return nil, guru.New(400, "at most 100000 hits can be imported in one request")
	}

	sites := make(map[string]goatcounter.Site)
	for i, raw := range in.Hits {
		_, site, err := apiSiteContext(ctx, raw.Site)
		if err != nil {
			return nil, guru.Errorf(400, "hits[%d]: %v", i, err)
		}
		if raw.Path == "" || (raw.CreatedAt == "" && raw.CreatedAtUnix == 0) {
			return nil, guru.Errorf(400, "hits[%d]: path and created_at or created_at_unix are required", i)
		}
		if raw.CreatedAt != "" {
			if _, err := time.Parse(time.RFC3339, raw.CreatedAt); err != nil {
				return nil, guru.Errorf(400, "hits[%d].created_at: %v", i, err)
			}
		}
		if raw.RefScheme != "" && !slices.Contains([]string{"h", "g", "c", "o"}, raw.RefScheme) {
			return nil, guru.Errorf(400, "hits[%d].ref_scheme is invalid", i)
		}
		if _, err := parseSession(raw.Session); err != nil {
			return nil, guru.Errorf(400, "hits[%d].session: %v", i, err)
		}
		sites[site.Key] = site
	}

	counts := make(map[string]int)
	err := zdb.TX(ctx, func(txctx context.Context) error {
		txctx = goatcounter.NewBatchCache(txctx)
		if in.Replace {
			for _, site := range sites {
				sctx := goatcounter.WithSite(txctx, &site)
				if err := site.DeleteAll(sctx); err != nil {
					return err
				}
			}
		}

		ins, err := zdb.NewBulkInsert(txctx, "hits", []string{"site", "path_id", "ref_id", "browser_id", "system_id", "width", "location", "language", "created_at", "session", "first_visit", "campaign"})
		if err != nil {
			return err
		}
		bySite := make(map[string][]goatcounter.Hit)
		campaigns := make(map[string]goatcounter.CampaignID)
		for i, raw := range in.Hits {
			site := sites[raw.Site]
			if site.Key == "" { // apiSiteContext permits case-insensitive names.
				_, site, _ = apiSiteContext(txctx, raw.Site)
			}
			sctx := goatcounter.WithSite(txctx, &site)
			if raw.Path == "" || (raw.CreatedAt == "" && raw.CreatedAtUnix == 0) {
				return guru.Errorf(400, "hits[%d]: path and created_at or created_at_unix are required", i)
			}
			var created time.Time
			if raw.CreatedAt != "" {
				created, err = time.Parse(time.RFC3339, raw.CreatedAt)
				if err != nil {
					return guru.Errorf(400, "hits[%d].created_at: %v", i, err)
				}
			} else {
				created = time.Unix(raw.CreatedAtUnix, 0)
			}
			if created.After(time.Now().Add(5 * time.Second)) {
				return guru.Errorf(400, "hits[%d].created_at is in the future", i)
			}
			path := goatcounter.Path{Path: raw.Path, Event: zbool.Bool(raw.Event)}
			if err := path.GetOrInsert(sctx); err != nil {
				return guru.Errorf(400, "hits[%d].path: %v", i, err)
			}
			scheme := raw.RefScheme
			if scheme == "" {
				if raw.Ref == "" {
					scheme = goatcounter.RefSchemeOther
				} else {
					scheme = goatcounter.RefSchemeHTTP
				}
			}
			if !slices.Contains([]string{"h", "g", "c", "o"}, scheme) {
				return guru.Errorf(400, "hits[%d].ref_scheme is invalid", i)
			}
			ref := goatcounter.Ref{Ref: raw.Ref, RefScheme: scheme}
			if err := ref.GetOrInsert(sctx); err != nil {
				return guru.Errorf(400, "hits[%d].ref: %v", i, err)
			}
			var browser goatcounter.Browser
			if raw.Browser != "" {
				if err := browser.GetOrInsert(sctx, raw.Browser, raw.BrowserVersion); err != nil {
					return err
				}
			}
			var system goatcounter.System
			if raw.System != "" {
				if err := system.GetOrInsert(sctx, raw.System, raw.SystemVersion); err != nil {
					return err
				}
			}
			var campaignID *goatcounter.CampaignID
			if raw.Campaign != "" {
				id, ok := campaigns[strings.ToLower(raw.Campaign)]
				if !ok {
					campaign := goatcounter.Campaign{}
					err := campaign.ByName(sctx, raw.Campaign)
					if zdb.ErrNoRows(err) {
						campaign.Name = raw.Campaign
						err = campaign.Insert(sctx)
					}
					if err != nil {
						return err
					}
					id = campaign.ID
					campaigns[strings.ToLower(raw.Campaign)] = id
				}
				campaignID = &id
			}
			session, err := parseSession(raw.Session)
			if err != nil {
				return guru.Errorf(400, "hits[%d].session: %v", i, err)
			}
			var language *string
			if raw.Language != "" {
				language = &raw.Language
			}
			hit := goatcounter.Hit{Site: site.Key, PathID: path.ID, RefID: ref.ID, BrowserID: browser.ID, SystemID: system.ID,
				CampaignID: campaignID, Session: session, Width: raw.Width, Location: raw.Location, Language: language,
				FirstVisit: zbool.Bool(raw.FirstVisit), CreatedAt: created.UTC().Round(time.Second)}
			ins.Values(hit.Site, hit.PathID, hit.RefID, hit.BrowserID, hit.SystemID, hit.Width, hit.Location, hit.Language,
				hit.CreatedAt, hit.Session, hit.FirstVisit, hit.CampaignID)
			bySite[site.Key] = append(bySite[site.Key], hit)
			counts[site.Key]++
		}
		if err := ins.Finish(); err != nil {
			return err
		}
		for key, hits := range bySite {
			site := sites[key]
			if err := cron.UpdateStats(goatcounter.WithSite(txctx, &site), hits); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"imported": len(in.Hits), "sites": counts, "replaced": in.Replace}, nil
}

func parseSession(s string) (zint.Uint128, error) {
	if s == "" {
		return zint.Uint128{}, nil
	}
	b, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(b) != 16 {
		return zint.Uint128{}, fmt.Errorf("must be 32 hexadecimal characters")
	}
	return zint.Uint128{
		uint64FromBytes(b[:8]), uint64FromBytes(b[8:]),
	}, nil
}

func uint64FromBytes(b []byte) uint64 {
	v, _ := strconv.ParseUint(hex.EncodeToString(b), 16, 64)
	return v
}
