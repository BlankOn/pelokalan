DROP TABLE IF EXISTS module_reservations;
ALTER TABLE projects DROP COLUMN IF EXISTS reservation_days;
ALTER TABLE projects DROP COLUMN IF EXISTS reservations;
