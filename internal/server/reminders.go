package server

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-tasks/internal/ical"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

// reminderLoop sends alarms whose time has come and the morning digest.
func (s *Server) reminderLoop(ctx context.Context) {
	for _, uid := range s.store.UserIDs(ctx) {
		s.rescheduleAll(ctx, uid)
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		s.fireAlarms(ctx)
		s.digests(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// rescheduleAll recomputes every alarm time of a person (zone or day-start changed).
func (s *Server) rescheduleAll(ctx context.Context, uid int64) {
	ts, err := s.store.TasksWithAlarms(ctx, uid)
	if err != nil {
		return
	}
	for _, t := range ts {
		s.scheduleAlarms(ctx, t)
	}
}

func (s *Server) fireAlarms(ctx context.Context) {
	due, err := s.store.DueAlarms(ctx, time.Now())
	if err != nil {
		return
	}
	// Reminders of a list that groups its notifications go out together, one per list and person.
	type bucket struct {
		uid    int64
		list   *store.List
		titles []string
	}
	var buckets []*bucket
	lists := map[[2]int64]*store.List{}
	listOf := func(uid, id int64) *store.List {
		k := [2]int64{uid, id}
		if l, ok := lists[k]; ok {
			return l
		}
		l, _ := s.store.GetList(ctx, uid, id)
		lists[k] = l
		return l
	}
	for _, a := range due {
		// anything older than a day is stale (the box was off): mark, don't send
		if at, err := time.Parse(time.RFC3339, a.FireAt); err == nil && time.Since(at) > 24*time.Hour {
			_ = s.store.MarkAlarmFired(ctx, a.ID)
			continue
		}
		t, err := s.store.GetTask(ctx, a.UserID, a.TaskID)
		if err != nil || !t.Open() {
			_ = s.store.MarkAlarmFired(ctx, a.ID)
			continue
		}
		if err := s.store.MarkAlarmFired(ctx, a.ID, s.taskEvent(ctx, t, "reminder", "alarm", map[string]any{"trigger": a.Trigger})); err != nil {
			s.log.Warn("record reminder", "err", err)
			continue
		}
		st, _ := s.store.GetSettings(ctx, a.UserID)
		body := a.Description
		if body == "" {
			body = s.whenText(ctx, t, a.UserID)
		}
		link := s.taskLink(t)
		s.store.AddActivity(ctx, a.UserID, t.ID, "reminded", a.Trigger)
		if !st.NotifyAlarms {
			continue
		}
		if l := listOf(a.UserID, t.ListID); l != nil && l.GroupNotifications {
			var b *bucket
			for _, x := range buckets {
				if x.uid == a.UserID && x.list.ID == l.ID {
					b = x
				}
			}
			if b == nil {
				b = &bucket{uid: a.UserID, list: l}
				buckets = append(buckets, b)
			}
			if !slices.Contains(b.titles, t.Title) {
				b.titles = append(b.titles, t.Title)
			}
			continue
		}
		kind, url, token := s.notifyRoute(ctx, a.UserID, st)
		if url == "" {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if err := notify.Send(cctx, kind, url, token, t.Title, body, link); err != nil {
			s.log.Warn("reminder not sent", "task", t.ID, "err", err)
		}
		cancel()
	}
	for _, b := range buckets {
		st, _ := s.store.GetSettings(ctx, b.uid)
		kind, url, token := s.notifyRoute(ctx, b.uid, st)
		if url == "" {
			continue
		}
		title, body := groupedReminder(b.list.Name, b.titles)
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if err := notify.Send(cctx, kind, url, token, title, body, s.listLink(b.list)); err != nil {
			s.log.Warn("grouped reminder not sent", "list", b.list.ID, "err", err)
		}
		cancel()
	}
}

// groupedReminder is the one notification for a list's reminders: "Kupovina · 3", "Jaja, Kefir, So".
func groupedReminder(list string, titles []string) (string, string) {
	if len(titles) == 1 {
		return titles[0], list
	}
	return fmt.Sprintf("%s · %d", list, len(titles)), strings.Join(titles, ", ")
}

func (s *Server) listLink(l *store.List) string {
	if s.cfg.BaseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/list/%d", s.cfg.BaseURL, l.ID)
}

func (s *Server) taskLink(t *store.Task) string {
	if s.cfg.BaseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/today?task=%d", s.cfg.BaseURL, t.ID)
}

// whenText says when a task is due, for a notification.
func (s *Server) whenText(ctx context.Context, t *store.Task, uid int64) string {
	loc := s.loc(ctx, uid)
	parts := []string{}
	if d := t.DueStr(); d != "" {
		parts = append(parts, "Due "+humanWhen(d, loc))
	} else if st := t.StartStr(); st != "" {
		parts = append(parts, humanWhen(st, loc))
	}
	if t.List != nil && t.List.Name != "" {
		parts = append(parts, t.List.Name)
	}
	if len(parts) == 0 {
		return "Reminder"
	}
	return strings.Join(parts, " · ")
}

func humanWhen(v string, loc *time.Location) string {
	tm, ok := ical.ParseLocal(v, loc)
	if !ok {
		return v
	}
	now := time.Now().In(loc)
	day := ""
	switch tm.Format(ical.DayLayout) {
	case now.Format(ical.DayLayout):
		day = "today"
	case now.AddDate(0, 0, 1).Format(ical.DayLayout):
		day = "tomorrow"
	case now.AddDate(0, 0, -1).Format(ical.DayLayout):
		day = "yesterday"
	default:
		day = tm.Format("Mon 2 Jan")
	}
	if ical.IsDay(v) {
		return day
	}
	return day + " at " + tm.Format("15:04")
}

// digests sends the morning summary to everyone whose time it is.
func (s *Server) digests(ctx context.Context) {
	for _, uid := range s.store.UserIDs(ctx) {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || st.DigestTime == "" {
			continue
		}
		loc := s.loc(ctx, uid)
		now := time.Now().In(loc)
		today := now.Format(ical.DayLayout)
		if st.DigestSentOn == today || now.Format("15:04") < st.DigestTime {
			continue
		}
		// not more than an hour late (the box was off)
		at, _ := time.ParseInLocation(ical.DayLayout+" 15:04", today+" "+st.DigestTime, loc)
		if now.Sub(at) > time.Hour {
			_ = s.store.SetUserSetting(ctx, uid, "digest_sent_on", today)
			continue
		}
		_ = s.store.SetUserSetting(ctx, uid, "digest_sent_on", today)
		kind, url, token := s.notifyRoute(ctx, uid, st)
		if url == "" {
			continue
		}
		inbox, _ := s.store.Inbox(ctx, uid)
		tasks, _, err := s.store.ListTasks(ctx, uid, store.Query{View: "today", Today: today, Limit: 40}, inbox.ID)
		if err != nil {
			continue
		}
		if len(tasks) == 0 {
			continue
		}
		var lines []string
		overdue := 0
		// A list that groups its notifications is one line: "• Kupovina: Jaja, Kefir, So".
		grouped := map[int64]int{}
		allLists, _ := s.store.ListLists(ctx, uid)
		groups := map[int64]string{}
		for _, l := range allLists {
			if l.GroupNotifications {
				groups[l.ID] = l.Name
			}
		}
		for _, t := range tasks {
			s.decorate(t, today, now.Format(ical.TimeLayout))
			if t.Overdue {
				overdue++
			}
			if name, ok := groups[t.ListID]; ok {
				if i, ok := grouped[t.ListID]; ok {
					lines[i] += ", " + t.Title
				} else {
					grouped[t.ListID] = len(lines)
					lines = append(lines, "• "+name+": "+t.Title)
				}
				continue
			}
			mark := "•"
			if t.Overdue {
				mark = "⚠︎"
			}
			when := ""
			if d := t.DueStr(); d != "" && !ical.IsDay(d) && ical.Day(d) == today {
				when = " (" + d[11:16] + ")"
			} else if st := t.StartStr(); st != "" && !ical.IsDay(st) && ical.Day(st) == today {
				when = " (" + st[11:16] + ")"
			}
			lines = append(lines, mark+" "+t.Title+when)
		}
		title := fmt.Sprintf("Today: %d task", len(tasks))
		if len(tasks) != 1 {
			title += "s"
		}
		if overdue > 0 {
			title += fmt.Sprintf(", %d overdue", overdue)
		}
		link := ""
		if s.cfg.BaseURL != "" {
			link = s.cfg.BaseURL + "/today"
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if err := notify.Send(cctx, kind, url, token, title, strings.Join(lines, "\n"), link); err != nil {
			s.log.Warn("digest not sent", "user", uid, "err", err)
		}
		cancel()
	}
}
