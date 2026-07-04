package devserver

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTokenTimeout = 5 * time.Second
	tokenAccessWildcard = "wildcard"
	tokenAccessGrants   = "grants"
)

type tokenOptions struct {
	baseURL    string
	tokenURL   string
	publicKey  string
	kind       string
	username   string
	tenant     string
	room       string
	groups     string
	access     string
	scope      string
	jsonOutput bool
	envOutput  bool
	timeout    time.Duration
}

type devTokenResponse struct {
	Token     string                   `json:"token"`
	Kind      string                   `json:"kind"`
	Username  string                   `json:"username"`
	Tenant    string                   `json:"tenant"`
	Groups    []string                 `json:"groups"`
	ExpiresAt string                   `json:"expiresAt"`
	Room      string                   `json:"room,omitempty"`
	Config    *devClientConfigSnapshot `json:"config,omitempty"`
}

func tokenMain(args []string, stdout io.Writer, stderr io.Writer) int {
	output := stderr
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			output = stdout
			break
		}
	}
	opts, err := parseTokenOptions(args, output)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	response, err := runToken(ctx, opts)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "openrtc dev token: %v\n", err)
		return 1
	}
	if err := writeTokenResult(stdout, response, opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "openrtc dev token: %v\n", err)
		return 1
	}
	return 0
}

func parseTokenOptions(args []string, output io.Writer) (tokenOptions, error) {
	opts := tokenOptions{
		baseURL:   envOr("OPENRTC_DEV_BASE_URL", "http://127.0.0.1:3000"),
		tokenURL:  envOr("OPENRTC_DEV_TOKEN_URL", "/dev/token"),
		publicKey: envOr("OPENRTC_DEV_PUBLIC_KEY", localPublicKey),
		kind:      "client",
		tenant:    "demo",
		access:    tokenAccessWildcard,
		timeout:   defaultTokenTimeout,
	}
	flags := flag.NewFlagSet("openrtc dev token", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.baseURL, "base-url", opts.baseURL, "base URL for a running openrtc dev app")
	flags.StringVar(&opts.tokenURL, "token-url", opts.tokenURL, "token endpoint path or absolute URL")
	flags.StringVar(&opts.publicKey, "public-key", opts.publicKey, "local dev public key; empty requires --username")
	flags.StringVar(&opts.kind, "kind", opts.kind, "token kind: client or admin")
	flags.StringVar(&opts.username, "username", "", "subject username; defaults to anonymous local dev user")
	flags.StringVar(&opts.tenant, "tenant", opts.tenant, "tenant claim")
	flags.StringVar(&opts.room, "room", "", "room to include in the token response")
	flags.StringVar(&opts.groups, "groups", "", "comma-separated group IDs")
	flags.StringVar(&opts.access, "access", opts.access, "client access mode: wildcard or grants")
	flags.StringVar(&opts.scope, "scope", "", "admin scope override")
	flags.BoolVar(&opts.jsonOutput, "json", false, "write the full token response as JSON")
	flags.BoolVar(&opts.envOutput, "env", false, "write shell-safe OPENRTC_DEV_* environment assignments")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "overall token request timeout")
	if err := flags.Parse(args); err != nil {
		return tokenOptions{}, err
	}
	if flags.NArg() > 0 {
		return tokenOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if opts.timeout <= 0 {
		return tokenOptions{}, fmt.Errorf("timeout must be positive")
	}
	if opts.jsonOutput && opts.envOutput {
		return tokenOptions{}, fmt.Errorf("json and env output are mutually exclusive")
	}
	if opts.kind != "client" && opts.kind != "admin" {
		return tokenOptions{}, fmt.Errorf("kind must be client or admin")
	}
	if opts.access != tokenAccessWildcard && opts.access != tokenAccessGrants {
		return tokenOptions{}, fmt.Errorf("access must be wildcard or grants")
	}
	if opts.username == "" && opts.publicKey == "" {
		return tokenOptions{}, fmt.Errorf("username is required when public-key is empty")
	}
	if _, err := url.ParseRequestURI(opts.baseURL); err != nil {
		return tokenOptions{}, fmt.Errorf("base-url must be absolute: %w", err)
	}
	return opts, nil
}

func runToken(ctx context.Context, opts tokenOptions) (devTokenResponse, error) {
	client := probeHTTPClient()
	var response devTokenResponse
	if err := probeGetJSON(ctx, client, devTokenURL(opts), nil, &response); err != nil {
		return response, fmt.Errorf("fetch dev token: %w", err)
	}
	if response.Token == "" {
		return response, fmt.Errorf("fetch dev token: response missing token")
	}
	return response, nil
}

func devTokenURL(opts tokenOptions) string {
	query := map[string]string{
		"kind":   opts.kind,
		"tenant": opts.tenant,
	}
	if opts.publicKey != "" {
		query["pubkey"] = opts.publicKey
	}
	if opts.username != "" {
		query["username"] = opts.username
	}
	if opts.room != "" {
		query["room"] = opts.room
	}
	if opts.groups != "" {
		query["groups"] = opts.groups
	}
	if opts.kind == "client" && opts.access == tokenAccessGrants {
		query["access"] = tokenAccessGrants
	}
	if opts.kind == "admin" && opts.scope != "" {
		query["scope"] = opts.scope
	}
	return endpointURL(opts.baseURL, opts.tokenURL, "/dev/token", query)
}

func writeTokenResult(w io.Writer, response devTokenResponse, opts tokenOptions) error {
	if opts.jsonOutput {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(response)
	}
	if opts.envOutput {
		return writeTokenEnv(w, response)
	}
	_, err := fmt.Fprintln(w, response.Token)
	return err
}

func writeTokenEnv(w io.Writer, response devTokenResponse) error {
	lines := [][2]string{
		{"OPENRTC_DEV_TOKEN", response.Token},
		{"OPENRTC_DEV_KIND", response.Kind},
		{"OPENRTC_DEV_USERNAME", response.Username},
		{"OPENRTC_DEV_TENANT", response.Tenant},
		{"OPENRTC_DEV_EXPIRES_AT", response.ExpiresAt},
	}
	if response.Room != "" {
		lines = append(lines, [2]string{"OPENRTC_DEV_ROOM", response.Room})
	}
	if response.Config != nil {
		lines = append(lines,
			[2]string{"OPENRTC_DEV_PUBLIC_KEY", response.Config.PublicKey},
			[2]string{"OPENRTC_DEV_TOKEN_URL", response.Config.TokenURL},
			[2]string{"OPENRTC_DEV_WS_URL", response.Config.WSURL},
			[2]string{"OPENRTC_DEV_YJS_URL", response.Config.YJSURL},
			[2]string{"OPENRTC_DEV_ADMIN_URL", response.Config.AdminURL},
			[2]string{"OPENRTC_DEV_RUNTIME_URL", response.Config.RuntimeURL},
		)
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "%s=%s\n", line[0], shellQuoteEnv(line[1])); err != nil {
			return err
		}
	}
	return nil
}

func shellQuoteEnv(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
