package server

import (
	"context"
	"time"

	"github.com/abjelosevic88/pocket-homelab-tasks/internal/ical"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

// seedDemo fills an empty box with made-up tasks (DEMO_DATA=true).
func (s *Server) seedDemo(ctx context.Context) error {
	if n := s.store.Counts(ctx)["tasks"]; n > 0 {
		return nil
	}
	email, name := "demo@localhost", "Demo"
	if s.cfg.AuthMode == "none" {
		email, name = "admin@localhost", "Admin"
	}
	u, err := s.firstOrSystemUser(ctx, email, name)
	if err != nil {
		return err
	}
	uid := u.ID
	loc := s.loc(ctx, uid)
	day := func(d int) *string {
		v := time.Now().In(loc).AddDate(0, 0, d).Format(ical.DayLayout)
		return &v
	}
	at := func(d int, hm string) *string {
		v := time.Now().In(loc).AddDate(0, 0, d).Format(ical.DayLayout) + "T" + hm + ":00"
		return &v
	}
	home := &store.Area{UserID: uid, Name: "Home", Color: "#16a34a"}
	work := &store.Area{UserID: uid, Name: "Work", Color: "#2563eb"}
	_ = s.store.CreateArea(ctx, home)
	_ = s.store.CreateArea(ctx, work)
	inbox, _ := s.store.Inbox(ctx, uid)
	house := &store.List{UserID: uid, Name: "House", Color: "#16a34a", AreaID: &home.ID}
	shop := &store.List{UserID: uid, Name: "Shopping", Color: "#f59e0b", AreaID: &home.ID, Description: "Things to pick up"}
	server := &store.List{UserID: uid, Name: "Server rebuild", Color: "#2563eb", AreaID: &work.ID}
	for _, l := range []*store.List{house, shop, server} {
		_ = s.store.CreateList(ctx, l)
	}
	plan := &store.Section{UserID: uid, ListID: server.ID, Name: "Plan"}
	build := &store.Section{UserID: uid, ListID: server.ID, Name: "Build"}
	_ = s.store.CreateSection(ctx, plan)
	_ = s.store.CreateSection(ctx, build)
	est := 30
	tasks := []*store.Task{
		{ListID: inbox.ID, Title: "Call the insurance about the roof", Notes: "Policy number is in the folder."},
		{ListID: inbox.ID, Title: "Book dentist", Tags: []string{"health"}},
		{ListID: house.ID, Title: "Water the plants", Repeat: &store.Repeat{RRule: "FREQ=DAILY;INTERVAL=3", From: "completion"}, Start: day(0)},
		{ListID: house.ID, Title: "Take out the bins", Repeat: &store.Repeat{RRule: "FREQ=WEEKLY;BYDAY=TU", From: "due"}, Due: day(1), Alarms: []ical.Alarm{{Trigger: "-PT1H"}}},
		{ListID: house.ID, Title: "Fix the garden gate", Due: day(-2), Priority: 1, Tags: []string{"diy"}},
		{ListID: house.ID, Title: "Paint the fence", Someday: true, Tags: []string{"diy"}},
		{ListID: shop.ID, Title: "Milk"},
		{ListID: shop.ID, Title: "Coffee beans"},
		{ListID: shop.ID, Title: "Light bulbs E27", Notes: "Two warm white, dimmable."},
		{ListID: server.ID, SectionID: &plan.ID, Title: "Decide on the disk layout", Due: day(3), Priority: 5, EstimateMin: &est},
		{ListID: server.ID, SectionID: &plan.ID, Title: "Write the runbook", Tags: []string{"docs"}},
		{ListID: server.ID, SectionID: &build.ID, Title: "Move Immich to the NVMe", Start: at(0, "14:00"), EstimateMin: &est, Alarms: []ical.Alarm{{Trigger: "-PT15M"}}},
		{ListID: server.ID, SectionID: &build.ID, Title: "Re-check the backup plan", Start: day(2), Due: day(5)},
		{ListID: server.ID, SectionID: &build.ID, Title: "Order the case", Status: ical.StatusDone},
	}
	var parent *store.Task
	for i, t := range tasks {
		t.UserID = uid
		if t.Status == "" {
			t.Status = ical.StatusOpen
		}
		if t.Status == ical.StatusDone {
			now := store.FormatTime(time.Now().Add(-26 * time.Hour))
			t.CompletedAt = &now
		}
		if t.Tags == nil {
			t.Tags = []string{}
		}
		if t.Alarms == nil {
			t.Alarms = []ical.Alarm{}
		}
		if err := s.saveTask(ctx, t, "demo", "created"); err != nil {
			return err
		}
		if i == 11 {
			parent = t
		}
	}
	for _, title := range []string{"Stop the container", "rsync the library", "Point the volume at the new path"} {
		c := &store.Task{UserID: uid, ListID: parent.ListID, ParentID: &parent.ID, Title: title, Status: ical.StatusOpen, Tags: []string{}, Alarms: []ical.Alarm{}}
		_ = s.saveTask(ctx, c, "demo", "created")
	}
	_ = s.store.SaveFilter(ctx, &store.Filter{UserID: uid, Name: "High priority", Icon: "!", Query: store.Query{Status: "open", Priority: intPtr(1)}})
	s.log.Info("demo data seeded", "user", email)
	return nil
}

func intPtr(n int) *int { return &n }
