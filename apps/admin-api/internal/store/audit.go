package store

import "strings"

func auditResourceType(action string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		return ""
	}
	if i := strings.IndexByte(action, '.'); i > 0 {
		return action[:i]
	}
	return action
}

func legacyAuditEntry(e AuditEvent) AuditEntry {
	detail := ""
	if e.Metadata != nil {
		if v, ok := e.Metadata["detail"].(string); ok {
			detail = v
		}
	}
	actor := e.ActorEmail
	if actor == "" {
		actor = e.ActorUserID
	}
	return AuditEntry{
		ID:       e.ID,
		At:       e.At,
		Actor:    actor,
		Action:   e.Action,
		Resource: e.ResourceID,
		Detail:   detail,
	}
}

func isLegacyAuditEvent(e AuditEvent) bool {
	if e.Metadata != nil {
		if v, ok := e.Metadata["legacy_entry"].(bool); ok && v {
			return true
		}
		if v, ok := e.Metadata["legacy_table"].(string); ok && v == "audit_log" {
			return true
		}
	}
	return false
}
