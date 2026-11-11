-- Reminders of a list's tasks arrive as one notification per list (a shopping list), and the
-- morning digest shows the list as one line.
ALTER TABLE lists ADD COLUMN group_notifications INTEGER NOT NULL DEFAULT 0;
