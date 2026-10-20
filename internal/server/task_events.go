package server

import (
	"context"
	"database/sql"
	"fmt"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

func (s *Server) taskEvent(ctx context.Context, t *store.Task, kind, source string, extra map[string]any) core.CommitHook {
	if !s.core.Configured() {
		return nil
	}
	u, err := s.store.GetUser(ctx, t.UserID)
	switch kind {
	case "created", "updated", "completed", "deleted", "reminder":
	default:
		kind = "updated"
	}
	return func(tx *sql.Tx) error {
		if err != nil {
			return err
		}
		p := map[string]any{"id": t.ID, "title": t.Title, "list_id": t.ListID, "status": t.Status, "due": t.DueStr(), "start": t.StartStr(), "source": source, "user_id": t.UserID, "user_email": u.Email}
		for k, v := range extra {
			p[k] = v
		}
		return core.QueueEvent(ctx, tx, "tasks."+kind, fmt.Sprintf("task:%d", t.ID), p)
	}
}
