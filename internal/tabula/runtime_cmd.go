package tabula

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
)

type runtimeTokenIssueRecord struct {
	RuntimeID string `json:"runtime_id"`
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type runtimeTokenListRecord struct {
	RuntimeID  string `json:"runtime_id"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
	RevokedAt  string `json:"revoked_at,omitempty"`
}

func runtimeCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tabula runtime <token>")
		return 1
	}
	switch args[0] {
	case "token":
		return runtimeTokenCmd(args[1:], os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown runtime command %q\n", args[0])
		return 1
	}
}

func runtimeTokenCmd(args []string, stdout io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tabula runtime token <issue|list|revoke>")
		return 1
	}
	switch args[0] {
	case "issue":
		return runtimeTokenIssueCmd(args[1:], stdout)
	case "list":
		return runtimeTokenListCmd(args[1:], stdout)
	case "revoke":
		return runtimeTokenRevokeCmd(args[1:], stdout, nil)
	default:
		fmt.Fprintf(os.Stderr, "unknown runtime token command %q\n", args[0])
		return 1
	}
}

func runtimeTokenIssueCmd(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("runtime token issue", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	runtimeID := fs.String("runtime-id", "", "runtime id")
	expiresIn := fs.String("expires-in", "", "duration until expiry")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || strings.TrimSpace(*runtimeID) == "" {
		fmt.Fprintln(os.Stderr, "usage: tabula runtime token issue --runtime-id ID [--expires-in DURATION] [--json]")
		return 1
	}
	store, err := runtimeTokenStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "runtime_token_issue_failed: %v\n", err)
		return 1
	}
	now := time.Now().UTC()
	expiresAt := time.Time{}
	if strings.TrimSpace(*expiresIn) != "" {
		duration, err := time.ParseDuration(*expiresIn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "runtime_token_issue_failed: invalid --expires-in: %v\n", err)
			return 1
		}
		expiresAt = now.Add(duration).UTC()
	}
	record, err := store.Issue(*runtimeID, expiresAt, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runtime_token_issue_failed: %v\n", err)
		return 1
	}
	out := runtimeTokenIssueRecord{RuntimeID: record.RuntimeID, Token: record.Token, CreatedAt: formatStatusTime(record.CreatedAt), ExpiresAt: formatOptionalTime(record.ExpiresAt)}
	if *asJSON {
		data, _ := json.Marshal(out)
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintf(stdout, "runtime_id: %s\ntoken: %s\ncreated_at: %s\n", out.RuntimeID, out.Token, out.CreatedAt)
	if out.ExpiresAt != "" {
		fmt.Fprintf(stdout, "expires_at: %s\n", out.ExpiresAt)
	}
	return 0
}

func runtimeTokenListCmd(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("runtime token list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: tabula runtime token list [--json]")
		return 1
	}
	store, err := runtimeTokenStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "runtime_token_list_failed: %v\n", err)
		return 1
	}
	records := runtimeTokenListRecords(store.List())
	if *asJSON {
		data, _ := json.Marshal(records)
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, record := range records {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", record.RuntimeID, record.CreatedAt, record.ExpiresAt, record.RevokedAt)
	}
	_ = tw.Flush()
	return 0
}

func runtimeTokenRevokeCmd(args []string, stdout io.Writer, detach func(string)) int {
	fs := flag.NewFlagSet("runtime token revoke", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	runtimeID := fs.String("runtime-id", "", "runtime id")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || strings.TrimSpace(*runtimeID) == "" {
		fmt.Fprintln(os.Stderr, "usage: tabula runtime token revoke --runtime-id ID [--json]")
		return 1
	}
	store, err := runtimeTokenStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "runtime_token_revoke_failed: %v\n", err)
		return 1
	}
	if err := store.Revoke(*runtimeID, time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "runtime_token_revoke_failed: %v\n", err)
		return 1
	}
	if detach != nil {
		detach(*runtimeID)
	}
	if *asJSON {
		data, _ := json.Marshal(map[string]any{"runtime_id": strings.TrimSpace(*runtimeID), "revoked": true})
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintf(stdout, "runtime_id: %s\nrevoked: true\n", strings.TrimSpace(*runtimeID))
	return 0
}

func runtimeTokenStore() (*runtimeauth.FileStore, error) {
	tabulaHome, err := resolveTabulaHome()
	if err != nil {
		return nil, err
	}
	return runtimeauth.NewFileStore(runtimeauth.RuntimeTokenStorePath(tabulaHome))
}

func runtimeTokenListRecords(in []runtimeauth.TokenMetadata) []runtimeTokenListRecord {
	out := make([]runtimeTokenListRecord, 0, len(in))
	for _, item := range in {
		out = append(out, runtimeTokenListRecord{RuntimeID: item.RuntimeID, CreatedAt: formatStatusTime(item.CreatedAt), ExpiresAt: formatOptionalTime(item.ExpiresAt), LastSeenAt: formatOptionalTime(item.LastSeenAt), RevokedAt: formatOptionalTime(item.RevokedAt)})
	}
	return out
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return formatStatusTime(t)
}
