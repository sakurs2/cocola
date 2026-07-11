package service

import (
	"context"
	"time"
)

// StartConversationTraceMaintenance expires only detailed spans and repairs
// runs left open by a process crash. Audit summaries remain queryable.
func (a *Admin) StartConversationTraceMaintenance(ctx context.Context) {
	run := func() {
		now := a.now().UTC()
		retentionDays := a.settingInt(ctx, SettingTraceRetentionDays, 30)
		_, _ = a.store.ExpireConversationTraceSpans(ctx, now.AddDate(0, 0, -retentionDays))
		_, _ = a.store.InterruptStaleConversationRuns(ctx, now.Add(-2*time.Hour), now)
	}
	run()
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
