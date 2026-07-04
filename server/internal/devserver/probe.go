package devserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/openrtc/openrtc/server/internal/cluster"
	runtimeapp "github.com/openrtc/openrtc/server/internal/runtime"
)

const (
	defaultProbeTimeout = 5 * time.Second
	probeRestartNone    = "none"
	probeRestartRuntime = "runtime"
	probeRestartAdmin   = "admin"
	probeRestartBoth    = "both"
)

var probeHTTPClient = func() *http.Client {
	return &http.Client{}
}

type probeOptions struct {
	baseURL           string
	room              string
	restart           string
	jsonOutput        bool
	timeout           time.Duration
	afterSequence     uint64
	limit             int
	expectSeedRoom    bool
	expectSeedStorage bool
}

type devProbeResult struct {
	OK        bool              `json:"ok"`
	Room      string            `json:"room"`
	Checks    []devProbeCheck   `json:"checks"`
	Snapshots devProbeSnapshots `json:"snapshots"`
}

type devProbeCheck struct {
	Name    string      `json:"name"`
	OK      bool        `json:"ok"`
	Message string      `json:"message"`
	Detail  interface{} `json:"detail,omitempty"`
}

type devProbeSnapshots struct {
	Config         *devClientConfigSnapshot           `json:"config,omitempty"`
	Status         *devStatusSnapshot                 `json:"status,omitempty"`
	Connections    *devConnectionsSnapshot            `json:"connections,omitempty"`
	Sockets        *runtimeapp.DevConnectionsSnapshot `json:"sockets,omitempty"`
	Storage        *devStorageSnapshot                `json:"storage,omitempty"`
	YJS            *devYJSSnapshot                    `json:"yjs,omitempty"`
	Events         *devEventsSnapshot                 `json:"events,omitempty"`
	RuntimeRestart *devRestartSnapshot                `json:"runtimeRestart,omitempty"`
	AdminRestart   *devRestartSnapshot                `json:"adminRestart,omitempty"`
}

type devConnectionsSnapshot struct {
	Room        string               `json:"room"`
	Connections []cluster.ActiveUser `json:"connections"`
}

func probeMain(args []string, stdout io.Writer, stderr io.Writer) int {
	output := stderr
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			output = stdout
			break
		}
	}
	opts, err := parseProbeOptions(args, output)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	result, err := runProbe(ctx, opts)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "openrtc dev probe: %v\n", err)
		return 1
	}
	if err := writeProbeResult(stdout, result, opts.jsonOutput); err != nil {
		_, _ = fmt.Fprintf(stderr, "openrtc dev probe: %v\n", err)
		return 1
	}
	if !result.OK {
		return 1
	}
	return 0
}

func parseProbeOptions(args []string, output io.Writer) (probeOptions, error) {
	opts := probeOptions{
		baseURL:           envOr("OPENRTC_DEV_BASE_URL", "http://127.0.0.1:3000"),
		restart:           probeRestartNone,
		timeout:           defaultProbeTimeout,
		expectSeedRoom:    true,
		expectSeedStorage: true,
	}
	flags := flag.NewFlagSet("openrtc dev probe", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.baseURL, "base-url", opts.baseURL, "base URL for a running openrtc dev app")
	flags.StringVar(&opts.room, "room", "", "room to probe; defaults to the first advertised seed room")
	flags.StringVar(&opts.restart, "restart", opts.restart, "optional restart drill: none, runtime, admin, or both")
	flags.BoolVar(&opts.jsonOutput, "json", false, "write the full probe report as JSON")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "overall probe timeout")
	flags.Uint64Var(&opts.afterSequence, "after-seq", 0, "event-log sequence lower bound")
	flags.IntVar(&opts.limit, "limit", 20, "event-log limit")
	flags.BoolVar(&opts.expectSeedRoom, "expect-seed-room", opts.expectSeedRoom, "require the selected room to be advertised as a seed room")
	flags.BoolVar(&opts.expectSeedStorage, "expect-seed-storage", opts.expectSeedStorage, "require the selected room to have seeded storage")
	if err := flags.Parse(args); err != nil {
		return probeOptions{}, err
	}
	if flags.NArg() > 0 {
		return probeOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if opts.timeout <= 0 {
		return probeOptions{}, fmt.Errorf("timeout must be positive")
	}
	if opts.limit <= 0 || opts.limit > devEventsMax {
		return probeOptions{}, fmt.Errorf("limit must be between 1 and %d", devEventsMax)
	}
	switch opts.restart {
	case probeRestartNone, probeRestartRuntime, probeRestartAdmin, probeRestartBoth:
	default:
		return probeOptions{}, fmt.Errorf("restart must be none, runtime, admin, or both")
	}
	if _, err := url.ParseRequestURI(opts.baseURL); err != nil {
		return probeOptions{}, fmt.Errorf("base-url must be absolute: %w", err)
	}
	return opts, nil
}

func runProbe(ctx context.Context, opts probeOptions) (devProbeResult, error) {
	client := probeHTTPClient()
	var result devProbeResult

	configURL := resolveProbeURL(opts.baseURL, "/dev/config", nil)
	var config devClientConfigSnapshot
	if err := probeGetJSON(ctx, client, configURL, nil, &config); err != nil {
		return result, fmt.Errorf("fetch dev config: %w", err)
	}
	result.Snapshots.Config = &config
	result.Checks = append(result.Checks, probeCheck(
		"config",
		config.PublicKey != "" && config.TokenURL != "" && config.WSURL != "" && config.AdminURL != "",
		"Dev config advertises auth, runtime, and admin endpoints",
		map[string]interface{}{
			"publicKey": config.PublicKey,
			"wsURL":     config.WSURL,
			"adminURL":  config.AdminURL,
			"seedRooms": config.SeedRooms,
		},
	))

	room := strings.TrimSpace(opts.room)
	if room == "" {
		room = firstSeedRoom(config.SeedRooms)
	}
	result.Room = room
	result.Checks = append(result.Checks, probeCheck(
		"seed-room",
		room != "" && (!opts.expectSeedRoom || containsString(config.SeedRooms, room)),
		probeSeedRoomMessage(room, opts.expectSeedRoom),
		map[string]interface{}{"room": room, "seedRooms": config.SeedRooms},
	))
	if room == "" {
		result.OK = false
		return result, nil
	}

	captureProbeCheck(&result, "status", func() devProbeCheck {
		var snapshot devStatusSnapshot
		if err := probeGetJSON(ctx, client, endpointURL(opts.baseURL, config.StatusURL, "/dev/status", nil), map[int]bool{http.StatusServiceUnavailable: true}, &snapshot); err != nil {
			return probeErrorCheck("status", err)
		}
		result.Snapshots.Status = &snapshot
		ok := snapshot.Status == "ok" && snapshot.Redis.Healthy && snapshot.Runtime.Running && snapshot.Admin.Running
		if opts.expectSeedStorage {
			ok = ok && seedStorageFound(snapshot.SeedRooms, room)
		}
		return probeCheck(
			"status",
			ok,
			probeStatusMessage(ok),
			map[string]interface{}{
				"status":         snapshot.Status,
				"storageBackend": snapshot.StorageBackend,
				"redisHealthy":   snapshot.Redis.Healthy,
				"runtimeRunning": snapshot.Runtime.Running,
				"adminRunning":   snapshot.Admin.Running,
			},
		)
	})

	captureProbeCheck(&result, "connections", func() devProbeCheck {
		var snapshot devConnectionsSnapshot
		if err := probeGetJSON(ctx, client, endpointURL(opts.baseURL, config.ConnectionsURL, "/dev/connections", map[string]string{"room": room}), nil, &snapshot); err != nil {
			return probeErrorCheck("connections", err)
		}
		result.Snapshots.Connections = &snapshot
		return probeCheck("connections", snapshot.Room == room, "Dev active-user inspection endpoint is reachable", map[string]interface{}{
			"room":  snapshot.Room,
			"count": len(snapshot.Connections),
		})
	})

	captureProbeCheck(&result, "sockets", func() devProbeCheck {
		var snapshot runtimeapp.DevConnectionsSnapshot
		if err := probeGetJSON(ctx, client, endpointURL(opts.baseURL, config.SocketsURL, "/dev/sockets", map[string]string{"room": room}), nil, &snapshot); err != nil {
			return probeErrorCheck("sockets", err)
		}
		result.Snapshots.Sockets = &snapshot
		return probeCheck("sockets", true, probeSocketsMessage(snapshot, room), map[string]interface{}{
			"activeSockets":   len(snapshot.Connections) + len(snapshot.YJSConnections),
			"activeRoomCount": snapshot.ActiveRoomCount,
			"roomInSockets":   socketSnapshotHasRoom(snapshot, room),
		})
	})

	captureProbeCheck(&result, "storage", func() devProbeCheck {
		var snapshot devStorageSnapshot
		if err := probeGetJSON(ctx, client, endpointURL(opts.baseURL, config.StorageURL, "/dev/storage", map[string]string{"room": room}), nil, &snapshot); err != nil {
			return probeErrorCheck("storage", err)
		}
		result.Snapshots.Storage = &snapshot
		found := snapshot.Durable.Found || (snapshot.Runtime != nil && snapshot.Runtime.Found)
		return probeCheck("storage", snapshot.Room == room && (!opts.expectSeedStorage || found), probeStorageMessage(opts.expectSeedStorage), map[string]interface{}{
			"room":         snapshot.Room,
			"durableFound": snapshot.Durable.Found,
			"runtimeFound": snapshot.Runtime != nil && snapshot.Runtime.Found,
		})
	})

	captureProbeCheck(&result, "yjs", func() devProbeCheck {
		var snapshot devYJSSnapshot
		if err := probeGetJSON(ctx, client, endpointURL(opts.baseURL, config.YJSInspectionURL, "/dev/yjs", map[string]string{"room": room}), nil, &snapshot); err != nil {
			return probeErrorCheck("yjs", err)
		}
		result.Snapshots.YJS = &snapshot
		return probeCheck("yjs", snapshot.Room == room, "Dev Yjs inspection endpoint is reachable", map[string]interface{}{
			"room":         snapshot.Room,
			"durableFound": snapshot.Durable.Found,
			"updates":      snapshot.Durable.UpdateCount,
			"snapshotHash": snapshot.Durable.SnapshotHash,
		})
	})

	captureProbeCheck(&result, "events", func() devProbeCheck {
		var snapshot devEventsSnapshot
		query := map[string]string{"room": room, "limit": strconv.Itoa(opts.limit)}
		if opts.afterSequence > 0 {
			query["after_seq"] = strconv.FormatUint(opts.afterSequence, 10)
		}
		if err := probeGetJSON(ctx, client, endpointURL(opts.baseURL, config.EventsURL, "/dev/events", query), nil, &snapshot); err != nil {
			return probeErrorCheck("events", err)
		}
		result.Snapshots.Events = &snapshot
		return probeCheck("events", snapshot.Room == room, "Dev event-log inspection endpoint is reachable", map[string]interface{}{
			"room":          snapshot.Room,
			"afterSequence": snapshot.AfterSequence,
			"limit":         snapshot.Limit,
			"count":         len(snapshot.Events),
		})
	})

	if opts.restart == probeRestartRuntime || opts.restart == probeRestartBoth {
		captureProbeCheck(&result, "restart-runtime", func() devProbeCheck {
			var snapshot devRestartSnapshot
			if err := probePostJSON(ctx, client, endpointURL(opts.baseURL, config.CrashRuntimeURL, "/dev/crash/runtime", nil), &snapshot); err != nil {
				return probeErrorCheck("restart-runtime", err)
			}
			result.Snapshots.RuntimeRestart = &snapshot
			return probeCheck("restart-runtime", snapshot.Service == "runtime" && snapshot.ServiceStatus.Running, "Runtime restart drill completed", map[string]interface{}{"generation": snapshot.ServiceStatus.Generation})
		})
	}
	if opts.restart == probeRestartAdmin || opts.restart == probeRestartBoth {
		captureProbeCheck(&result, "restart-admin", func() devProbeCheck {
			var snapshot devRestartSnapshot
			if err := probePostJSON(ctx, client, endpointURL(opts.baseURL, config.CrashAdminURL, "/dev/crash/admin", nil), &snapshot); err != nil {
				return probeErrorCheck("restart-admin", err)
			}
			result.Snapshots.AdminRestart = &snapshot
			return probeCheck("restart-admin", snapshot.Service == "admin" && snapshot.ServiceStatus.Running, "Admin restart drill completed", map[string]interface{}{"generation": snapshot.ServiceStatus.Generation})
		})
	}

	result.OK = true
	for _, check := range result.Checks {
		if !check.OK {
			result.OK = false
			break
		}
	}
	return result, nil
}

func writeProbeResult(w io.Writer, result devProbeResult, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	status := "ok"
	if !result.OK {
		status = "failed"
	}
	if _, err := fmt.Fprintf(w, "OpenRTC dev probe %s for %s\n", status, result.Room); err != nil {
		return err
	}
	for _, check := range result.Checks {
		marker := "ok"
		if !check.OK {
			marker = "fail"
		}
		if _, err := fmt.Fprintf(w, "- [%s] %s: %s\n", marker, check.Name, check.Message); err != nil {
			return err
		}
	}
	return nil
}

func probeGetJSON(ctx context.Context, client *http.Client, rawURL string, allowedStatuses map[int]bool, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	return doProbeJSON(client, req, allowedStatuses, out)
}

func probePostJSON(ctx context.Context, client *http.Client, rawURL string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, nil)
	if err != nil {
		return err
	}
	return doProbeJSON(client, req, nil, out)
}

func doProbeJSON(client *http.Client, req *http.Request, allowedStatuses map[int]bool, out interface{}) error {
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if !allowedStatuses[res.StatusCode] {
			return fmt.Errorf("%s %s returned %d: %s", req.Method, req.URL.String(), res.StatusCode, strings.TrimSpace(string(body)))
		}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("%s %s returned empty JSON", req.Method, req.URL.String())
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s %s returned invalid JSON: %w", req.Method, req.URL.String(), err)
	}
	return nil
}

func resolveProbeURL(baseURL string, path string, query map[string]string) string {
	return endpointURL(baseURL, "", path, query)
}

func endpointURL(baseURL string, advertisedURL string, fallbackPath string, query map[string]string) string {
	raw := advertisedURL
	if raw == "" {
		raw = fallbackPath
	}
	parsed := mustResolveURL(baseURL, raw)
	values := parsed.Query()
	for key, value := range query {
		values.Set(key, value)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func mustResolveURL(baseURL string, raw string) *url.URL {
	normalizedBase := baseURL
	if !strings.HasSuffix(normalizedBase, "/") {
		normalizedBase += "/"
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return &url.URL{Scheme: "http", Host: "invalid.local", Path: "/invalid-url"}
	}
	base, err := url.Parse(normalizedBase)
	if err != nil {
		return parsed
	}
	return base.ResolveReference(parsed)
}

func captureProbeCheck(result *devProbeResult, name string, run func() devProbeCheck) {
	check := run()
	if check.Name == "" {
		check.Name = name
	}
	result.Checks = append(result.Checks, check)
}

func probeCheck(name string, ok bool, message string, detail interface{}) devProbeCheck {
	return devProbeCheck{Name: name, OK: ok, Message: message, Detail: detail}
}

func probeErrorCheck(name string, err error) devProbeCheck {
	return devProbeCheck{Name: name, OK: false, Message: err.Error(), Detail: map[string]string{"error": err.Error()}}
}

func probeSeedRoomMessage(room string, expect bool) string {
	if expect {
		if room == "" {
			return "No dev room selected"
		}
		return fmt.Sprintf("Dev room %s is advertised as a seed room", room)
	}
	return fmt.Sprintf("Dev room %s selected", room)
}

func probeStatusMessage(ok bool) string {
	if ok {
		return "Dev stack status is ok"
	}
	return "Dev stack status is degraded"
}

func probeSocketsMessage(snapshot runtimeapp.DevConnectionsSnapshot, room string) string {
	if socketSnapshotHasRoom(snapshot, room) {
		return "Dev socket inspection sees the selected room"
	}
	return "Dev socket inspection endpoint is reachable"
}

func probeStorageMessage(expectSeedStorage bool) string {
	if expectSeedStorage {
		return "Seeded room storage is available"
	}
	return "Dev storage inspection endpoint is reachable"
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func seedStorageFound(rooms []devSeedRoomStatus, room string) bool {
	for _, candidate := range rooms {
		if candidate.Room == room {
			return candidate.Exists && candidate.StorageFound && candidate.Error == ""
		}
	}
	return false
}

func socketSnapshotHasRoom(snapshot runtimeapp.DevConnectionsSnapshot, room string) bool {
	for _, connection := range snapshot.Connections {
		if containsString(connection.Rooms, room) {
			return true
		}
	}
	for _, connection := range snapshot.YJSConnections {
		if connection.Room == room {
			return true
		}
	}
	return false
}
