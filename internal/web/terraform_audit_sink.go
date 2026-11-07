package web

import (
	"context"

	"pantau/internal/logger"
)

// StartTerraformAuditSink persists the redacted `terraform` log events of the
// default broadcaster so the audit trail survives restarts.
func (s *Server) StartTerraformAuditSink(ctx context.Context) {
	if s.db == nil {
		return
	}
	// Subscribe before spawning so no event emitted after this call is missed.
	ch, unsub := logger.DefaultBroadcaster().Subscribe()
	go func() {
		defer unsub()
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-ch:
				if evt.Module == "terraform" {
					_ = s.db.AppendTFAudit(evt.Timestamp, evt.Level, evt.Host, evt.Message)
				}
			}
		}
	}()
}
