package sandbox

import (
	"context"
	"fmt"
	"time"
)

// HeartbeatRenewalError 保存失败续租的原始身份；不将未知结果当作可重新续租。
type HeartbeatRenewalError struct {
	SessionID   string
	WorkspaceID string
	Cause       error
}

func (e *HeartbeatRenewalError) Error() string {
	return fmt.Sprintf("sandbox heartbeat stopped session=%s workspace=%s: %v", e.SessionID, e.WorkspaceID, e.Cause)
}

func (e *HeartbeatRenewalError) Unwrap() error { return e.Cause }

// HeartbeatError 返回停止自动续租的首个错误；Open/Resume 不清除它或重启心跳。
func (sb *Sandbox) HeartbeatError() error {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.heartbeatErr
}

func (sb *Sandbox) stopHeartbeatWithError(sessionID, workspaceID string, cause error) {
	failure := &HeartbeatRenewalError{SessionID: sessionID, WorkspaceID: workspaceID, Cause: cause}
	sb.mu.Lock()
	if sb.heartbeatErr == nil {
		sb.heartbeatErr = failure
	}
	sb.mu.Unlock()
	sb.logf("heartbeat stopped session=%s workspace=%s err=%v", sessionID, workspaceID, cause)
}

func (sb *Sandbox) heartbeatLoop(ctx context.Context) {
	defer sb.wg.Done()
	ticker := time.NewTicker(sb.opts.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sid, wid := sb.SessionID(), sb.WorkspaceID()
			renewCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			session, err := sb.client.RenewSession(renewCtx, sid, sb.opts.heartbeatExtend)
			cancel()
			if err == nil && (session == nil || session.SessionID != sid || session.WorkspaceID != wid) {
				err = fmt.Errorf("renewal receipt does not match the original session and workspace")
			}
			if err != nil {
				// 定时器的下一次 tick 也是新的写请求；失败后必须由调用方只读核对。
				sb.stopHeartbeatWithError(sid, wid, err)
				return
			}
		}
	}
}
