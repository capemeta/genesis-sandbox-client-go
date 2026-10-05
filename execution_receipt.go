package sandbox

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/capemeta/genesis-sandbox-client-go/internal/genapi"
)

func (c *Client) cancelExecReceipt(ctx context.Context, session, exec string) (*ExecRecord, error) {
	if err := validateLookupKey(session, 128); err != nil {
		return nil, err
	}
	if err := validateLookupKey(exec, 128); err != nil {
		return nil, err
	}
	var wire genapi.ExecRecord
	path := fmt.Sprintf("/v1/sessions/%s/execs/%s:cancel", url.PathEscape(session), url.PathEscape(exec))
	if err := c.request(ctx, http.MethodPost, path, nil, &wire); err != nil {
		return nil, err
	}
	record := fromGenExecRecord(wire)
	if err := validateExecReceipt(record, session, exec, ""); err != nil {
		return nil, err
	}
	return record, nil
}

func validateExecReceipt(record *ExecRecord, session, exec, operation string) error {
	if record == nil || record.ExecID == "" || record.OperationID == "" || record.SessionID != session || (exec != "" && record.ExecID != exec) || (operation != "" && record.OperationID != operation) {
		return fmt.Errorf("execution receipt original identity mismatch")
	}
	switch record.Status {
	case "queued", "running", "succeeded", "failed", "cancelled", "timed_out", "interrupted":
	default:
		return fmt.Errorf("unknown execution receipt status")
	}
	if record.StopConfirmed && (record.Status == "queued" || record.Status == "running") {
		return fmt.Errorf("nonterminal execution cannot confirm physical stop")
	}
	return nil
}

func checkedExecReceipt(record *ExecRecord, session, exec, operation string) (*ExecRecord, error) {
	if err := validateExecReceipt(record, session, exec, operation); err != nil {
		return nil, err
	}
	return record, nil
}

func validateSessionLookup(session *Session, operation string) (*Session, error) {
	if session == nil || session.SessionID == "" || session.IdempotencyKey != operation {
		return nil, fmt.Errorf("session lookup original identity mismatch")
	}
	return session, nil
}

func validateExecPageRequest(session string, query ExecListQuery) error {
	if err := validateLookupKey(session, 128); err != nil {
		return err
	}
	if query.Limit < 0 || query.Limit > 100 {
		return fmt.Errorf("execution page limit must be 1..100 or omitted")
	}
	if query.Cursor != "" {
		return validateLookupKey(query.Cursor, 4096)
	}
	return nil
}

func validateExecPage(page *ExecRecordList, session string, query ExecListQuery) (*ExecRecordList, error) {
	limit := query.Limit
	if limit == 0 {
		limit = 50
	}
	if page == nil || page.Total < len(page.Items) || len(page.Items) > limit {
		return nil, fmt.Errorf("invalid execution history page")
	}
	seen := map[string]bool{}
	for i := range page.Items {
		record := &page.Items[i]
		if err := validateExecReceipt(record, session, "", ""); err != nil {
			return nil, err
		}
		if seen[record.ExecID] {
			return nil, fmt.Errorf("duplicate execution history identity")
		}
		seen[record.ExecID] = true
	}
	return page, nil
}
