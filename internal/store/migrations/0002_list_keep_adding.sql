-- Quick add stays open after Return for lists you keep adding to (a shopping list).
ALTER TABLE lists ADD COLUMN keep_adding INTEGER NOT NULL DEFAULT 0;
