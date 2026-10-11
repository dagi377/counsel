-- module: app/legal/matter
-- Place-of-supply facts on the matter (docs/QUESTIONS.md Q-TAX-FIELDS). Additive only: two
-- nullable columns. court_province is the province whose court or tribunal has the litigation
-- (BIL-T-012); property_province is where the real property the matter relates to is situated
-- (BIL-T-011). NULL means not litigation / not about real property.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

ALTER TABLE matter.matter
  ADD COLUMN court_province    char(2),
  ADD COLUMN property_province char(2);
