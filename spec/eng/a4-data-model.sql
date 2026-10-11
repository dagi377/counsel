-- Counsel canonical data model. PostgreSQL 16 (also parses on 14+; no 15+/16-only syntax used).
-- One database per firm cell. No firm_id on rows: tenancy is the database (see a4-data-model.html, DM-002).
-- Conventions (each is a DM rule in a4-data-model.html):
--   * PK: id uuid DEFAULT core.uuidv7()                         (DM-001)
--   * money: bigint *_cents, CAD only, column currency fixed     (DM-003, DM-004)
--   * ALL rates and percentages: integer *_ppm (13% = 130000; 9.975% = 99750). No *_bp columns (DM-005, CF-01)
--   * ledger shape (CF-02): trust.trust_transaction is the By-Law 9 book of record (one signed row per client-ledger
--     movement); TR-1500/TR-2500 balances are derived from it. gl.* holds the operating book only.
--   * ownership (RV2-014): every object is owned by counsel_owner (migrations only). counsel_app is never owner;
--     RLS is ENABLEd and FORCEd on every matter-scoped table. Integrity functions run as counsel_integrity (BYPASSRLS).
--   * trust posting transactions run at READ COMMITTED (RV1-025; enforced by trust.before_txn, DM-065)
--   * durations: integer minutes; quantities: bigint *_milli     (DM-006)
--   * timestamps timestamptz (UTC stored); business dates `date` in America/Toronto (DM-007)
--   * mutable rows carry created_at/by, updated_at/by, row_version, archived_at; no hard delete (DM-010)
--   * append-only tables: core.forbid_mutation() trigger + REVOKE UPDATE, DELETE (DM-011)
--   * metadata lines "-- @module x @retention RC-y @class group:Pn,..." precede each table and drive a4-data-model.json
-- Conversation / document bodies live in encrypted object storage; DB holds *_ref keys only (DM-020).
-- Layers (k1-kernel; per-table tag in a4-data-model.json "layer"; this comment is the only DDL change):
--   kernel schemas (domain-free business plumbing): core, audit, gate, platform, work, agent
--   app schemas (legal app, app/legal):              party, intake, conflict, matter, timekeeping, trust, billing
--   mixed schemas (kernel except the listed app tables):
--     gl    app: vendor_bill                (kernel ledger engine: account, journal_entry, journal_line, period)
--     docs  app: court_form, engagement_letter, precedent, research_memo, bundle, analysis_table
--     cal   app: limitation_assessment      (kernel deadline engine: rule_pack, holiday, deadline, ...)
--     comms app: comm_filing                (kernel notifications/portal: portal_access, message_template, ...)
--   app table in a kernel schema: platform.readiness_record
--   kernel tables that FK into app tables (e.g. *.matter_id) are listed in a4-data-model.json layers.kernel_to_app_fks
--   and tools/kernel_bindings.txt; that list may only shrink.

CREATE SCHEMA IF NOT EXISTS core;
CREATE SCHEMA IF NOT EXISTS party;
CREATE SCHEMA IF NOT EXISTS intake;
CREATE SCHEMA IF NOT EXISTS conflict;
CREATE SCHEMA IF NOT EXISTS matter;
CREATE SCHEMA IF NOT EXISTS docs;
CREATE SCHEMA IF NOT EXISTS cal;
CREATE SCHEMA IF NOT EXISTS work;
CREATE SCHEMA IF NOT EXISTS timekeeping;
CREATE SCHEMA IF NOT EXISTS trust;
CREATE SCHEMA IF NOT EXISTS billing;
CREATE SCHEMA IF NOT EXISTS gl;
CREATE SCHEMA IF NOT EXISTS comms;
CREATE SCHEMA IF NOT EXISTS gate;
CREATE SCHEMA IF NOT EXISTS agent;
CREATE SCHEMA IF NOT EXISTS audit;
CREATE SCHEMA IF NOT EXISTS platform;

-- =====================================================================================
-- Shared functions
-- =====================================================================================

-- RFC 9562 UUIDv7 (48-bit unix ms + random). PG16 has no built-in uuidv7 (PG18 does).
CREATE OR REPLACE FUNCTION core.uuidv7() RETURNS uuid LANGUAGE plpgsql VOLATILE AS $$
DECLARE
  ms bigint := floor(extract(epoch FROM clock_timestamp()) * 1000);
  b  bytea  := uuid_send(gen_random_uuid());
BEGIN
  b := overlay(b PLACING substring(int8send(ms) FROM 3) FROM 1 FOR 6);
  b := set_byte(b, 6, (get_byte(b, 6) & 15) | 112);   -- version 7
  b := set_byte(b, 8, (get_byte(b, 8) & 63) | 128);   -- variant 10
  RETURN encode(b, 'hex')::uuid;
END $$;

-- Session context set by the app per transaction: SET LOCAL counsel.user_id = '<uuid>'.
CREATE OR REPLACE FUNCTION core.current_user_id() RETURNS uuid LANGUAGE sql STABLE AS $$
  SELECT nullif(current_setting('counsel.user_id', true), '')::uuid
$$;

CREATE OR REPLACE FUNCTION core.touch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at  := now();
  NEW.updated_by  := coalesce(core.current_user_id(), NEW.updated_by);
  NEW.row_version := OLD.row_version + 1;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION core.forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'DM-011: % on append-only table %.% is forbidden', TG_OP, TG_TABLE_SCHEMA, TG_TABLE_NAME
    USING ERRCODE = 'restrict_violation';
END $$;

CREATE OR REPLACE FUNCTION core.forbid_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'DM-010: DELETE on %.% is forbidden; archive instead', TG_TABLE_SCHEMA, TG_TABLE_NAME
    USING ERRCODE = 'restrict_violation';
END $$;

-- =====================================================================================
-- core: firm singleton, users, roles, access, settings
-- =====================================================================================

-- @module core @retention RC-CONFIG @class identity:P1,tax_numbers:P1
CREATE TABLE core.firm (
  id                    uuid PRIMARY KEY DEFAULT core.uuidv7(),
  singleton             boolean NOT NULL DEFAULT true UNIQUE CHECK (singleton),
  legal_name            text NOT NULL,
  display_name          text NOT NULL,
  address               jsonb NOT NULL DEFAULT '{}'::jsonb,
  hst_registration_no   text,
  fiscal_year_end_mmdd  char(5) NOT NULL DEFAULT '12-31' CHECK (fiscal_year_end_mmdd ~ '^(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])$'),
  matter_number_format  text NOT NULL DEFAULT '{YY}-{SEQ:4}',
  letterhead_ref        text,
  region                text NOT NULL DEFAULT 'ca-central-1' CHECK (region LIKE 'ca-%'),
  time_zone             text NOT NULL DEFAULT 'America/Toronto' CHECK (time_zone = 'America/Toronto'),
  currency              char(3) NOT NULL DEFAULT 'CAD' CHECK (currency = 'CAD'),
  sole_practitioner     boolean NOT NULL DEFAULT false,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  updated_by            uuid,
  row_version           integer NOT NULL DEFAULT 1
);

-- @module core @retention RC-CONFIG @class name:P1
CREATE TABLE core.role (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  key             text NOT NULL UNIQUE,
  name            text NOT NULL UNIQUE,
  built_in        boolean NOT NULL DEFAULT false,
  based_on_id     uuid REFERENCES core.role(id),
  licensee_only   boolean NOT NULL DEFAULT false,
  status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','retired')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  row_version     integer NOT NULL DEFAULT 1,
  archived_at     timestamptz
);

-- @module core @retention RC-CONFIG @class config:P0
CREATE TABLE core.capability (
  id                              text PRIMARY KEY,           -- e.g. 'trust.withdraw' (natural key, fixed catalogue)
  name                            text NOT NULL,
  area                            text NOT NULL,
  licensee_required_for_approve   boolean NOT NULL DEFAULT false
);

-- @module core @retention RC-CONFIG @class config:P0
CREATE TABLE core.role_grant (
  role_id        uuid NOT NULL REFERENCES core.role(id),
  capability_id  text NOT NULL REFERENCES core.capability(id),
  level          text NOT NULL CHECK (level IN ('none','view','own','all','approve')),
  PRIMARY KEY (role_id, capability_id)
);

-- Merged: access.User + platform.User. Agents are NOT users (DM-030).
-- @module core @retention RC-PERSONNEL @class name_email:P2,licence:P1,security:P3
CREATE TABLE core.app_user (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  email             text NOT NULL,
  display_name      text NOT NULL,
  role_id           uuid NOT NULL REFERENCES core.role(id),
  licence_type      text NOT NULL DEFAULT 'none' CHECK (licence_type IN ('lawyer','paralegal','none')),
  lso_number        text,
  licence_status    text NOT NULL DEFAULT 'n/a' CHECK (licence_status IN ('good_standing','suspended','unknown','n/a')),
  supervisor_id     uuid REFERENCES core.app_user(id),
  is_system_admin   boolean NOT NULL DEFAULT false,
  principal_kind    text NOT NULL DEFAULT 'human' CHECK (principal_kind IN ('human','agent','service')),
  expires_on        date,
  mfa_method        text CHECK (mfa_method IN ('totp','webauthn','sms')),
  mfa_verified_at   timestamptz,
  failed_sign_ins   integer NOT NULL DEFAULT 0 CHECK (failed_sign_ins >= 0),
  status            text NOT NULL DEFAULT 'invited' CHECK (status IN ('invited','pending_approval','active','suspended','offboarded')),
  offboarded_at     timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  created_by        uuid,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid,
  row_version       integer NOT NULL DEFAULT 1,
  archived_at       timestamptz,
  CONSTRAINT app_user_licensee_needs_lso CHECK (licence_type = 'none' OR lso_number IS NOT NULL)
);
CREATE UNIQUE INDEX app_user_email_uq ON core.app_user (lower(email));

-- @module core @retention RC-CONFIG @class name:P1
CREATE TABLE core.team (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name         text NOT NULL UNIQUE,
  lead_id      uuid REFERENCES core.app_user(id),
  status       text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  created_by   uuid REFERENCES core.app_user(id),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  updated_by   uuid REFERENCES core.app_user(id),
  row_version  integer NOT NULL DEFAULT 1,
  archived_at  timestamptz
);

-- @module core @retention RC-CONFIG @class membership:P1
CREATE TABLE core.team_user (
  team_id  uuid NOT NULL REFERENCES core.team(id),
  user_id  uuid NOT NULL REFERENCES core.app_user(id),
  PRIMARY KEY (team_id, user_id)
);

-- @module core @retention RC-AUDIT @class access:P1
CREATE TABLE core.temp_grant (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id        uuid NOT NULL REFERENCES core.app_user(id),
  capability_id  text NOT NULL REFERENCES core.capability(id),
  level          text NOT NULL CHECK (level IN ('view','own','all','approve')),
  matter_id      uuid,                         -- FK added after matter.matter
  reason         text NOT NULL,
  approval_id    uuid,                         -- FK added after gate.approval_request (GA3)
  starts_at      timestamptz NOT NULL DEFAULT now(),
  expires_at     timestamptz NOT NULL,
  revoked_at     timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now(),
  created_by     uuid NOT NULL REFERENCES core.app_user(id),
  CHECK (expires_at > starts_at),
  CHECK (created_by <> user_id)
);

-- @module core @retention RC-AUDIT @class limits:P1
CREATE TABLE core.authority_limit (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id      uuid NOT NULL REFERENCES core.app_user(id),
  kind         text NOT NULL CHECK (kind IN ('writedown_pct','trust_withdrawal_max','practice_area')),
  value_int    bigint,                          -- cents for trust_withdrawal_max, ppm for writedown_pct
  value_ref    uuid,                            -- practice_area id
  approval_id  uuid,
  valid_from   date NOT NULL,
  valid_to     date,
  created_at   timestamptz NOT NULL DEFAULT now(),
  created_by   uuid NOT NULL REFERENCES core.app_user(id),
  CHECK (valid_to IS NULL OR valid_to >= valid_from),
  CHECK (created_by <> user_id)
);

-- @module core @retention RC-CONFIG @class security:P1
CREATE TABLE core.sign_in_policy (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  singleton          boolean NOT NULL DEFAULT true UNIQUE CHECK (singleton),
  idp_protocol       text CHECK (idp_protocol IN ('oidc','saml')),
  idp_config_ref     text,
  verified_domains   text[] NOT NULL DEFAULT '{}',
  enforce_sso        boolean NOT NULL DEFAULT false,
  mfa_methods        text[] NOT NULL DEFAULT '{totp,webauthn}',
  lockout_threshold  integer NOT NULL DEFAULT 5 CHECK (lockout_threshold BETWEEN 3 AND 20),
  session_lifetime_min integer NOT NULL DEFAULT 720 CHECK (session_lifetime_min > 0),
  idle_timeout_min   integer NOT NULL DEFAULT 30 CHECK (idle_timeout_min > 0),
  ip_allowlist       cidr[],
  approval_id        uuid,
  updated_at         timestamptz NOT NULL DEFAULT now(),
  updated_by         uuid REFERENCES core.app_user(id),
  row_version        integer NOT NULL DEFAULT 1
);

-- Merged: access.Session + readiness.DeviceSession.
-- @module core @retention RC-OPS @class device_ip:P2
CREATE TABLE core.user_session (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id      uuid NOT NULL REFERENCES core.app_user(id),
  platform     text NOT NULL CHECK (platform IN ('web','ios','android','desktop','portal')),
  device       text,
  ip           inet,
  started_at   timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  ended_at     timestamptz,
  end_reason   text CHECK (end_reason IN ('logout','idle','expired','revoked','offboarded'))
);
CREATE INDEX user_session_open_idx ON core.user_session (user_id) WHERE ended_at IS NULL;

-- Gapless per-series counters for matter, bill, credit note numbers (DM-040).
-- @module core @retention RC-FIN @class counters:P0
CREATE TABLE core.number_series (
  series      text NOT NULL,                     -- 'matter','bill','credit_note','receipt'
  period_key  text NOT NULL DEFAULT '',          -- e.g. '2026' when the format resets yearly
  next_value  bigint NOT NULL DEFAULT 1 CHECK (next_value > 0),
  PRIMARY KEY (series, period_key)
);

-- @module core @retention RC-CONFIG @class config:P0
CREATE TABLE core.tag (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name        text NOT NULL,
  colour      text,
  applies_to  text NOT NULL CHECK (applies_to IN ('matter','contact','document','work_item')),
  UNIQUE (applies_to, name)
);

-- Merged: matters.SavedView + work.SavedFilter.
-- @module core @retention RC-CONFIG @class config:P1
CREATE TABLE core.saved_view (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  owner_id     uuid NOT NULL REFERENCES core.app_user(id),
  surface      text NOT NULL CHECK (surface IN ('matters','work','time','billing','contacts','documents','leads')),
  name         text NOT NULL,
  query        jsonb NOT NULL,
  sort         jsonb,
  visibility   text NOT NULL DEFAULT 'private' CHECK (visibility IN ('private','team','firm')),
  subscription jsonb,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  updated_by   uuid,
  row_version  integer NOT NULL DEFAULT 1,
  archived_at  timestamptz,
  UNIQUE (owner_id, surface, name)
);

-- @module core @retention RC-CONFIG @class prefs:P1
CREATE TABLE core.notification_preference (
  user_id      uuid NOT NULL REFERENCES core.app_user(id),
  event        text NOT NULL,
  channel      text NOT NULL CHECK (channel IN ('in_app','email','sms','push')),
  enabled      boolean NOT NULL DEFAULT true,
  locked       boolean NOT NULL DEFAULT false,   -- firm-mandated (e.g. critical deadline) cannot be disabled
  quiet_hours  jsonb,
  delegate_id  uuid REFERENCES core.app_user(id),
  PRIMARY KEY (user_id, event, channel),
  CHECK (NOT locked OR enabled)
);

-- @module core @retention RC-OPS @class schedule:P1
CREATE TABLE core.availability (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id            uuid NOT NULL REFERENCES core.app_user(id),
  starts_on          date NOT NULL,
  ends_on            date NOT NULL,
  kind               text NOT NULL CHECK (kind IN ('vacation','sick','court','training','part_time','other')),
  hours_unavailable  integer CHECK (hours_unavailable >= 0),
  delegate_id        uuid REFERENCES core.app_user(id),
  note               text,
  status             text NOT NULL DEFAULT 'requested' CHECK (status IN ('requested','approved','declined','cancelled')),
  approved_by        uuid REFERENCES core.app_user(id),
  approved_at        timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  CHECK (ends_on >= starts_on),
  CHECK (delegate_id IS DISTINCT FROM user_id)
);

-- =====================================================================================
-- party: contacts (people and organizations), relationships, identification (By-Law 7.1)
-- =====================================================================================

-- Merged: intake.Contact + matters.Contact.
-- @module party @retention RC-CONFLICT @class names:P2,contact_details:P2,dob_numbers:P3
CREATE TABLE party.contact (
  id                  uuid PRIMARY KEY DEFAULT core.uuidv7(),
  kind                text NOT NULL CHECK (kind IN ('person','organization')),
  legal_name          text NOT NULL,
  display_name        text NOT NULL,
  aliases             text[] NOT NULL DEFAULT '{}',
  emails              jsonb NOT NULL DEFAULT '[]'::jsonb,
  phones              jsonb NOT NULL DEFAULT '[]'::jsonb,
  addresses           jsonb NOT NULL DEFAULT '[]'::jsonb,
  date_of_birth       date,
  occupation          text,
  org_number          text,
  preferred_language  text NOT NULL DEFAULT 'en' CHECK (preferred_language IN ('en','fr')),
  ok_to_leave_message boolean NOT NULL DEFAULT false,
  marketing_consent   boolean NOT NULL DEFAULT false,
  is_client           boolean NOT NULL DEFAULT false,
  state               text NOT NULL DEFAULT 'proposed' CHECK (state IN ('proposed','active','archived','merged')),
  merged_into_id      uuid REFERENCES party.contact(id),
  payment_profile_id  uuid,                         -- FK added after billing.payment_profile
  bill_layout_id      uuid,                         -- FK added after billing.bill_layout
  custom_fields       jsonb NOT NULL DEFAULT '{}'::jsonb,
  search_tsv          tsvector,
  created_at          timestamptz NOT NULL DEFAULT now(),
  created_by          uuid REFERENCES core.app_user(id),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  updated_by          uuid REFERENCES core.app_user(id),
  row_version         integer NOT NULL DEFAULT 1,
  archived_at         timestamptz,
  CHECK ((state = 'merged') = (merged_into_id IS NOT NULL)),
  CHECK (merged_into_id IS DISTINCT FROM id),
  CHECK (kind = 'person' OR date_of_birth IS NULL)
);
CREATE INDEX contact_search_idx ON party.contact USING gin (search_tsv);
CREATE INDEX contact_name_idx   ON party.contact (lower(legal_name));

-- @module party @retention RC-CONFIG @class consent:P2
CREATE TABLE party.contact_tag (
  contact_id uuid NOT NULL REFERENCES party.contact(id),
  tag_id     uuid NOT NULL REFERENCES core.tag(id),
  PRIMARY KEY (contact_id, tag_id)
);

-- Consent log is append-only (marketing, SMS, recording consent).
-- @module party @retention RC-AUDIT @class consent:P2
CREATE TABLE party.consent_event (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  contact_id  uuid NOT NULL REFERENCES party.contact(id),
  purpose     text NOT NULL CHECK (purpose IN ('marketing','sms','call_recording','portal','e_signature')),
  granted     boolean NOT NULL,
  method      text NOT NULL,
  evidence_ref text,
  at          timestamptz NOT NULL DEFAULT now(),
  recorded_by uuid REFERENCES core.app_user(id)
);

-- Merged: matters.Relationship + conflicts.PartyRelationship.
-- @module party @retention RC-CONFLICT @class relationship:P2
CREATE TABLE party.contact_relationship (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  from_contact_id  uuid NOT NULL REFERENCES party.contact(id),
  to_contact_id    uuid NOT NULL REFERENCES party.contact(id),
  kind             text NOT NULL CHECK (kind IN ('spouse','parent','child','director_of','officer_of','shareholder_of',
                                                 'subsidiary_of','employee_of','member_of_firm','other')),
  source_ref       text,
  starts_on        date,
  ends_on          date,
  proposed_by_agent boolean NOT NULL DEFAULT false,
  confirmed_by     uuid REFERENCES core.app_user(id),
  confirmed_at     timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid REFERENCES core.app_user(id),
  archived_at      timestamptz,
  CHECK (from_contact_id <> to_contact_id),
  UNIQUE (from_contact_id, to_contact_id, kind)
);

-- Merged: conflicts.IdentityRecord + matters IdVerification. By-Law 7.1 client identification (verify sections).
-- @module party @retention RC-ID @class identity:P3,risk:P3
CREATE TABLE party.identity_record (
  id                      uuid PRIMARY KEY DEFAULT core.uuidv7(),
  contact_id              uuid NOT NULL REFERENCES party.contact(id),
  subject_kind            text NOT NULL CHECK (subject_kind IN ('client','third_party','beneficial_owner')),
  obligation              text NOT NULL CHECK (obligation IN ('identify','identify_and_verify','exempt')),
  exemption_reason        text,
  status                  text NOT NULL DEFAULT 'open' CHECK (status IN ('open','verified','failed','expired')),
  source_of_funds         text,
  risk_rating_open        text CHECK (risk_rating_open IN ('low','medium','high')),
  risk_reason_open        text,
  risk_rating_close       text CHECK (risk_rating_close IN ('low','medium','high')),
  risk_reason_close       text,
  verified_by             uuid REFERENCES core.app_user(id),
  verified_on             date,
  retention_until         date,                    -- computed by app: later of end of retainer / last txn + 6y (verify)
  created_at              timestamptz NOT NULL DEFAULT now(),
  created_by              uuid REFERENCES core.app_user(id),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  updated_by              uuid REFERENCES core.app_user(id),
  row_version             integer NOT NULL DEFAULT 1,
  CHECK (obligation <> 'exempt' OR exemption_reason IS NOT NULL),
  CHECK (status <> 'verified' OR (verified_by IS NOT NULL AND verified_on IS NOT NULL))
);

-- @module party @retention RC-ID @class id_numbers:P3
CREATE TABLE party.id_document (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  identity_record_id uuid NOT NULL REFERENCES party.identity_record(id),
  method             text NOT NULL CHECK (method IN ('government_photo_id','credit_file','dual_process','affiliate','agent')),
  doc_type           text NOT NULL,
  number_ciphertext  bytea NOT NULL,                -- envelope-encrypted with cell key; never plaintext (DM-021)
  number_last4       text,
  issuer_place       text,
  expires_on         date,
  file_ref           text,
  auth_vendor_ref    text,
  auth_result        text CHECK (auth_result IN ('pass','fail','review')),
  created_at         timestamptz NOT NULL DEFAULT now(),
  created_by         uuid REFERENCES core.app_user(id)
);

-- @module party @retention RC-ID @class ownership:P3
CREATE TABLE party.beneficial_owner (
  id                  uuid PRIMARY KEY DEFAULT core.uuidv7(),
  identity_record_id  uuid NOT NULL REFERENCES party.identity_record(id),
  owner_contact_id    uuid NOT NULL REFERENCES party.contact(id),
  role                text NOT NULL,
  ownership_ppm       integer CHECK (ownership_ppm BETWEEN 0 AND 1000000),
  control_description text,
  id_status           text NOT NULL DEFAULT 'pending' CHECK (id_status IN ('pending','verified','not_required')),
  sanctions_result    text CHECK (sanctions_result IN ('clear','hit','review'))
);

-- =====================================================================================
-- matter: practice areas, matters, roles, team, walls, facts, notes
-- =====================================================================================

-- Merged: matters.PracticeArea + platform.PracticeArea.
-- @module matter @retention RC-CONFIG @class config:P0
CREATE TABLE matter.practice_area (
  id                          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name                        text NOT NULL UNIQUE,
  default_billing_method      text NOT NULL DEFAULT 'hourly' CHECK (default_billing_method IN ('hourly','flat','contingency','mixed','pro_bono','legal_aid')),
  default_roles               text[] NOT NULL DEFAULT '{}',
  default_retention_class     text NOT NULL DEFAULT 'RC-FILE-15',
  intake_questions            jsonb NOT NULL DEFAULT '[]'::jsonb,
  closing_checklist_template  jsonb NOT NULL DEFAULT '[]'::jsonb,
  default_skills              text[] NOT NULL DEFAULT '{}',
  config_version              integer NOT NULL DEFAULT 1,
  approval_id                 uuid,
  created_at                  timestamptz NOT NULL DEFAULT now(),
  updated_at                  timestamptz NOT NULL DEFAULT now(),
  updated_by                  uuid REFERENCES core.app_user(id),
  row_version                 integer NOT NULL DEFAULT 1,
  archived_at                 timestamptz
);

-- @module matter @retention RC-CONFIG @class config:P0
CREATE TABLE matter.stage (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  practice_area_id  uuid NOT NULL REFERENCES matter.practice_area(id),
  name              text NOT NULL,
  ordinal           integer NOT NULL CHECK (ordinal >= 0),
  expected_days     integer CHECK (expected_days >= 0),
  flags             text[] NOT NULL DEFAULT '{}',
  entry_criteria    jsonb,
  archived_at       timestamptz,
  UNIQUE (practice_area_id, ordinal),
  UNIQUE (practice_area_id, name)
);

-- @module matter @retention RC-CONFIG @class config:P0
CREATE TABLE matter.field_def (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  scope             text NOT NULL CHECK (scope IN ('matter','contact')),
  practice_area_id  uuid REFERENCES matter.practice_area(id),
  key               text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{0,62}$'),
  label             text NOT NULL,
  type              text NOT NULL CHECK (type IN ('text','number','money_cents','date','bool','select','multiselect','contact_ref')),
  required          boolean NOT NULL DEFAULT false,
  options           jsonb,
  help              text,
  ordinal           integer NOT NULL DEFAULT 0,
  hidden            boolean NOT NULL DEFAULT false,
  archived_at       timestamptz,
  CHECK (scope = 'matter' OR practice_area_id IS NULL)
);
CREATE UNIQUE INDEX field_def_key_uq ON matter.field_def (scope, coalesce(practice_area_id, '00000000-0000-0000-0000-000000000000'::uuid), key);

-- @module matter @retention RC-CONFIG @class config:P0
CREATE TABLE matter.matter_template (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name              text NOT NULL UNIQUE,
  practice_area_id  uuid NOT NULL REFERENCES matter.practice_area(id),
  is_default        boolean NOT NULL DEFAULT false,
  preset_fields     jsonb NOT NULL DEFAULT '{}'::jsonb,
  billing_preset    jsonb NOT NULL DEFAULT '{}'::jsonb,
  folder_structure  jsonb NOT NULL DEFAULT '[]'::jsonb,
  team_roles        jsonb NOT NULL DEFAULT '[]'::jsonb,
  work_template_id  uuid,                            -- FK added after work.work_template
  status            text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','retired')),
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid REFERENCES core.app_user(id),
  row_version       integer NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX matter_template_one_default ON matter.matter_template (practice_area_id) WHERE is_default;

-- @module matter @retention RC-FILE @class description:P4,numbers:P1
CREATE TABLE matter.matter (
  id                      uuid PRIMARY KEY DEFAULT core.uuidv7(),
  number                  text NOT NULL UNIQUE,
  short_name              text NOT NULL,
  description             text,
  client_id               uuid NOT NULL REFERENCES party.contact(id),
  practice_area_id        uuid NOT NULL REFERENCES matter.practice_area(id),
  stage_id                uuid REFERENCES matter.stage(id),
  status                  text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','open','closing','closed','archived')),
  visibility              text NOT NULL DEFAULT 'firm_open' CHECK (visibility IN ('firm_open','team_only','restricted')),
  responsible_lawyer_id   uuid NOT NULL REFERENCES core.app_user(id),
  originating_lawyer_id   uuid REFERENCES core.app_user(id),
  responsible_staff_id    uuid REFERENCES core.app_user(id),
  open_date               date,
  close_date              date,
  billing_method          text NOT NULL DEFAULT 'hourly' CHECK (billing_method IN ('hourly','flat','contingency','mixed','pro_bono','legal_aid')),
  rate_table_id           uuid,                    -- FK added after timekeeping.rate_table
  budget_cents            bigint CHECK (budget_cents >= 0),
  retainer_floor_cents    bigint CHECK (retainer_floor_cents >= 0),
  court_file_number       text,
  court_location          text,
  limitation_date         date,                    -- denormalized from cal.deadline (critical); display only
  retention_class         text NOT NULL DEFAULT 'RC-FILE-15',
  legal_hold              boolean NOT NULL DEFAULT false,
  legacy_number           text,
  applied_template_id     uuid REFERENCES matter.matter_template(id),
  source_lead_id          uuid,                    -- FK added after intake.lead
  custom_fields           jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at              timestamptz NOT NULL DEFAULT now(),
  created_by              uuid REFERENCES core.app_user(id),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  updated_by              uuid REFERENCES core.app_user(id),
  row_version             integer NOT NULL DEFAULT 1,
  archived_at             timestamptz,
  CHECK (close_date IS NULL OR open_date IS NULL OR close_date >= open_date),
  CHECK (status NOT IN ('closed','archived') OR close_date IS NOT NULL),
  CHECK (status = 'pending' OR open_date IS NOT NULL)
);
CREATE INDEX matter_client_idx ON matter.matter (client_id);
CREATE INDEX matter_resp_idx   ON matter.matter (responsible_lawyer_id) WHERE status IN ('open','closing');

-- Merged: matters.MatterRole + intake.PartyRole (lead or matter, exactly one).
-- @module matter @retention RC-CONFLICT @class role:P2
CREATE TABLE matter.party_role (
  id                      uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id               uuid REFERENCES matter.matter(id),
  lead_id                 uuid,                    -- FK added after intake.lead
  contact_id              uuid NOT NULL REFERENCES party.contact(id),
  role                    text NOT NULL,            -- client, opposing_party, opposing_counsel, witness, expert, payer, ...
  side                    text CHECK (side IN ('client','adverse','neutral','related')),
  represented_by_role_id  uuid REFERENCES matter.party_role(id),
  conflict_relevant       boolean NOT NULL DEFAULT true,
  conflict_check_id       uuid,                    -- FK added after conflict.conflict_check
  starts_on               date,
  ends_on                 date,
  proposed_by_agent       boolean NOT NULL DEFAULT false,
  approved_by             uuid REFERENCES core.app_user(id),
  approved_at             timestamptz,
  notes                   text,
  created_at              timestamptz NOT NULL DEFAULT now(),
  created_by              uuid REFERENCES core.app_user(id),
  archived_at             timestamptz,
  CHECK ((matter_id IS NULL) <> (lead_id IS NULL))
);
CREATE INDEX party_role_matter_idx  ON matter.party_role (matter_id);
CREATE INDEX party_role_contact_idx ON matter.party_role (contact_id);

-- Merged: access.TeamMember + matters.MatterAccess + matters MatterMember.
-- @module matter @retention RC-AUDIT @class access:P1
CREATE TABLE matter.matter_member (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id     uuid NOT NULL REFERENCES matter.matter(id),
  user_id       uuid REFERENCES core.app_user(id),
  team_id       uuid REFERENCES core.team(id),
  matter_role   text NOT NULL DEFAULT 'member' CHECK (matter_role IN ('responsible','originating','member','staff','reviewer')),
  level         text NOT NULL DEFAULT 'edit' CHECK (level IN ('view','edit','manage')),
  starts_on     date NOT NULL DEFAULT CURRENT_DATE,
  ends_on       date,
  granted_by    uuid NOT NULL REFERENCES core.app_user(id),
  granted_at    timestamptz NOT NULL DEFAULT now(),
  CHECK ((user_id IS NULL) <> (team_id IS NULL)),
  CHECK (granted_by IS DISTINCT FROM user_id)
);
CREATE UNIQUE INDEX matter_member_active_uq ON matter.matter_member (matter_id, coalesce(user_id, team_id)) WHERE ends_on IS NULL;

-- Merged: conflicts.EthicalWall + matters.EthicalWall + platform.EthicalWall + access.Wall.
-- @module matter @retention RC-AUDIT @class wall:P1
CREATE TABLE matter.ethical_wall (
  id                   uuid PRIMARY KEY DEFAULT core.uuidv7(),
  reason               text NOT NULL,
  origin_clearance_id  uuid,                         -- FK added after conflict.clearance
  status               text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','active','lifted')),
  effective_from       timestamptz,
  review_on            date,
  lifted_at            timestamptz,
  approval_id          uuid,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by           uuid NOT NULL REFERENCES core.app_user(id),
  CHECK (status <> 'active' OR effective_from IS NOT NULL),
  CHECK ((status = 'lifted') = (lifted_at IS NOT NULL))
);

-- @module matter @retention RC-AUDIT @class wall:P1
CREATE TABLE matter.ethical_wall_matter (
  wall_id    uuid NOT NULL REFERENCES matter.ethical_wall(id),
  matter_id  uuid NOT NULL REFERENCES matter.matter(id),
  PRIMARY KEY (wall_id, matter_id)
);

-- @module matter @retention RC-AUDIT @class wall:P1
CREATE TABLE matter.ethical_wall_screened_user (
  wall_id   uuid NOT NULL REFERENCES matter.ethical_wall(id),
  user_id   uuid NOT NULL REFERENCES core.app_user(id),
  undertaking_doc_ref text,
  PRIMARY KEY (wall_id, user_id)
);

-- @module matter @retention RC-FILE @class link:P1
CREATE TABLE matter.matter_link (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_a_id uuid NOT NULL REFERENCES matter.matter(id),
  matter_b_id uuid NOT NULL REFERENCES matter.matter(id),
  reason      text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid REFERENCES core.app_user(id),
  CHECK (matter_a_id < matter_b_id),
  UNIQUE (matter_a_id, matter_b_id)
);

-- @module matter @retention RC-CONFIG @class tag:P0
CREATE TABLE matter.matter_tag (
  matter_id uuid NOT NULL REFERENCES matter.matter(id),
  tag_id    uuid NOT NULL REFERENCES core.tag(id),
  PRIMARY KEY (matter_id, tag_id)
);

-- Append-only stage history.
-- @module matter @retention RC-FILE @class history:P1
CREATE TABLE matter.stage_change (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id      uuid NOT NULL REFERENCES matter.matter(id),
  from_stage_id  uuid REFERENCES matter.stage(id),
  to_stage_id    uuid NOT NULL REFERENCES matter.stage(id),
  evidence_refs  text[] NOT NULL DEFAULT '{}',
  proposed_by_agent_run_id uuid,
  approved_by    uuid REFERENCES core.app_user(id),
  at             timestamptz NOT NULL DEFAULT now()
);

-- Facts are versioned by supersession, never edited in place.
-- @module matter @retention RC-FILE @class facts:P4
CREATE TABLE matter.fact (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id      uuid NOT NULL REFERENCES matter.matter(id),
  key            text NOT NULL,
  value          jsonb NOT NULL,
  source_refs    text[] NOT NULL DEFAULT '{}',
  confirmed_by   uuid REFERENCES core.app_user(id),
  confirmed_at   timestamptz,
  superseded_by  uuid REFERENCES matter.fact(id),
  created_at     timestamptz NOT NULL DEFAULT now(),
  created_by     uuid REFERENCES core.app_user(id)
);
CREATE UNIQUE INDEX fact_current_uq ON matter.fact (matter_id, key) WHERE superseded_by IS NULL;

-- @module matter @retention RC-FILE @class body:P4
CREATE TABLE matter.note (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id         uuid NOT NULL REFERENCES matter.matter(id),
  author_id         uuid REFERENCES core.app_user(id),
  agent_run_id      uuid,                           -- FK added after agent.agent_run
  body_ref          text NOT NULL,                   -- encrypted object; DB holds key only (DM-020)
  visibility        text NOT NULL DEFAULT 'team' CHECK (visibility IN ('private','team','firm','client')),
  privileged        boolean NOT NULL DEFAULT true,
  version           integer NOT NULL DEFAULT 1,
  confirmed_by      uuid REFERENCES core.app_user(id),
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid REFERENCES core.app_user(id),
  row_version       integer NOT NULL DEFAULT 1,
  archived_at       timestamptz,
  CHECK ((author_id IS NULL) <> (agent_run_id IS NULL) OR (author_id IS NOT NULL)),
  CHECK (NOT (privileged AND visibility = 'client'))
);

-- Merged: platform.ClosingChecklist + platform.RetentionRecord.
-- @module matter @retention RC-FILE @class closing:P1
CREATE TABLE matter.matter_closing (
  matter_id              uuid PRIMARY KEY REFERENCES matter.matter(id),
  items                  jsonb NOT NULL DEFAULT '[]'::jsonb,
  trust_zero_verified_at timestamptz,
  closing_letter_doc_id  uuid,                       -- FK added after docs.document
  retention_class        text NOT NULL,
  destroy_after          date,
  legal_hold             boolean NOT NULL DEFAULT false,
  hold_reason            text,
  keep_set               jsonb NOT NULL DEFAULT '[]'::jsonb,
  closed_by              uuid REFERENCES core.app_user(id),
  closed_at              timestamptz,
  destroyed_at           timestamptz,
  destruction_certificate_ref text,
  CHECK (NOT legal_hold OR hold_reason IS NOT NULL),
  CHECK (destroyed_at IS NULL OR NOT legal_hold),
  CHECK (closed_at IS NULL OR trust_zero_verified_at IS NOT NULL)
);

-- =====================================================================================
-- intake: leads, forms, consultations, sequences
-- =====================================================================================

-- @module intake @retention RC-CONFIG @class config:P0
CREATE TABLE intake.lead_source (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name              text NOT NULL UNIQUE,
  kind              text NOT NULL CHECK (kind IN ('web_form','referral','phone','email','directory','ads','receptionist','other')),
  active            boolean NOT NULL DEFAULT true,
  capture_key_hash  bytea
);

-- @module intake @retention RC-FIN @class spend:P1
CREATE TABLE intake.campaign (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  source_id       uuid NOT NULL REFERENCES intake.lead_source(id),
  name            text NOT NULL,
  starts_on       date NOT NULL,
  ends_on         date,
  spend_by_month  jsonb NOT NULL DEFAULT '{}'::jsonb,   -- {"2026-10": 125000} cents
  CHECK (ends_on IS NULL OR ends_on >= starts_on)
);

-- Merged: intake.Pipeline + PipelineStage (stages as child table).
-- @module intake @retention RC-CONFIG @class config:P0
CREATE TABLE intake.pipeline (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  practice_area_id  uuid REFERENCES matter.practice_area(id),
  name              text NOT NULL UNIQUE
);

-- @module intake @retention RC-CONFIG @class config:P0
CREATE TABLE intake.pipeline_stage (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  pipeline_id  uuid NOT NULL REFERENCES intake.pipeline(id),
  name         text NOT NULL,
  ordinal      integer NOT NULL,
  is_gate      boolean NOT NULL DEFAULT false,
  sla_days     integer CHECK (sla_days >= 0),
  UNIQUE (pipeline_id, ordinal)
);

-- @module intake @retention RC-CONFIG @class config:P0
CREATE TABLE intake.intake_form (
  id                    uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name                  text NOT NULL,
  slug                  text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9-]{3,64}$'),
  practice_area_id      uuid REFERENCES matter.practice_area(id),
  field_defs            jsonb NOT NULL,
  auto_ack_template_id  uuid,
  allow_self_booking    boolean NOT NULL DEFAULT false,
  consult_fee_cents     bigint NOT NULL DEFAULT 0 CHECK (consult_fee_cents >= 0),
  status                text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','retired')),
  approved_by           uuid REFERENCES core.app_user(id),
  approved_at           timestamptz,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  updated_by            uuid REFERENCES core.app_user(id),
  row_version           integer NOT NULL DEFAULT 1,
  CHECK (status <> 'published' OR approved_by IS NOT NULL)
);

-- @module intake @retention RC-LEAD @class prospect:P2,analysis:P4
CREATE TABLE intake.lead (
  id                         uuid PRIMARY KEY DEFAULT core.uuidv7(),
  contact_id                 uuid NOT NULL REFERENCES party.contact(id),
  source_id                  uuid REFERENCES intake.lead_source(id),
  campaign_id                uuid REFERENCES intake.campaign(id),
  form_id                    uuid REFERENCES intake.intake_form(id),
  practice_area_id           uuid REFERENCES matter.practice_area(id),
  practice_area_confidence   integer CHECK (practice_area_confidence BETWEEN 0 AND 100),
  pipeline_stage_id          uuid REFERENCES intake.pipeline_stage(id),
  urgency                    text CHECK (urgency IN ('low','normal','high','limitation_risk')),
  limitation_flag            boolean NOT NULL DEFAULT false,
  pre_conflict_status        text NOT NULL DEFAULT 'not_run' CHECK (pre_conflict_status IN ('not_run','clear','possible','conflict')),
  assignee_id                uuid REFERENCES core.app_user(id),
  estimated_value_cents      bigint CHECK (estimated_value_cents >= 0),
  fit                        text CHECK (fit IN ('fit','maybe','no_fit')),
  fit_reasons                jsonb,
  fit_overridden_by          uuid REFERENCES core.app_user(id),
  triage_outcome             text,
  decision                   text CHECK (decision IN ('accept','decline','refer')),
  decision_reason            text,
  decided_by                 uuid REFERENCES core.app_user(id),
  decided_at                 timestamptz,
  sla_due_at                 timestamptz,
  received_at                timestamptz NOT NULL DEFAULT now(),
  merged_into_id             uuid REFERENCES intake.lead(id),
  agent_stopped_at           timestamptz,
  payment_status             text CHECK (payment_status IN ('none','pending','paid','refunded')),
  converted_matter_id        uuid REFERENCES matter.matter(id),
  created_at                 timestamptz NOT NULL DEFAULT now(),
  updated_at                 timestamptz NOT NULL DEFAULT now(),
  updated_by                 uuid REFERENCES core.app_user(id),
  row_version                integer NOT NULL DEFAULT 1,
  archived_at                timestamptz,
  CHECK ((decision IS NULL) = (decided_by IS NULL)),
  CHECK (converted_matter_id IS NULL OR decision = 'accept')
);
CREATE UNIQUE INDEX lead_converted_uq ON intake.lead (converted_matter_id) WHERE converted_matter_id IS NOT NULL;

-- @module intake @retention RC-LEAD @class payload:P4
CREATE TABLE intake.intake_submission (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  form_id          uuid REFERENCES intake.intake_form(id),
  lead_id          uuid NOT NULL REFERENCES intake.lead(id),
  channel          text NOT NULL CHECK (channel IN ('web','email','phone','receptionist','import')),
  raw_payload_ref  text NOT NULL,
  attachment_refs  text[] NOT NULL DEFAULT '{}',
  consents         jsonb NOT NULL DEFAULT '{}'::jsonb,
  ip_hash          bytea,
  submitted_at     timestamptz NOT NULL DEFAULT now()
);

-- @module intake @retention RC-CONFIG @class config:P0
CREATE TABLE intake.consult_type (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name               text NOT NULL UNIQUE,
  duration_min       integer NOT NULL CHECK (duration_min > 0),
  modes              text[] NOT NULL DEFAULT '{video}',
  availability       jsonb NOT NULL DEFAULT '{}'::jsonb,
  min_notice_hours   integer NOT NULL DEFAULT 24 CHECK (min_notice_hours >= 0),
  buffers_min        integer NOT NULL DEFAULT 0 CHECK (buffers_min >= 0),
  fee_cents          bigint NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
  paused             boolean NOT NULL DEFAULT false
);

-- @module intake @retention RC-FILE @class notes:P4
CREATE TABLE intake.consultation (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  lead_id          uuid NOT NULL REFERENCES intake.lead(id),
  consult_type_id  uuid REFERENCES intake.consult_type(id),
  lawyer_id        uuid NOT NULL REFERENCES core.app_user(id),
  mode             text NOT NULL CHECK (mode IN ('in_person','phone','video')),
  starts_at        timestamptz NOT NULL,
  duration_min     integer NOT NULL CHECK (duration_min > 0),
  fee_cents        bigint NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
  fee_status       text NOT NULL DEFAULT 'none' CHECK (fee_status IN ('none','due','paid','waived','refunded')),
  payment_id       uuid,                              -- FK added after billing.payment
  status           text NOT NULL DEFAULT 'booked' CHECK (status IN ('booked','held','no_show','cancelled')),
  brief_doc_id     uuid,
  note_doc_id      uuid,
  outcome          text,
  signed_off_by    uuid REFERENCES core.app_user(id),
  signed_off_at    timestamptz
);

-- Merged: NonEngagementLetter + Referral as typed lead outcome letters.
-- @module intake @retention RC-FILE @class letter:P4
CREATE TABLE intake.lead_outcome_letter (
  id                    uuid PRIMARY KEY DEFAULT core.uuidv7(),
  lead_id               uuid NOT NULL REFERENCES intake.lead(id),
  kind                  text NOT NULL CHECK (kind IN ('non_engagement','referral')),
  reason                text NOT NULL,
  limitation_warning    text,
  referred_to_firm      text,
  referred_to_lawyer    text,
  referral_fee_agreement_ref text,
  client_consent        boolean,
  draft_doc_id          uuid,
  approval_id           uuid,
  sent_at               timestamptz,
  CHECK (kind <> 'referral' OR referred_to_firm IS NOT NULL),
  CHECK (kind <> 'non_engagement' OR limitation_warning IS NOT NULL)
);

-- @module intake @retention RC-CONFIG @class config:P0
CREATE TABLE intake.intake_sequence (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name             text NOT NULL,
  version          integer NOT NULL DEFAULT 1,
  trigger          jsonb NOT NULL,
  filters          jsonb NOT NULL DEFAULT '{}'::jsonb,
  steps            jsonb NOT NULL,
  stop_conditions  jsonb NOT NULL DEFAULT '[]'::jsonb,
  status           text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','paused','retired')),
  UNIQUE (name, version)
);

-- @module intake @retention RC-OPS @class state:P1
CREATE TABLE intake.sequence_run (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  sequence_id  uuid NOT NULL REFERENCES intake.intake_sequence(id),
  lead_id      uuid NOT NULL REFERENCES intake.lead(id),
  step_index   integer NOT NULL DEFAULT 0,
  state        text NOT NULL CHECK (state IN ('running','stopped','done')),
  next_at      timestamptz,
  stopped_reason text,
  UNIQUE (sequence_id, lead_id)
);

-- @module intake @retention RC-LEAD @class transcript:P4
CREATE TABLE intake.receptionist_conversation (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  lead_id            uuid REFERENCES intake.lead(id),
  channel            text NOT NULL CHECK (channel IN ('phone','chat','sms')),
  started_at         timestamptz NOT NULL,
  transcript_ref     text,
  summary_ref        text,
  fit_result         text,
  recording_consent  boolean NOT NULL DEFAULT false,
  CHECK (recording_consent OR transcript_ref IS NULL OR channel <> 'phone')
);

-- =====================================================================================
-- conflict: checks, runs, hits, clearances, consents, sealed prospects, laterals
-- =====================================================================================

-- @module conflict @retention RC-CONFIG @class config:P0
CREATE TABLE conflict.conflict_settings (
  version               integer PRIMARY KEY,
  sources_searched      text[] NOT NULL,
  default_match         text NOT NULL CHECK (default_match IN ('exact','fuzzy','phonetic')),
  identifiers           text[] NOT NULL DEFAULT '{}',
  similarity_threshold_ppm integer NOT NULL DEFAULT 850000 CHECK (similarity_threshold_ppm BETWEEN 0 AND 1000000),
  auto_close_days       integer NOT NULL DEFAULT 30,
  stale_after_days      integer NOT NULL DEFAULT 90,
  approval_id           uuid,
  created_at            timestamptz NOT NULL DEFAULT now()
);

-- @module conflict @retention RC-CONFLICT @class description:P4
CREATE TABLE conflict.conflict_check (
  id                  uuid PRIMARY KEY DEFAULT core.uuidv7(),
  lead_id             uuid REFERENCES intake.lead(id),
  matter_id           uuid REFERENCES matter.matter(id),
  trigger             text NOT NULL CHECK (trigger IN ('new_lead','new_matter','new_party','lateral_hire','manual','recheck')),
  matter_description  text,
  scope               text NOT NULL DEFAULT 'full' CHECK (scope IN ('full','party_only')),
  status              text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','running','review','cleared','declined','stale')),
  settings_version    integer NOT NULL REFERENCES conflict.conflict_settings(version),
  requested_by        uuid NOT NULL REFERENCES core.app_user(id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  updated_by          uuid REFERENCES core.app_user(id),
  row_version         integer NOT NULL DEFAULT 1,
  CHECK (lead_id IS NOT NULL OR matter_id IS NOT NULL OR trigger IN ('lateral_hire','manual'))
);

-- Snapshot of parties as searched (not a live FK to role, so the check is reproducible).
-- @module conflict @retention RC-CONFLICT @class names:P2,identifiers:P3
CREATE TABLE conflict.check_party (
  id                      uuid PRIMARY KEY DEFAULT core.uuidv7(),
  check_id                uuid NOT NULL REFERENCES conflict.conflict_check(id),
  contact_id              uuid REFERENCES party.contact(id),
  name                    text NOT NULL,
  kind                    text NOT NULL CHECK (kind IN ('person','organization')),
  role                    text NOT NULL,
  aliases                 text[] NOT NULL DEFAULT '{}',
  identifier_hash         bytea,                     -- hashed DOB / corp number; never plaintext
  related_party_ids       uuid[] NOT NULL DEFAULT '{}',
  require_full_name_match boolean NOT NULL DEFAULT false
);

-- Append-only: each run is a new row.
-- @module conflict @retention RC-CONFLICT @class run:P1
CREATE TABLE conflict.check_run (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  check_id       uuid NOT NULL REFERENCES conflict.conflict_check(id),
  version        integer NOT NULL CHECK (version > 0),
  index_version  bigint NOT NULL,
  ran_at         timestamptz NOT NULL DEFAULT now(),
  complete       boolean NOT NULL,
  UNIQUE (check_id, version)
);

-- @module conflict @retention RC-CONFLICT @class hit:P2
CREATE TABLE conflict.hit (
  id                     uuid PRIMARY KEY DEFAULT core.uuidv7(),
  run_id                 uuid NOT NULL REFERENCES conflict.check_run(id),
  check_party_id         uuid NOT NULL REFERENCES conflict.check_party(id),
  matched_kind           text NOT NULL CHECK (matched_kind IN ('contact','party_role','sealed_prospect','lateral_disclosure')),
  matched_id             uuid NOT NULL,
  match_type             text NOT NULL CHECK (match_type IN ('exact','fuzzy','phonetic','identifier','relationship')),
  score_ppm              integer NOT NULL CHECK (score_ppm BETWEEN 0 AND 1000000),
  conflict_kind_proposed text,
  reason                 text,
  disposition            text CHECK (disposition IN ('not_a_conflict','conflict','needs_consent','needs_wall')),
  disposition_note       text,
  disposed_by            uuid REFERENCES core.app_user(id),
  disposed_at            timestamptz,
  CHECK ((disposition IS NULL) = (disposed_by IS NULL))
);

-- @module conflict @retention RC-CONFLICT @class memo:P4
CREATE TABLE conflict.clearance (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  check_id       uuid NOT NULL REFERENCES conflict.conflict_check(id),
  run_id         uuid NOT NULL REFERENCES conflict.check_run(id),
  memo_doc_ref   text,
  outcome        text NOT NULL CHECK (outcome IN ('clear','clear_with_conditions','decline')),
  conditions     jsonb NOT NULL DEFAULT '[]'::jsonb,
  rationale      text NOT NULL,
  approval_id    uuid NOT NULL,                       -- G2 decision; FK added after gate
  signed_by      uuid NOT NULL REFERENCES core.app_user(id),
  signed_at      timestamptz NOT NULL,
  stale          boolean NOT NULL DEFAULT false
);

-- @module conflict @retention RC-CONFLICT @class consent:P4
CREATE TABLE conflict.consent (
  id                  uuid PRIMARY KEY DEFAULT core.uuidv7(),
  clearance_id        uuid NOT NULL REFERENCES conflict.clearance(id),
  contact_id          uuid NOT NULL REFERENCES party.contact(id),
  type                text NOT NULL CHECK (type IN ('joint_retainer','acting_against_former','other')),
  disclosure_summary  text NOT NULL,
  ila_recommended     boolean NOT NULL DEFAULT true,
  status              text NOT NULL DEFAULT 'requested' CHECK (status IN ('requested','signed','refused','withdrawn')),
  document_id         uuid,
  signed_at           timestamptz
);

-- @module conflict @retention RC-CONFLICT @class prospect:P2
CREATE TABLE conflict.sealed_prospect (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  lead_id        uuid REFERENCES intake.lead(id),
  names          text[] NOT NULL,
  adverse_names  text[] NOT NULL DEFAULT '{}',
  nature         text,
  declined_at    timestamptz NOT NULL,
  sealed         boolean NOT NULL DEFAULT true
);

-- @module conflict @retention RC-CONFLICT @class lateral:P2
CREATE TABLE conflict.lateral_disclosure (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id      uuid NOT NULL REFERENCES core.app_user(id),
  former_firm  text NOT NULL,
  matters      jsonb NOT NULL,
  start_date   date NOT NULL,
  cleared_by   uuid REFERENCES core.app_user(id),
  cleared_at   timestamptz,
  CHECK (cleared_by IS DISTINCT FROM user_id)
);

-- =====================================================================================
-- docs: documents, versions, templates, drafts, citations, reviews, signatures, shares
-- =====================================================================================

-- @module docs @retention RC-FILE @class name:P1
CREATE TABLE docs.folder (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id      uuid REFERENCES matter.matter(id),
  parent_id      uuid REFERENCES docs.folder(id),
  name           text NOT NULL,
  from_template  boolean NOT NULL DEFAULT false,
  archived_at    timestamptz,
  CHECK (parent_id IS DISTINCT FROM id)
);
CREATE UNIQUE INDEX folder_name_uq ON docs.folder (matter_id, coalesce(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name)) WHERE archived_at IS NULL;

-- @module docs @retention RC-FILE @class title:P4,meta:P1
CREATE TABLE docs.document (
  id                  uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id           uuid REFERENCES matter.matter(id),
  area                text NOT NULL DEFAULT 'matter' CHECK (area IN ('matter','firm','precedent','intake')),
  folder_id           uuid REFERENCES docs.folder(id),
  title               text NOT NULL,
  doc_type            text NOT NULL,
  source              text NOT NULL CHECK (source IN ('upload','email','portal','scan','generated','agent','import')),
  privilege           text NOT NULL DEFAULT 'privileged' CHECK (privilege IN ('privileged','confidential','public')),
  client_visible      boolean NOT NULL DEFAULT false,
  retention_class     text NOT NULL DEFAULT 'RC-FILE-15',
  legal_hold          boolean NOT NULL DEFAULT false,
  current_version_id  uuid,                         -- FK added after document_version
  status              text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','final','sent','superseded','archived','lost_in_dr')),   -- lost_in_dr: object missing after DR restore (a1)
  tags                text[] NOT NULL DEFAULT '{}',
  received_at         timestamptz,
  locked_by           uuid REFERENCES core.app_user(id),
  locked_at           timestamptz,
  owner_id            uuid REFERENCES core.app_user(id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  created_by          uuid REFERENCES core.app_user(id),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  updated_by          uuid REFERENCES core.app_user(id),
  row_version         integer NOT NULL DEFAULT 1,
  archived_at         timestamptz,
  CHECK (area <> 'matter' OR matter_id IS NOT NULL),
  CHECK ((locked_by IS NULL) = (locked_at IS NULL)),
  CHECK (NOT (client_visible AND privilege = 'privileged' AND status = 'draft'))
);
CREATE INDEX document_matter_idx ON docs.document (matter_id, folder_id) WHERE archived_at IS NULL;

-- Append-only: a version is never modified; new content = new version.
-- @module docs @retention RC-FILE @class content_ref:P4
CREATE TABLE docs.document_version (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  document_id      uuid NOT NULL REFERENCES docs.document(id),
  number           integer NOT NULL CHECK (number > 0),
  sha256           bytea NOT NULL CHECK (octet_length(sha256) = 32),
  storage_key      text NOT NULL,
  mime             text NOT NULL,
  size_bytes       bigint NOT NULL CHECK (size_bytes >= 0),
  pages            integer CHECK (pages >= 0),
  text_layer_key   text,
  ocr_confidence_ppm integer CHECK (ocr_confidence_ppm BETWEEN 0 AND 1000000),
  author_id        uuid REFERENCES core.app_user(id),
  agent_run_id     uuid,
  change_summary   text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (document_id, number)
);
ALTER TABLE docs.document ADD CONSTRAINT document_current_version_fk
  FOREIGN KEY (current_version_id) REFERENCES docs.document_version(id) DEFERRABLE INITIALLY DEFERRED;

-- @module docs @retention RC-FILE @class extracted:P4
CREATE TABLE docs.extraction (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  version_id   uuid NOT NULL REFERENCES docs.document_version(id),
  kind         text NOT NULL,
  value        jsonb NOT NULL,
  page         integer,
  line_from    integer,
  line_to      integer,
  confidence_ppm integer CHECK (confidence_ppm BETWEEN 0 AND 1000000),
  status       text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','accepted','rejected')),
  CHECK (line_to IS NULL OR line_from IS NULL OR line_to >= line_from)
);

-- @module docs @retention RC-CONFIG @class form:P0
CREATE TABLE docs.court_form (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  jurisdiction  text NOT NULL DEFAULT 'ON',
  rule_set      text NOT NULL,
  form_number   text NOT NULL,
  title         text NOT NULL,
  source_url    text NOT NULL,
  version_date  date NOT NULL,
  checksum      bytea NOT NULL,
  checked_at    timestamptz NOT NULL,
  UNIQUE (rule_set, form_number, version_date)
);

-- Merged: documents.Template + platform.MessageTemplate kept separate (comms.message_template); this is document templates.
-- @module docs @retention RC-CONFIG @class template:P0
CREATE TABLE docs.doc_template (
  id                    uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name                  text NOT NULL UNIQUE,
  practice_area_id      uuid REFERENCES matter.practice_area(id),
  doc_kind              text NOT NULL,
  language              text NOT NULL DEFAULT 'en' CHECK (language IN ('en','fr')),
  locked_text           boolean NOT NULL DEFAULT false,
  status                text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','retired')),
  built_on_form_id      uuid REFERENCES docs.court_form(id),
  trigger               jsonb
);

-- @module docs @retention RC-CONFIG @class template:P0
CREATE TABLE docs.doc_template_version (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  template_id   uuid NOT NULL REFERENCES docs.doc_template(id),
  number        integer NOT NULL CHECK (number > 0),
  body_key      text NOT NULL,
  fields        jsonb NOT NULL DEFAULT '[]'::jsonb,
  conditions    jsonb NOT NULL DEFAULT '[]'::jsonb,
  approved_by   uuid REFERENCES core.app_user(id),
  approved_at   timestamptz,
  UNIQUE (template_id, number)
);

-- @module docs @retention RC-FILE @class instruction:P4
CREATE TABLE docs.draft (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id       uuid NOT NULL REFERENCES matter.matter(id),
  instruction     text NOT NULL,
  doc_kind        text NOT NULL,
  source_scope    jsonb NOT NULL DEFAULT '{}'::jsonb,
  precedent_ids   uuid[] NOT NULL DEFAULT '{}',
  document_id     uuid REFERENCES docs.document(id),
  coverage_ppm    integer CHECK (coverage_ppm BETWEEN 0 AND 1000000),
  status          text NOT NULL DEFAULT 'drafting' CHECK (status IN ('drafting','ready','in_review','approved','discarded')),
  revise_mode     text,
  agent_run_id    uuid,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid REFERENCES core.app_user(id)
);

-- @module docs @retention RC-FILE @class quote:P4
CREATE TABLE docs.citation (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  draft_id           uuid REFERENCES docs.draft(id),
  research_memo_id   uuid,                       -- FK added after docs.research_memo
  analysis_table_id  uuid,
  claim_span         int4range,
  target_version_id  uuid REFERENCES docs.document_version(id),
  external_cite      text,                       -- e.g. statute / case citation
  page               integer,
  line_from          integer,
  line_to            integer,
  quote              text,
  match_status       text NOT NULL DEFAULT 'unchecked' CHECK (match_status IN ('unchecked','exact','close','missing')),
  CHECK (target_version_id IS NOT NULL OR external_cite IS NOT NULL),
  CHECK (num_nonnulls(draft_id, research_memo_id, analysis_table_id) = 1)
);

-- @module docs @retention RC-FILE @class redline:P4
CREATE TABLE docs.redline (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  draft_id     uuid NOT NULL REFERENCES docs.draft(id),
  version_id   uuid REFERENCES docs.document_version(id),
  op           text NOT NULL CHECK (op IN ('insert','delete','replace','comment')),
  span         int4range NOT NULL,
  text         text,
  reason       text,
  citation_id  uuid REFERENCES docs.citation(id),
  decision     text CHECK (decision IN ('accepted','rejected')),
  decided_by   uuid REFERENCES core.app_user(id)
);

-- G8 review payload (decision itself is gate.gate_decision).
-- @module docs @retention RC-FILE @class review:P4
CREATE TABLE docs.document_review (
  id                   uuid PRIMARY KEY DEFAULT core.uuidv7(),
  document_version_id  uuid NOT NULL REFERENCES docs.document_version(id),
  destination          text NOT NULL CHECK (destination IN ('client','court','opposing','third_party','internal')),
  risk                 text NOT NULL CHECK (risk IN ('low','medium','high')),
  precheck_results     jsonb NOT NULL DEFAULT '{}'::jsonb,
  approval_id          uuid NOT NULL,              -- FK added after gate.approval_request
  UNIQUE (document_version_id, destination)
);

-- @module docs @retention RC-FILE @class precedent:P4
CREATE TABLE docs.precedent (
  id                   uuid PRIMARY KEY DEFAULT core.uuidv7(),
  title                text NOT NULL,
  kind                 text NOT NULL,
  practice_area_id     uuid REFERENCES matter.practice_area(id),
  tags                 text[] NOT NULL DEFAULT '{}',
  source_document_id   uuid REFERENCES docs.document(id),
  anonymized_body_key  text NOT NULL,
  status               text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','approved','retired')),
  review_due           date,
  approved_by          uuid REFERENCES core.app_user(id)
);

-- @module docs @retention RC-FILE @class letter:P4
CREATE TABLE docs.engagement_letter (
  id                   uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id            uuid NOT NULL REFERENCES matter.matter(id),
  letter_type          text NOT NULL CHECK (letter_type IN ('engagement','limited_scope','non_engagement','disengagement')),
  fee_arrangement_id   uuid,                      -- FK added after timekeeping.fee_arrangement
  scope_included       text NOT NULL,
  scope_excluded       text,
  retainer_cents       bigint CHECK (retainer_cents >= 0),
  estimate_cents       bigint CHECK (estimate_cents >= 0),
  template_version_id  uuid REFERENCES docs.doc_template_version(id),
  document_version_id  uuid REFERENCES docs.document_version(id),
  approval_id          uuid,
  approved_hash        bytea,
  status               text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','sent','signed','declined','superseded')),
  CHECK (status IN ('draft') OR approved_hash IS NOT NULL)
);
CREATE UNIQUE INDEX engagement_letter_current_uq ON docs.engagement_letter (matter_id) WHERE status IN ('approved','sent','signed');

-- Merged: documents.SignatureRequest + platform.SignatureRequest.
-- @module docs @retention RC-FILE @class signature:P2
CREATE TABLE docs.signature_request (
  id                    uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id             uuid NOT NULL REFERENCES matter.matter(id),
  document_version_ids  uuid[] NOT NULL CHECK (cardinality(document_version_ids) > 0),
  status                text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','sent','partially_signed','completed','declined','expired','voided')),
  expires_at            timestamptz,
  reminder_cadence      jsonb,
  completed_hash        bytea,
  certificate_ref       text,
  created_at            timestamptz NOT NULL DEFAULT now(),
  created_by            uuid REFERENCES core.app_user(id),
  CHECK (status <> 'completed' OR completed_hash IS NOT NULL)
);

-- @module docs @retention RC-FILE @class signer:P2
CREATE TABLE docs.signer (
  id                    uuid PRIMARY KEY DEFAULT core.uuidv7(),
  signature_request_id  uuid NOT NULL REFERENCES docs.signature_request(id),
  contact_id            uuid NOT NULL REFERENCES party.contact(id),
  role                  text NOT NULL,
  sign_order            integer NOT NULL DEFAULT 1 CHECK (sign_order > 0),
  auth_method           text NOT NULL CHECK (auth_method IN ('email','sms_otp','id_check','in_person')),
  status                text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','viewed','signed','declined')),
  signed_at             timestamptz,
  ip                    inet,
  user_agent            text,
  UNIQUE (signature_request_id, contact_id)
);

-- Merged: documents.Share + platform.DocumentShare + platform.SecureLink.
-- @module docs @retention RC-AUDIT @class recipient:P2
CREATE TABLE docs.document_share (
  id                   uuid PRIMARY KEY DEFAULT core.uuidv7(),
  document_version_id  uuid REFERENCES docs.document_version(id),
  folder_id            uuid REFERENCES docs.folder(id),
  matter_id            uuid NOT NULL REFERENCES matter.matter(id),
  recipient_kind       text NOT NULL CHECK (recipient_kind IN ('portal_contact','email','link')),
  recipient_contact_id uuid REFERENCES party.contact(id),
  recipient_email      text,
  allow_download       boolean NOT NULL DEFAULT true,
  token_hash           bytea,
  passcode_hash        bytea,
  expires_at           timestamptz,
  download_count       integer NOT NULL DEFAULT 0 CHECK (download_count >= 0),
  approval_id          uuid,
  shared_by            uuid NOT NULL REFERENCES core.app_user(id),
  shared_at            timestamptz NOT NULL DEFAULT now(),
  first_opened_at      timestamptz,
  revoked_at           timestamptz,
  CHECK ((document_version_id IS NULL) <> (folder_id IS NULL)),
  CHECK (recipient_kind <> 'link' OR (token_hash IS NOT NULL AND expires_at IS NOT NULL))
);

-- @module docs @retention RC-OPS @class request:P2
CREATE TABLE docs.document_request (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id       uuid NOT NULL REFERENCES matter.matter(id),
  contact_id      uuid REFERENCES party.contact(id),
  items           jsonb NOT NULL,
  due_on          date,
  status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open','partial','complete','cancelled')),
  reminders_sent  integer NOT NULL DEFAULT 0
);

-- @module docs @retention RC-OPS @class upload:P4
CREATE TABLE docs.upload (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id          uuid REFERENCES matter.matter(id),
  contact_id         uuid REFERENCES party.contact(id),
  request_id         uuid REFERENCES docs.document_request(id),
  file_ref           text NOT NULL,
  sha256             bytea NOT NULL,
  scan_status        text NOT NULL DEFAULT 'pending' CHECK (scan_status IN ('pending','clean','infected','error')),
  filed_document_id  uuid REFERENCES docs.document(id),
  uploaded_at        timestamptz NOT NULL DEFAULT now(),
  CHECK (filed_document_id IS NULL OR scan_status = 'clean')
);

-- @module docs @retention RC-FILE @class bundle:P4
CREATE TABLE docs.bundle (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id       uuid NOT NULL REFERENCES matter.matter(id),
  items           jsonb NOT NULL,
  options         jsonb NOT NULL DEFAULT '{}'::jsonb,
  output_doc_id   uuid REFERENCES docs.document(id),
  status          text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','building','ready','failed'))
);

-- @module docs @retention RC-FILE @class analysis:P4
CREATE TABLE docs.analysis_table (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id      uuid NOT NULL REFERENCES matter.matter(id),
  columns        jsonb NOT NULL,
  document_ids   uuid[] NOT NULL,
  template_name  text,
  created_by     uuid REFERENCES core.app_user(id),
  created_at     timestamptz NOT NULL DEFAULT now()
);

-- Merged: platform.ResearchMemo + readiness.ResearchMemo.
-- @module docs @retention RC-FILE @class memo:P4
CREATE TABLE docs.research_memo (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id          uuid NOT NULL REFERENCES matter.matter(id),
  question           text NOT NULL,
  answer_ref         text,
  jurisdiction       text NOT NULL DEFAULT 'ON',
  corpus_source      text,
  treatment_checked  boolean NOT NULL DEFAULT false,
  status             text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','in_review','approved','discarded')),
  approval_id        uuid,
  agent_run_id       uuid,
  created_at         timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE docs.citation ADD CONSTRAINT citation_memo_fk FOREIGN KEY (research_memo_id) REFERENCES docs.research_memo(id);
ALTER TABLE docs.citation ADD CONSTRAINT citation_analysis_fk FOREIGN KEY (analysis_table_id) REFERENCES docs.analysis_table(id);

-- =====================================================================================
-- cal: calendars, events, triggers, deadlines (rule packs pinned), confirmations, reminders
-- =====================================================================================

-- Merged: deadlines.RulePack + readiness.RulePack; HolidayCalendar as child.
-- @module cal @retention RC-PERM @class rules:P0
CREATE TABLE cal.rule_pack (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  code            text NOT NULL,                  -- e.g. 'ON-RCP', 'ON-LIMITATIONS'
  province        text NOT NULL DEFAULT 'ON',
  kind            text NOT NULL CHECK (kind IN ('court_rules','limitations','notice','holidays')),
  version         text NOT NULL,
  effective_from  date NOT NULL,
  status          text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','verified','active','retired')),
  rules           jsonb NOT NULL,
  golden_cases    jsonb NOT NULL DEFAULT '[]'::jsonb,
  verified_by     uuid REFERENCES core.app_user(id),
  verified_on     date,
  UNIQUE (code, version),
  CHECK (status NOT IN ('verified','active') OR verified_by IS NOT NULL)
);

-- @module cal @retention RC-PERM @class dates:P0
CREATE TABLE cal.holiday (
  rule_pack_id  uuid NOT NULL REFERENCES cal.rule_pack(id),
  day           date NOT NULL,
  name          text NOT NULL,
  kind          text NOT NULL CHECK (kind IN ('holiday','court_closure')),
  source        text NOT NULL,
  PRIMARY KEY (rule_pack_id, day, kind)
);

-- @module cal @retention RC-CONFIG @class config:P1
CREATE TABLE cal.calendar (
  id                  uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name                text NOT NULL,
  owner_user_id       uuid REFERENCES core.app_user(id),
  kind                text NOT NULL CHECK (kind IN ('personal','firm','matter','resource','deadlines')),
  colour              text,
  default_visibility  text NOT NULL DEFAULT 'busy_only' CHECK (default_visibility IN ('private','busy_only','details'))
);

-- @module cal @retention RC-CONFIG @class access:P1
CREATE TABLE cal.calendar_share (
  calendar_id  uuid NOT NULL REFERENCES cal.calendar(id),
  user_id      uuid REFERENCES core.app_user(id),
  team_id      uuid REFERENCES core.team(id),
  level        text NOT NULL CHECK (level IN ('busy','view','edit')),
  granted_by   uuid NOT NULL REFERENCES core.app_user(id),
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  CHECK ((user_id IS NULL) <> (team_id IS NULL))
);

-- @module cal @retention RC-CONFIG @class config:P0
CREATE TABLE cal.event_category (
  id                  uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name                text NOT NULL UNIQUE,
  colour              text,
  default_duration_min integer CHECK (default_duration_min > 0),
  default_reminders   jsonb,
  active              boolean NOT NULL DEFAULT true
);

-- @module cal @retention RC-FILE @class trigger:P4
CREATE TABLE cal.trigger_event (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id       uuid NOT NULL REFERENCES matter.matter(id),
  kind            text NOT NULL,
  occurred_on     date NOT NULL,
  service_method  text,
  served_where    text,
  source_doc_version_id uuid REFERENCES docs.document_version(id),
  source_page     integer,
  source_quote    text,
  status          text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','confirmed','superseded'))
);

-- Deadlines are never deleted; changes are supersession + confirmation trail.
-- @module cal @retention RC-FILE @class deadline:P1,source:P4
CREATE TABLE cal.deadline (
  id                   uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id            uuid REFERENCES matter.matter(id),
  lead_id              uuid REFERENCES intake.lead(id),
  trigger_id           uuid REFERENCES cal.trigger_event(id),
  title                text NOT NULL,
  kind                 text NOT NULL CHECK (kind IN ('limitation_basic','limitation_ultimate','notice','court','filing','internal','other')),
  due_on               date NOT NULL,
  due_time             time,
  critical             boolean NOT NULL DEFAULT false,
  status               text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','confirmed','met','released','superseded','missed')),
  responsible_user_id  uuid REFERENCES core.app_user(id),
  rule_cite            text,
  rule_pack_id         uuid REFERENCES cal.rule_pack(id),     -- pinned version (DM-060)
  computation_trail    jsonb NOT NULL DEFAULT '[]'::jsonb,
  source_doc_version_id uuid REFERENCES docs.document_version(id),
  source_page          integer,
  source_quote         text,
  confidence_ppm       integer CHECK (confidence_ppm BETWEEN 0 AND 1000000),
  assumptions          jsonb NOT NULL DEFAULT '[]'::jsonb,
  evidence_doc_id      uuid REFERENCES docs.document(id),
  release_reason       text,
  superseded_by        uuid REFERENCES cal.deadline(id),
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by           uuid REFERENCES core.app_user(id),
  agent_run_id         uuid,
  updated_at           timestamptz NOT NULL DEFAULT now(),
  updated_by           uuid REFERENCES core.app_user(id),
  row_version          integer NOT NULL DEFAULT 1,
  CHECK ((matter_id IS NULL) <> (lead_id IS NULL)),
  CHECK (status <> 'released' OR release_reason IS NOT NULL),
  CHECK (status <> 'met' OR evidence_doc_id IS NOT NULL OR NOT critical),
  CHECK ((status = 'superseded') = (superseded_by IS NOT NULL)),
  CHECK (rule_cite IS NULL OR rule_pack_id IS NOT NULL OR kind = 'internal')
);
CREATE INDEX deadline_due_idx ON cal.deadline (due_on) WHERE status IN ('proposed','confirmed');

-- Append-only two-person check trail (G7*).
-- @module cal @retention RC-FILE @class confirmation:P1
CREATE TABLE cal.deadline_confirmation (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  deadline_id       uuid NOT NULL REFERENCES cal.deadline(id),
  kind              text NOT NULL CHECK (kind IN ('confirm','second_check','change','met','release')),
  user_id           uuid NOT NULL REFERENCES core.app_user(id),
  independent_date  date,
  result            text NOT NULL CHECK (result IN ('agree','disagree','done')),
  note              text,
  gate_decision_id  uuid,
  at                timestamptz NOT NULL DEFAULT now(),
  CHECK (kind <> 'second_check' OR independent_date IS NOT NULL)
);
CREATE UNIQUE INDEX deadline_confirmation_once ON cal.deadline_confirmation (deadline_id, user_id, kind);

-- @module cal @retention RC-FILE @class assessment:P4
CREATE TABLE cal.limitation_assessment (
  id                     uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id              uuid REFERENCES matter.matter(id),
  lead_id                uuid REFERENCES intake.lead(id),
  defendant_contact_id   uuid REFERENCES party.contact(id),
  claim_type             text NOT NULL,
  act_date               date,
  discovery_date         date,
  discovery_facts        text,
  suspensions            jsonb NOT NULL DEFAULT '[]'::jsonb,
  tolling_agreement_doc_id uuid REFERENCES docs.document(id),
  basic_deadline_id      uuid REFERENCES cal.deadline(id),
  ultimate_deadline_id   uuid REFERENCES cal.deadline(id),
  notice_deadline_ids    uuid[] NOT NULL DEFAULT '{}',
  CHECK ((matter_id IS NULL) <> (lead_id IS NULL)),
  CHECK (discovery_date IS NULL OR act_date IS NULL OR discovery_date >= act_date)
);

-- @module cal @retention RC-FILE @class event:P1,title:P4
CREATE TABLE cal.calendar_event (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  calendar_id   uuid NOT NULL REFERENCES cal.calendar(id),
  category_id   uuid REFERENCES cal.event_category(id),
  title         text NOT NULL,
  type          text NOT NULL CHECK (type IN ('meeting','court','deadline','consult','task','other')),
  starts_at     timestamptz NOT NULL,
  ends_at       timestamptz NOT NULL,
  all_day       boolean NOT NULL DEFAULT false,
  location      text,
  visibility    text NOT NULL DEFAULT 'details' CHECK (visibility IN ('private','busy_only','details')),
  matter_id     uuid REFERENCES matter.matter(id),
  deadline_id   uuid UNIQUE REFERENCES cal.deadline(id),
  external_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
  archived_at   timestamptz,
  CHECK (ends_at >= starts_at),
  CHECK (type <> 'deadline' OR deadline_id IS NOT NULL)
);

-- @module cal @retention RC-FILE @class attendee:P2
CREATE TABLE cal.event_attendee (
  event_id    uuid NOT NULL REFERENCES cal.calendar_event(id),
  user_id     uuid REFERENCES core.app_user(id),
  contact_id  uuid REFERENCES party.contact(id),
  response    text CHECK (response IN ('needs_action','accepted','declined','tentative')),
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  CHECK ((user_id IS NULL) <> (contact_id IS NULL))
);

-- @module cal @retention RC-CONFIG @class config:P0
CREATE TABLE cal.reminder_policy (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  deadline_kind     text NOT NULL UNIQUE,
  offsets_days      integer[] NOT NULL,
  recipients        text[] NOT NULL,
  channels          text[] NOT NULL,
  ack_required      boolean NOT NULL DEFAULT true,
  escalation_hours  integer NOT NULL DEFAULT 24 CHECK (escalation_hours > 0),
  absence_rule      text NOT NULL DEFAULT 'delegate'
);

-- Merged: deadlines.ReminderPlan; sent[] normalized into reminder_delivery.
-- @module cal @retention RC-FILE @class delivery:P1
CREATE TABLE cal.deadline_reminder (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  deadline_id   uuid NOT NULL REFERENCES cal.deadline(id),
  policy_id     uuid REFERENCES cal.reminder_policy(id),
  due_at        timestamptz NOT NULL,
  recipient_id  uuid NOT NULL REFERENCES core.app_user(id),
  channel       text NOT NULL,
  sent_at       timestamptz,
  ack_at        timestamptz,
  escalated_at  timestamptz,
  UNIQUE (deadline_id, due_at, recipient_id, channel)
);

-- =====================================================================================
-- work: work items, workflows, templates, automation, SLA
-- =====================================================================================

-- @module work @retention RC-CONFIG @class config:P0
CREATE TABLE work.workflow (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name        text NOT NULL,
  version     integer NOT NULL CHECK (version > 0),
  applies_to  text[] NOT NULL DEFAULT '{}',
  UNIQUE (name, version)
);

-- @module work @retention RC-CONFIG @class config:P0
CREATE TABLE work.status (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  workflow_id     uuid NOT NULL REFERENCES work.workflow(id),
  name            text NOT NULL,
  status_group    text NOT NULL CHECK (status_group IN ('todo','in_progress','approval','done')),
  is_agent_ready  boolean NOT NULL DEFAULT false,
  UNIQUE (workflow_id, name)
);

-- @module work @retention RC-CONFIG @class config:P0
CREATE TABLE work.transition (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  workflow_id     uuid NOT NULL REFERENCES work.workflow(id),
  from_status_id  uuid NOT NULL REFERENCES work.status(id),
  to_status_id    uuid NOT NULL REFERENCES work.status(id),
  name            text NOT NULL,
  guards          jsonb NOT NULL DEFAULT '[]'::jsonb,
  post_actions    jsonb NOT NULL DEFAULT '[]'::jsonb,
  UNIQUE (from_status_id, to_status_id)
);

-- @module work @retention RC-CONFIG @class config:P0
CREATE TABLE work.sla_policy (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name        text NOT NULL UNIQUE,
  applies_to  jsonb NOT NULL,
  target      text NOT NULL CHECK (target IN ('first_response','resolution')),
  duration_business_min integer NOT NULL CHECK (duration_business_min > 0),
  holiday_rule_pack_id uuid REFERENCES cal.rule_pack(id)
);

-- @module work @retention RC-CONFIG @class config:P0
CREATE TABLE work.item_category (
  id                    uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name                  text NOT NULL UNIQUE,
  colour                text,
  default_estimate_min  integer CHECK (default_estimate_min >= 0),
  default_approver_role text,
  active                boolean NOT NULL DEFAULT true
);

-- @module work @retention RC-CONFIG @class config:P0
CREATE TABLE work.work_template (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name              text NOT NULL,
  practice_area_id  uuid REFERENCES matter.practice_area(id),
  version           integer NOT NULL CHECK (version > 0),
  phases            jsonb NOT NULL DEFAULT '[]'::jsonb,
  items             jsonb NOT NULL DEFAULT '[]'::jsonb,
  UNIQUE (name, version)
);
ALTER TABLE matter.matter_template ADD CONSTRAINT matter_template_work_fk FOREIGN KEY (work_template_id) REFERENCES work.work_template(id);

-- @module work @retention RC-FILE @class description:P4
CREATE TABLE work.work_item (
  id                   uuid PRIMARY KEY DEFAULT core.uuidv7(),
  key                  text NOT NULL UNIQUE,
  type                 text NOT NULL CHECK (type IN ('task','request','review','filing','milestone','bug')),
  title                text NOT NULL,
  description          text,
  workflow_id          uuid NOT NULL REFERENCES work.workflow(id),
  status_id            uuid NOT NULL REFERENCES work.status(id),
  category_id          uuid REFERENCES work.item_category(id),
  matter_id            uuid REFERENCES matter.matter(id),
  parent_id            uuid REFERENCES work.work_item(id),
  assignee_user_id     uuid REFERENCES core.app_user(id),
  assignee_is_agent    boolean NOT NULL DEFAULT false,
  approver_id          uuid REFERENCES core.app_user(id),
  reporter_id          uuid NOT NULL REFERENCES core.app_user(id),
  priority             smallint NOT NULL DEFAULT 3 CHECK (priority BETWEEN 1 AND 5),
  due_at               timestamptz,
  deadline_id          uuid REFERENCES cal.deadline(id),
  sla_policy_id        uuid REFERENCES work.sla_policy(id),
  labels               text[] NOT NULL DEFAULT '{}',
  client_visible       boolean NOT NULL DEFAULT false,
  estimate_min         integer CHECK (estimate_min >= 0),
  rank                 text NOT NULL,                 -- lexorank
  template_id          uuid REFERENCES work.work_template(id),
  external_ref         text,
  recurrence           jsonb,                         -- merged: work.Recurrence (pattern, basis, ends_on)
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  updated_by           uuid REFERENCES core.app_user(id),
  row_version          integer NOT NULL DEFAULT 1,
  archived_at          timestamptz,
  CHECK (parent_id IS DISTINCT FROM id),
  CHECK (NOT (assignee_is_agent AND assignee_user_id IS NULL)),     -- agent work is on behalf of a user
  CHECK (approver_id IS DISTINCT FROM assignee_user_id OR approver_id IS NULL)
);
CREATE INDEX work_item_matter_idx ON work.work_item (matter_id) WHERE archived_at IS NULL;
CREATE INDEX work_item_assignee_idx ON work.work_item (assignee_user_id) WHERE archived_at IS NULL;

-- @module work @retention RC-FILE @class link:P1
CREATE TABLE work.work_item_link (
  from_id  uuid NOT NULL REFERENCES work.work_item(id),
  to_id    uuid NOT NULL REFERENCES work.work_item(id),
  kind     text NOT NULL CHECK (kind IN ('blocks','relates','duplicates')),
  PRIMARY KEY (from_id, to_id, kind),
  CHECK (from_id <> to_id)
);

-- @module work @retention RC-FILE @class watcher:P1
CREATE TABLE work.watcher (
  work_item_id uuid NOT NULL REFERENCES work.work_item(id),
  user_id      uuid NOT NULL REFERENCES core.app_user(id),
  PRIMARY KEY (work_item_id, user_id)
);

-- @module work @retention RC-FILE @class body:P4
CREATE TABLE work.comment (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  work_item_id     uuid NOT NULL REFERENCES work.work_item(id),
  author_kind      text NOT NULL CHECK (author_kind IN ('user','agent','client')),
  author_user_id   uuid REFERENCES core.app_user(id),
  author_contact_id uuid REFERENCES party.contact(id),
  agent_run_id     uuid,
  body_ref         text NOT NULL,
  mentions         uuid[] NOT NULL DEFAULT '{}',
  internal         boolean NOT NULL DEFAULT true,
  external_ref     text,
  at               timestamptz NOT NULL DEFAULT now(),
  archived_at      timestamptz,
  CHECK (author_kind <> 'user'   OR author_user_id IS NOT NULL),
  CHECK (author_kind <> 'client' OR author_contact_id IS NOT NULL),
  CHECK (author_kind <> 'agent'  OR agent_run_id IS NOT NULL)
);

-- @module work @retention RC-CONFIG @class config:P1
CREATE TABLE work.board (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name          text NOT NULL,
  saved_view_id uuid REFERENCES core.saved_view(id),
  columns       jsonb NOT NULL,
  swimlane      text
);

-- @module work @retention RC-CONFIG @class config:P0
CREATE TABLE work.automation_rule (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name            text NOT NULL,
  version         integer NOT NULL DEFAULT 1,
  when_event      text NOT NULL,
  conditions      jsonb NOT NULL DEFAULT '[]'::jsonb,
  actions         jsonb NOT NULL,
  owner_id        uuid NOT NULL REFERENCES core.app_user(id),
  work_template_id uuid REFERENCES work.work_template(id),
  enabled         boolean NOT NULL DEFAULT false,
  UNIQUE (name, version)
);

-- Append-only.
-- @module work @retention RC-OPS @class run:P1
CREATE TABLE work.automation_run (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  rule_id           uuid NOT NULL REFERENCES work.automation_rule(id),
  rule_version      integer NOT NULL,
  event             text NOT NULL,
  matter_id         uuid REFERENCES matter.matter(id),
  fired_at          timestamptz NOT NULL DEFAULT now(),
  outcome           text NOT NULL CHECK (outcome IN ('done','skipped','failed')),
  actions_taken     jsonb NOT NULL DEFAULT '[]'::jsonb,
  items_created     uuid[] NOT NULL DEFAULT '{}',
  skipped_reason    text,
  error             text,
  CHECK (outcome <> 'skipped' OR skipped_reason IS NOT NULL),
  CHECK (outcome <> 'failed' OR error IS NOT NULL)
);

-- =====================================================================================
-- time: activity codes, rates, fee arrangements, time, expenses, write-downs
-- =====================================================================================

-- @module time @retention RC-CONFIG @class config:P0
CREATE TABLE timekeeping.time_settings (
  singleton            boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  rounding_increment_min integer NOT NULL DEFAULT 6 CHECK (rounding_increment_min IN (1,5,6,10,15)),
  rounding_mode        text NOT NULL DEFAULT 'up' CHECK (rounding_mode IN ('up','nearest','none')),
  minimum_entry_min    integer NOT NULL DEFAULT 6 CHECK (minimum_entry_min >= 0),
  lock_before          date,
  default_billable     boolean NOT NULL DEFAULT true,
  reminder_time        time
);

-- Merged: time.ActivityCategory (UTBMS code, tax code).
-- @module time @retention RC-CONFIG @class config:P0
CREATE TABLE timekeeping.activity_code (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  kind             text NOT NULL CHECK (kind IN ('time','expense')),
  name             text NOT NULL,
  billing_method   text NOT NULL DEFAULT 'hourly' CHECK (billing_method IN ('hourly','flat','unit')),
  rate_cents       bigint CHECK (rate_cents >= 0),
  flat_cents       bigint CHECK (flat_cents >= 0),
  unit_price_cents bigint CHECK (unit_price_cents >= 0),
  snippet          text,
  utbms_code       text,
  tax_code         text NOT NULL DEFAULT 'HST' ,
  active           boolean NOT NULL DEFAULT true,
  UNIQUE (kind, name)
);

-- Effective-dated rates per user and optional activity code.
-- @module time @retention RC-FIN @class rate:P1
CREATE TABLE timekeeping.rate_table (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name             text NOT NULL UNIQUE,
  is_default       boolean NOT NULL DEFAULT false
);

-- @module time @retention RC-FIN @class rate:P1
CREATE TABLE timekeeping.rate (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  rate_table_id     uuid NOT NULL REFERENCES timekeeping.rate_table(id),
  user_id           uuid REFERENCES core.app_user(id),
  team_id           uuid REFERENCES core.team(id),
  activity_code_id  uuid REFERENCES timekeeping.activity_code(id),
  hourly_cents      bigint CHECK (hourly_cents >= 0),
  flat_cents        bigint CHECK (flat_cents >= 0),
  effective_from    date NOT NULL,
  CHECK (num_nonnulls(hourly_cents, flat_cents) = 1)
);
CREATE UNIQUE INDEX rate_effective_uq ON timekeeping.rate (rate_table_id,
  coalesce(user_id, team_id, '00000000-0000-0000-0000-000000000000'::uuid),
  coalesce(activity_code_id, '00000000-0000-0000-0000-000000000000'::uuid), effective_from);
ALTER TABLE matter.matter ADD CONSTRAINT matter_rate_table_fk FOREIGN KEY (rate_table_id) REFERENCES timekeeping.rate_table(id);

-- Versioned: new row per change, previous gets superseded_at.
-- @module time @retention RC-FIN @class terms:P4
CREATE TABLE timekeeping.fee_arrangement (
  id                    uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id             uuid NOT NULL REFERENCES matter.matter(id),
  version               integer NOT NULL CHECK (version > 0),
  type                  text NOT NULL CHECK (type IN ('hourly','flat','contingency','milestone','capped','mixed')),
  rate_overrides        jsonb NOT NULL DEFAULT '[]'::jsonb,
  flat_cents            bigint CHECK (flat_cents >= 0),
  milestones            jsonb NOT NULL DEFAULT '[]'::jsonb,
  scope                 text,
  contingency_ppm       integer CHECK (contingency_ppm BETWEEN 0 AND 1000000),
  contingency_steps     jsonb,
  recovery_base_cents   bigint CHECK (recovery_base_cents >= 0),   -- BIL-L-020 contingency base, set when the recovery is known
  budget_cents          bigint CHECK (budget_cents >= 0),
  engagement_doc_id     uuid REFERENCES docs.document(id),
  approval_id           uuid,
  effective_from        date NOT NULL,
  superseded_at         timestamptz,
  UNIQUE (matter_id, version),
  CHECK (type <> 'contingency' OR (contingency_ppm IS NOT NULL AND engagement_doc_id IS NOT NULL)),
  CHECK (type <> 'flat' OR flat_cents IS NOT NULL)
);
CREATE UNIQUE INDEX fee_arrangement_current_uq ON timekeeping.fee_arrangement (matter_id) WHERE superseded_at IS NULL;
ALTER TABLE docs.engagement_letter ADD CONSTRAINT engagement_fee_fk FOREIGN KEY (fee_arrangement_id) REFERENCES timekeeping.fee_arrangement(id);

-- @module time @retention RC-FIN @class timesheet:P1
CREATE TABLE timekeeping.timesheet (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id       uuid NOT NULL REFERENCES core.app_user(id),
  week_start    date NOT NULL CHECK (extract(isodow FROM week_start) = 1),
  status        text NOT NULL DEFAULT 'open' CHECK (status IN ('open','submitted','approved','returned')),
  submitted_at  timestamptz,
  approver_id   uuid REFERENCES core.app_user(id),
  approved_at   timestamptz,
  comment       text,
  UNIQUE (user_id, week_start),
  CHECK (approver_id IS DISTINCT FROM user_id OR approver_id IS NULL)
);

-- @module time @retention RC-OPS @class activity:P1
CREATE TABLE timekeeping.timer (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id          uuid NOT NULL REFERENCES core.app_user(id),
  matter_id        uuid REFERENCES matter.matter(id),
  started_at       timestamptz NOT NULL DEFAULT now(),
  paused_minutes   integer NOT NULL DEFAULT 0 CHECK (paused_minutes >= 0),
  state            text NOT NULL DEFAULT 'running' CHECK (state IN ('running','paused','stopped'))
);
CREATE UNIQUE INDEX timer_one_running ON timekeeping.timer (user_id) WHERE state = 'running';

-- @module time @retention RC-OPS @class signal:P2
CREATE TABLE timekeeping.activity_signal (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id     uuid NOT NULL REFERENCES core.app_user(id),
  kind        text NOT NULL CHECK (kind IN ('email','doc_edit','calendar','call','agent_run')),
  source_ref  text NOT NULL,
  matter_id   uuid REFERENCES matter.matter(id),
  starts_at   timestamptz NOT NULL,
  ends_at     timestamptz,
  CHECK (ends_at IS NULL OR ends_at >= starts_at)
);

-- @module time @retention RC-OPS @class narrative:P4
CREATE TABLE timekeeping.time_suggestion (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id          uuid NOT NULL REFERENCES core.app_user(id),
  matter_id        uuid REFERENCES matter.matter(id),
  alt_matter_ids   uuid[] NOT NULL DEFAULT '{}',
  starts_at        timestamptz NOT NULL,
  minutes          integer NOT NULL CHECK (minutes > 0),
  activity_code_id uuid REFERENCES timekeeping.activity_code(id),
  narrative        text,
  confidence_ppm   integer CHECK (confidence_ppm BETWEEN 0 AND 1000000),
  reason           text,
  flags            text[] NOT NULL DEFAULT '{}',
  signal_ids       uuid[] NOT NULL DEFAULT '{}',
  status           text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','accepted','rejected','expired')),
  reject_reason    text,
  agent_run_id     uuid
);

-- Amounts are frozen at approval; billed entries are immutable except via write_down (trigger, DM-052).
-- @module time @retention RC-FIN @class narrative:P4,amounts:P1
CREATE TABLE timekeeping.time_entry (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id         uuid NOT NULL REFERENCES matter.matter(id),
  user_id           uuid NOT NULL REFERENCES core.app_user(id),
  work_date         date NOT NULL,
  starts_at         timestamptz,
  minutes           integer NOT NULL CHECK (minutes > 0 AND minutes <= 1440),
  billed_minutes    integer NOT NULL CHECK (billed_minutes >= 0),
  activity_code_id  uuid REFERENCES timekeeping.activity_code(id),
  work_item_id      uuid REFERENCES work.work_item(id),
  narrative         text NOT NULL,
  internal_note     text,
  billable          boolean NOT NULL DEFAULT true,
  rate_cents        bigint NOT NULL CHECK (rate_cents >= 0),
  rate_source       text NOT NULL CHECK (rate_source IN ('manual','flat_activity','matter_user','matter_role','client_user','client_role','activity','user_default')),
  amount_cents      bigint NOT NULL CHECK (amount_cents >= 0),
  tax_code          text NOT NULL DEFAULT 'HST',
  origin            text NOT NULL CHECK (origin IN ('manual','timer','suggestion','import')),
  suggestion_id     uuid UNIQUE REFERENCES timekeeping.time_suggestion(id),
  timesheet_id      uuid REFERENCES timekeeping.timesheet(id),
  status            text NOT NULL DEFAULT 'draft' CHECK (status IN ('suggested','draft','accepted','submitted','approved','on_draft_bill','billed','written_off','rejected')),
  bill_id           uuid,                          -- FK added after billing.bill
  accepted_by       uuid REFERENCES core.app_user(id),
  accepted_at       timestamptz,
  approved_by       uuid REFERENCES core.app_user(id),
  approved_at       timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  created_by        uuid REFERENCES core.app_user(id),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid REFERENCES core.app_user(id),
  row_version       integer NOT NULL DEFAULT 1,
  CHECK (billable OR amount_cents = 0),
  CHECK ((status IN ('on_draft_bill','billed')) = (bill_id IS NOT NULL)),
  CHECK (origin <> 'suggestion' OR (suggestion_id IS NOT NULL AND accepted_by IS NOT NULL))
);
CREATE INDEX time_entry_unbilled_idx ON timekeeping.time_entry (matter_id) WHERE status IN ('submitted','approved');

-- @module time @retention RC-FIN @class receipt:P1
CREATE TABLE timekeeping.expense (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id         uuid NOT NULL REFERENCES matter.matter(id),
  user_id           uuid NOT NULL REFERENCES core.app_user(id),
  expense_date      date NOT NULL,
  activity_code_id  uuid REFERENCES timekeeping.activity_code(id),
  vendor            text,
  amount_pre_tax_cents bigint NOT NULL CHECK (amount_pre_tax_cents > 0),   -- RV1-020: vendor price before vendor tax
  vendor_tax_cents  bigint NOT NULL DEFAULT 0 CHECK (vendor_tax_cents >= 0),
  -- tax on the bill is derived: is_agency -> outside the tax base; else the matter's tax code (no stored flag, RV1-020)
  is_agency         boolean NOT NULL DEFAULT false,
  markup_ppm        integer NOT NULL DEFAULT 0 CHECK (markup_ppm >= 0),
  currency          char(3) NOT NULL DEFAULT 'CAD' CHECK (currency = 'CAD'),
  original_currency char(3),
  original_amount_minor bigint,
  fx_rate_ppm       bigint CHECK (fx_rate_ppm > 0),     -- CAD per unit * 1e6 (BIL-C-043)
  fx_date           date,
  paid_from         text NOT NULL CHECK (paid_from IN ('operating','trust','personal','card','unpaid')),
  trust_transaction_id uuid,                        -- FK added after trust
  billable          boolean NOT NULL DEFAULT true,
  receipt_document_id uuid REFERENCES docs.document(id),
  status            text NOT NULL DEFAULT 'draft' CHECK (status IN ('suggested','draft','missing_receipt','approved','on_draft_bill','billed','non_billable','written_off')),
  approved_by       uuid REFERENCES core.app_user(id),
  bill_id           uuid,
  created_at        timestamptz NOT NULL DEFAULT now(),
  created_by        uuid REFERENCES core.app_user(id),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid REFERENCES core.app_user(id),
  row_version       integer NOT NULL DEFAULT 1,
  CHECK (paid_from <> 'trust' OR trust_transaction_id IS NOT NULL OR status = 'draft'),
  CHECK ((original_currency IS NULL) = (original_amount_minor IS NULL)),
  CHECK ((original_currency IS NULL) = (fx_rate_ppm IS NULL) AND (original_currency IS NULL) = (fx_date IS NULL))
);

-- Append-only adjustments to time/expense (merged: time.WriteDown + BillLine.write_down_amount).
-- @module time @retention RC-FIN @class adjustment:P1
CREATE TABLE timekeeping.write_down (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  time_entry_id   uuid REFERENCES timekeeping.time_entry(id),
  expense_id      uuid REFERENCES timekeeping.expense(id),
  original_cents  bigint NOT NULL CHECK (original_cents >= 0),
  adjusted_cents  bigint NOT NULL CHECK (adjusted_cents >= 0),
  kind            text NOT NULL CHECK (kind IN ('write_down','write_off','no_charge','write_up')),
  reason_code     text NOT NULL,
  reason          text,
  show_on_bill    boolean NOT NULL DEFAULT true,
  stage           text NOT NULL CHECK (stage IN ('pre_bill','on_bill','post_bill')),
  proposed_by     uuid NOT NULL REFERENCES core.app_user(id),
  approval_id     uuid,
  at              timestamptz NOT NULL DEFAULT now(),
  CHECK (num_nonnulls(time_entry_id, expense_id) = 1),
  CHECK (kind = 'write_up' OR adjusted_cents <= original_cents),
  CHECK (kind <> 'write_off' OR adjusted_cents = 0)
);

-- @module time @retention RC-CONFIG @class target:P1
CREATE TABLE timekeeping.timekeeper_target (
  id                     uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id                uuid NOT NULL REFERENCES core.app_user(id),
  period                 daterange NOT NULL,
  hours_target_min       integer CHECK (hours_target_min >= 0),
  amount_target_cents    bigint CHECK (amount_target_cents >= 0),
  day_length_min         integer NOT NULL DEFAULT 450,
  working_days_per_year  integer NOT NULL DEFAULT 230,
  proposed_by            uuid REFERENCES core.app_user(id),
  approved_by            uuid REFERENCES core.app_user(id)
);

-- =====================================================================================
-- trust: accounts, signatories, client ledgers, append-only transactions, withdrawals,
--        bank lines, reconciliations (LSO By-Law 9, verify section numbers)
-- =====================================================================================

-- @module trust @retention RC-FIN @class bank:P3
CREATE TABLE trust.trust_account (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  type               text NOT NULL CHECK (type IN ('mixed','separate_interest_bearing')),
  institution        text NOT NULL,
  account_no_ciphertext bytea NOT NULL,
  account_no_last4   text NOT NULL CHECK (account_no_last4 ~ '^[0-9]{4}$'),
  title              text NOT NULL,
  for_matter_id      uuid REFERENCES matter.matter(id),
  opened_on          date NOT NULL,
  live_from          date,                               -- first month kept in Counsel (cut-over); NULL = opened_on
  closed_on          date,
  eft_mode           text NOT NULL DEFAULT 'none' CHECK (eft_mode IN ('none','single','dual')),
  dual_auth_threshold_cents bigint CHECK (dual_auth_threshold_cents >= 0),
  currency           char(3) NOT NULL DEFAULT 'CAD' CHECK (currency = 'CAD'),
  approval_id        uuid,                               -- GA1
  CHECK ((type = 'separate_interest_bearing') = (for_matter_id IS NOT NULL)),
  CHECK (closed_on IS NULL OR closed_on >= opened_on)
);

-- @module trust @retention RC-FIN @class signatory:P2
CREATE TABLE trust.signatory (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  account_id     uuid NOT NULL REFERENCES trust.trust_account(id),
  user_id        uuid NOT NULL REFERENCES core.app_user(id),
  is_licensee    boolean NOT NULL,
  bond_cents     bigint CHECK (bond_cents >= 0),
  bond_expiry    date,
  eft_user       boolean NOT NULL DEFAULT false,
  valid_from     date NOT NULL,
  valid_to       date,
  approval_id    uuid NOT NULL,                      -- GA1
  CHECK (is_licensee OR bond_cents IS NOT NULL),
  CHECK (valid_to IS NULL OR valid_to >= valid_from)
);
CREATE UNIQUE INDEX signatory_active_uq ON trust.signatory (account_id, user_id) WHERE valid_to IS NULL;

-- Client trust ledger: one per (account, matter). Balance is NOT stored (DM-050); see trust.ledger_balance view.
-- @module trust @retention RC-FIN @class client:P2
CREATE TABLE trust.client_ledger (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  account_id    uuid NOT NULL REFERENCES trust.trust_account(id),
  matter_id     uuid NOT NULL REFERENCES matter.matter(id),
  client_id     uuid NOT NULL REFERENCES party.contact(id),
  conditions    text,                               -- e.g. held on undertaking
  protected_cents bigint NOT NULL DEFAULT 0 CHECK (protected_cents >= 0),   -- merged MatterTrustSettings
  protected_purpose text,
  minimum_balance_cents bigint NOT NULL DEFAULT 0 CHECK (minimum_balance_cents >= 0),
  low_balance_alert_cents bigint CHECK (low_balance_alert_cents >= 0),
  opened_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (account_id, matter_id),
  CHECK (protected_cents = 0 OR protected_purpose IS NOT NULL)
);

-- @module trust @retention RC-FIN @class bank:P2
CREATE TABLE trust.bank_import (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  account_id    uuid NOT NULL REFERENCES trust.trust_account(id),
  file_ref      text NOT NULL,
  sha256        bytea NOT NULL,
  period        daterange NOT NULL,
  imported_by   uuid NOT NULL REFERENCES core.app_user(id),
  imported_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (account_id, sha256)
);

-- Append-only bank statement lines (both trust and operating; account_kind discriminator).
-- @module trust @retention RC-FIN @class bank:P2
CREATE TABLE trust.bank_line (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  account_kind     text NOT NULL CHECK (account_kind IN ('trust','operating')),
  trust_account_id uuid REFERENCES trust.trust_account(id),
  gl_account_id    uuid,                           -- FK added after gl.account
  import_id        uuid REFERENCES trust.bank_import(id),
  posted_on        date NOT NULL,
  description      text NOT NULL,
  amount_cents     bigint NOT NULL CHECK (amount_cents <> 0),     -- signed: + deposit, - withdrawal
  bank_ref         text,
  source_seq       integer NOT NULL DEFAULT 1 CHECK (source_seq >= 1),  -- nth identical (date, amount, ref/desc) tuple within one statement
  CHECK ((account_kind = 'trust') = (trust_account_id IS NOT NULL))
);
-- RV2-022: two same-day same-amount cheques get source_seq 1 and 2; an overlapping re-import repeats the same seqs and dedupes.
CREATE UNIQUE INDEX bank_line_dedupe ON trust.bank_line (coalesce(trust_account_id, gl_account_id), posted_on, amount_cents, coalesce(bank_ref, description), source_seq);

-- @module trust @retention RC-FIN @class payee:P2
CREATE TABLE trust.withdrawal_request (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  ledger_id          uuid NOT NULL REFERENCES trust.client_ledger(id),
  purpose_category   text NOT NULL CHECK (purpose_category IN ('fees_to_operating','disbursement','client_refund','third_party','transfer_out')),
  payee              text NOT NULL,
  amount_cents       bigint NOT NULL CHECK (amount_cents > 0),
  method             text NOT NULL CHECK (method IN ('cheque','eft','wire','bank_draft')),
  bill_id            uuid,                           -- FK added after billing.bill; required for fees_to_operating
  direction_doc_id   uuid REFERENCES docs.document(id),
  receiving_account  text,
  form_9a_doc_id     uuid REFERENCES docs.document(id),   -- EFT authorization record (By-Law 9, verify form name/section)
  initiated_by       uuid NOT NULL REFERENCES core.app_user(id),
  approval_id        uuid,                           -- G11
  authorized_by      uuid REFERENCES core.app_user(id),
  second_authorized_by uuid REFERENCES core.app_user(id),
  executed_by        uuid REFERENCES core.app_user(id),
  bank_confirmation  text,
  -- RV1-002: the trust_transaction is posted at 'executed' (txn_date = executed_on); 'applied' = fee transfer applied to the bill; 'confirmed' = bank_match found.
  status             text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','pending_authorization','authorized','executed','applied','confirmed','rejected','cancelled')),
  executed_on        date,
  created_at         timestamptz NOT NULL DEFAULT now(),
  -- RV1-011: separation of duties is a trigger (core.check_sod), not a CHECK, so a sole practitioner can act alone (audited).
  CHECK (lower(btrim(payee)) NOT IN ('cash','bearer')),
  CHECK (purpose_category <> 'fees_to_operating' OR bill_id IS NOT NULL),
  CHECK (method <> 'eft' OR status IN ('proposed','pending_authorization','rejected','cancelled') OR form_9a_doc_id IS NOT NULL),
  CHECK (status NOT IN ('authorized','executed','applied','confirmed') OR (authorized_by IS NOT NULL AND approval_id IS NOT NULL)),
  CHECK (status NOT IN ('executed','applied','confirmed') OR (executed_by IS NOT NULL AND executed_on IS NOT NULL))
);

-- @module trust @retention RC-FIN @class authority:P1
CREATE TABLE trust.trust_transfer (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  from_ledger_id   uuid NOT NULL REFERENCES trust.client_ledger(id),
  to_ledger_id     uuid NOT NULL REFERENCES trust.client_ledger(id),
  amount_cents     bigint NOT NULL CHECK (amount_cents > 0),
  authority_doc_id uuid REFERENCES docs.document(id),
  withdrawal_id    uuid REFERENCES trust.withdrawal_request(id),  -- when the move crosses bank accounts
  approval_id      uuid,                                         -- G11t
  status           text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','pending_authorization','authorized','posted','rejected','cancelled')),
  CHECK (from_ledger_id <> to_ledger_id)
);

-- THE trust journal. Append-only, signed amounts, corrections only by reversal (DM-051..DM-055).
-- @module trust @retention RC-FIN @class amounts:P1,counterparty:P2
CREATE TABLE trust.trust_transaction (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  account_id         uuid NOT NULL REFERENCES trust.trust_account(id),
  ledger_id          uuid NOT NULL REFERENCES trust.client_ledger(id),
  kind               text NOT NULL CHECK (kind IN ('opening_balance','receipt','disbursement','transfer_in','transfer_out','reversal','interest','bank_charge_correction')),
  txn_date           date NOT NULL,
  method             text NOT NULL CHECK (method IN ('cheque','eft','wire','cash','bank_draft','card','internal')),
  instrument_no      text,
  counterparty       text NOT NULL,
  amount_cents       bigint NOT NULL CHECK (amount_cents <> 0),     -- + into trust, - out of trust
  purpose            text NOT NULL,
  reverses_id        uuid UNIQUE REFERENCES trust.trust_transaction(id),
  withdrawal_id      uuid REFERENCES trust.withdrawal_request(id),
  transfer_id        uuid REFERENCES trust.trust_transfer(id),
  payment_id         uuid,                                          -- FK added after billing.payment
  cash_exception_code text,
  cleared_on         date,         -- RV1-003: funds usable from this date; NULL on an uncleared cheque/draft (see trust.receipt_clearance)
  replenishes_id     uuid REFERENCES trust.trust_transaction(id),   -- RV1-012: firm-funds receipt covering an NSF shortfall
  approval_id        uuid,                                          -- trust.receipt / trust.withdrawal / trust.transfer / migration.trust_opening
  posted_by          uuid NOT NULL REFERENCES core.app_user(id),
  posted_at          timestamptz NOT NULL DEFAULT now(),
  currency           char(3) NOT NULL DEFAULT 'CAD' CHECK (currency = 'CAD'),
  CHECK (kind IN ('opening_balance','receipt','transfer_in','interest') AND amount_cents > 0
      OR kind IN ('disbursement','transfer_out') AND amount_cents < 0
      OR kind IN ('reversal','bank_charge_correction')),
  CHECK ((kind = 'reversal') = (reverses_id IS NOT NULL)),
  CHECK (kind <> 'disbursement' OR withdrawal_id IS NOT NULL),
  CHECK (kind NOT IN ('transfer_in','transfer_out') OR transfer_id IS NOT NULL),
  CHECK (method <> 'cash' OR amount_cents > 0),         -- never pay out trust in cash (By-Law 9, verify)
  CHECK (kind <> 'opening_balance' OR approval_id IS NOT NULL),        -- RV2-005: needs the migration.trust_opening (G-IMP) gate
  CHECK (replenishes_id IS NULL OR (kind = 'receipt' AND approval_id IS NOT NULL)),
  CHECK (kind NOT IN ('receipt') OR method NOT IN ('cash','internal','eft','wire','card') OR cleared_on IS NOT NULL)  -- only cheque/bank_draft may start uncleared
);
-- RV2-004: once-only money effects. A retried/replayed effect hits a unique violation instead of double-posting.
CREATE UNIQUE INDEX trust_txn_once_per_withdrawal ON trust.trust_transaction (withdrawal_id) WHERE kind = 'disbursement';
CREATE UNIQUE INDEX trust_txn_once_per_transfer_leg ON trust.trust_transaction (transfer_id, ledger_id) WHERE kind IN ('transfer_in','transfer_out');
CREATE UNIQUE INDEX trust_txn_once_per_payment ON trust.trust_transaction (payment_id, ledger_id) WHERE payment_id IS NOT NULL AND kind <> 'reversal';
CREATE UNIQUE INDEX trust_txn_one_opening ON trust.trust_transaction (ledger_id) WHERE kind = 'opening_balance';
CREATE UNIQUE INDEX trust_txn_once_per_replenish ON trust.trust_transaction (replenishes_id) WHERE replenishes_id IS NOT NULL;
CREATE INDEX trust_txn_ledger_idx  ON trust.trust_transaction (ledger_id, txn_date);
CREATE INDEX trust_txn_account_idx ON trust.trust_transaction (account_id, txn_date);

-- @module trust @retention RC-FIN @class match:P1
CREATE TABLE trust.bank_match (
  bank_line_id         uuid NOT NULL REFERENCES trust.bank_line(id),
  trust_transaction_id uuid NOT NULL REFERENCES trust.trust_transaction(id),
  matched_by           uuid NOT NULL REFERENCES core.app_user(id),
  matched_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (bank_line_id, trust_transaction_id)
);

-- Monthly three-way reconciliation; signed rows are immutable; reopening = new row with superseded link.
-- @module trust @retention RC-FIN @class reconciliation:P1
CREATE TABLE trust.reconciliation (
  id                        uuid PRIMARY KEY DEFAULT core.uuidv7(),
  account_id                uuid NOT NULL REFERENCES trust.trust_account(id),
  month                     date NOT NULL CHECK (extract(day FROM month) = 1),
  bank_balance_cents        bigint NOT NULL,
  outstanding_items         jsonb NOT NULL DEFAULT '[]'::jsonb,
  adjusted_bank_cents       bigint NOT NULL,
  journal_balance_cents     bigint NOT NULL,
  client_listing_total_cents bigint NOT NULL,
  client_listing_snapshot   jsonb NOT NULL,                -- [{ledger_id, balance_cents}] frozen at sign-off
  explanations              text,
  prepared_by               uuid NOT NULL REFERENCES core.app_user(id),
  signed_by                 uuid REFERENCES core.app_user(id),
  signed_at                 timestamptz,
  approval_id               uuid,                          -- trust.reconciliation_signoff
  status                    text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','ready','signed','imported','superseded')),
  external_signer           text,                          -- imported months: signer named in the source system
  imported_job_id           uuid,                          -- FK added after platform.import_job
  bank_statement_doc_id     uuid REFERENCES docs.document(id),
  package_ref               text,
  package_sha256            bytea,
  superseded_by             uuid REFERENCES trust.reconciliation(id),
  reopen_reason             text,
  CHECK (status <> 'signed' OR (signed_by IS NOT NULL AND signed_at IS NOT NULL AND package_sha256 IS NOT NULL
         AND adjusted_bank_cents = journal_balance_cents AND journal_balance_cents = client_listing_total_cents)),
  CHECK (status <> 'imported' OR (imported_job_id IS NOT NULL AND external_signer IS NOT NULL)),
  CHECK ((status = 'superseded') = (superseded_by IS NOT NULL))
);
CREATE UNIQUE INDEX reconciliation_month_uq ON trust.reconciliation (account_id, month) WHERE status <> 'superseded';

-- @module trust @retention RC-FIN @class worksheet:P1
CREATE TABLE trust.annual_report_worksheet (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  licensee_id   uuid NOT NULL REFERENCES core.app_user(id),
  year          integer NOT NULL CHECK (year BETWEEN 2000 AND 2100),
  answers       jsonb NOT NULL,
  approval_id   uuid,                           -- GA2
  confirmed_by  uuid REFERENCES core.app_user(id),
  confirmed_at  timestamptz,
  UNIQUE (licensee_id, year)
);

-- Derived balances (DM-050). Never store a balance column.
CREATE VIEW trust.ledger_balance AS
  SELECT l.id AS ledger_id, l.account_id, l.matter_id, l.client_id,
         coalesce(sum(t.amount_cents), 0)::bigint AS balance_cents,
         max(t.txn_date) AS last_activity_on
    FROM trust.client_ledger l
    LEFT JOIN trust.trust_transaction t ON t.ledger_id = l.id
   GROUP BY l.id;

CREATE VIEW trust.account_balance AS
  SELECT a.id AS account_id, coalesce(sum(t.amount_cents), 0)::bigint AS journal_balance_cents
    FROM trust.trust_account a
    LEFT JOIN trust.trust_transaction t ON t.account_id = a.id
   GROUP BY a.id;

-- Serialize per ledger and enforce account/ledger consistency + month lock.
CREATE OR REPLACE FUNCTION trust.before_txn() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, trust, gate, core AS $$
DECLARE l_account uuid; avail bigint; ov text;
BEGIN
  IF current_setting('transaction_isolation') <> 'read committed' THEN       -- RV1-025
    RAISE EXCEPTION 'DM-065: trust postings must run at READ COMMITTED (got %)', current_setting('transaction_isolation');
  END IF;
  PERFORM 1 FROM trust.trust_account WHERE id = NEW.account_id FOR SHARE;    -- RV2-007: blocks while a month is being signed
  SELECT account_id INTO l_account FROM trust.client_ledger WHERE id = NEW.ledger_id FOR UPDATE;   -- per-ledger lock
  IF l_account IS DISTINCT FROM NEW.account_id THEN
    RAISE EXCEPTION 'DM-053: ledger % is not in account %', NEW.ledger_id, NEW.account_id;
  END IF;
  IF EXISTS (SELECT 1 FROM trust.reconciliation r
              WHERE r.account_id = NEW.account_id AND r.status IN ('signed','imported')
                AND r.month = date_trunc('month', NEW.txn_date)::date) THEN
    RAISE EXCEPTION 'DM-054: month % of account % is signed and locked', date_trunc('month', NEW.txn_date)::date, NEW.account_id;
  END IF;
  IF NEW.kind = 'reversal' THEN
    IF NOT EXISTS (SELECT 1 FROM trust.trust_transaction o
                    WHERE o.id = NEW.reverses_id AND o.ledger_id = NEW.ledger_id AND o.amount_cents = -NEW.amount_cents) THEN
      RAISE EXCEPTION 'DM-055: reversal must negate exactly one transaction on the same ledger';
    END IF;
  END IF;
  -- RV1-002/RV1-003/RV2-021: money out may use only cleared, unprotected, unheld funds above the minimum balance.
  IF NEW.kind IN ('disbursement','transfer_out') THEN
    avail := trust.available_cents(NEW.ledger_id, NEW.withdrawal_id, NEW.transfer_id);
    IF avail + NEW.amount_cents < 0 THEN
      SELECT gate_code INTO ov FROM gate.approval_request WHERE id = NEW.approval_id AND status = 'approved'
         AND gate_code IN ('trust.protected_release','trust.uncleared_override');
      IF ov IS NULL OR trust.balance_cents(NEW.ledger_id) + NEW.amount_cents < 0 THEN
        RAISE EXCEPTION 'DM-064: insufficient available trust funds on ledger % (available %, requested %)',
          NEW.ledger_id, avail, -NEW.amount_cents USING ERRCODE = 'check_violation';
      END IF;
    END IF;
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trust_txn_before BEFORE INSERT ON trust.trust_transaction
  FOR EACH ROW EXECUTE FUNCTION trust.before_txn();

-- Non-negative client ledger: checked at COMMIT so a transfer's two legs can be inserted in any order.
CREATE OR REPLACE FUNCTION trust.check_ledger_non_negative() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, trust AS $$
DECLARE bal bigint;
BEGIN
  SELECT coalesce(sum(amount_cents), 0) INTO bal FROM trust.trust_transaction WHERE ledger_id = NEW.ledger_id;
  IF bal < 0 THEN
    RAISE EXCEPTION 'DM-052: client trust ledger % would be overdrawn (balance % cents)', NEW.ledger_id, bal
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER trust_ledger_non_negative AFTER INSERT ON trust.trust_transaction
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION trust.check_ledger_non_negative();

CREATE TRIGGER trust_txn_immutable BEFORE UPDATE OR DELETE ON trust.trust_transaction
  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER trust_txn_no_truncate BEFORE TRUNCATE ON trust.trust_transaction
  FOR EACH STATEMENT EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER bank_line_immutable BEFORE UPDATE OR DELETE ON trust.bank_line
  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

-- Signed reconciliations are frozen except the supersede link.
CREATE OR REPLACE FUNCTION trust.reconciliation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'DM-010: reconciliations are never deleted'; END IF;
  IF OLD.status IN ('signed','imported') AND NOT (NEW.status = 'superseded' AND NEW.superseded_by IS NOT NULL AND NEW.reopen_reason IS NOT NULL
       AND (to_jsonb(NEW) - '{status,superseded_by,reopen_reason}'::text[]) = (to_jsonb(OLD) - '{status,superseded_by,reopen_reason}'::text[])) THEN
    RAISE EXCEPTION 'DM-056: signed reconciliation % is immutable', OLD.id;
  END IF;
  IF OLD.status = 'superseded' THEN RAISE EXCEPTION 'DM-056: superseded reconciliation is immutable'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER reconciliation_guard BEFORE UPDATE OR DELETE ON trust.reconciliation
  FOR EACH ROW EXECUTE FUNCTION trust.reconciliation_guard();

-- Month order: cannot sign month M unless M-1 is signed (or M is the account's first month).
CREATE OR REPLACE FUNCTION trust.reconciliation_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.status = 'signed' AND (TG_OP = 'INSERT' OR OLD.status <> 'signed') THEN
    -- RV2-007: exclusive account lock waits for in-flight postings, then the stored figures must match a recompute.
    PERFORM 1 FROM trust.trust_account WHERE id = NEW.account_id FOR UPDATE;
    IF NEW.journal_balance_cents <> (SELECT coalesce(sum(amount_cents),0) FROM trust.trust_transaction
                                       WHERE account_id = NEW.account_id AND txn_date < (NEW.month + interval '1 month')::date) THEN
      RAISE EXCEPTION 'DM-066: journal balance changed since the reconciliation was prepared; re-prepare month %', NEW.month;
    END IF;
    IF EXISTS (SELECT 1 FROM trust.trust_account a WHERE a.id = NEW.account_id
                 AND date_trunc('month', coalesce(a.live_from, a.opened_on))::date < NEW.month)
       AND NOT EXISTS (SELECT 1 FROM trust.reconciliation p
                        WHERE p.account_id = NEW.account_id AND p.status IN ('signed','imported')
                          AND p.month = (NEW.month - interval '1 month')::date) THEN
      RAISE EXCEPTION 'DM-057: previous month must be signed before %', NEW.month;
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER reconciliation_order BEFORE INSERT OR UPDATE ON trust.reconciliation
  FOR EACH ROW EXECUTE FUNCTION trust.reconciliation_order();

-- =====================================================================================
-- billing: tax rates, bills, lines, payments, allocations, credit notes, collections
-- =====================================================================================

-- Merged: billing.TaxProfile → firm.hst_registration_no + effective-dated tax_rate rows.
-- @module billing @retention RC-FIN @class tax:P0
CREATE TABLE billing.tax_rate (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  tax_code        text NOT NULL,                 -- 'HST-ON', 'GST', 'HST-NS', 'ZERO', 'EXEMPT'
  component       text NOT NULL CHECK (component IN ('HST','GST','PST','QST','NONE')),
  province        char(2),
  rate_ppm        integer NOT NULL CHECK (rate_ppm BETWEEN 0 AND 1000000),   -- 13% = 130000
  effective_from  date NOT NULL,
  effective_to    date,
  source_cite     text NOT NULL,                 -- canada.ca / ETA cite (verify)
  UNIQUE (tax_code, component, effective_from),
  CHECK (effective_to IS NULL OR effective_to > effective_from)
);

-- @module billing @retention RC-CONFIG @class terms:P1
CREATE TABLE billing.payment_profile (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name          text NOT NULL UNIQUE,
  terms_days    integer NOT NULL DEFAULT 30 CHECK (terms_days >= 0),
  grace_days    integer NOT NULL DEFAULT 0 CHECK (grace_days >= 0),
  interest_rate_ppm integer NOT NULL DEFAULT 0 CHECK (interest_rate_ppm >= 0),   -- annual, ppm (CF-01); postjudgment/prejudgment cap (verify Courts of Justice Act s.127-129)
  rate_source   text
);
ALTER TABLE party.contact ADD CONSTRAINT contact_payment_profile_fk FOREIGN KEY (payment_profile_id) REFERENCES billing.payment_profile(id);

-- @module billing @retention RC-CONFIG @class layout:P0
CREATE TABLE billing.bill_layout (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  name        text NOT NULL UNIQUE,
  logo_ref    text,
  columns     jsonb NOT NULL,
  grouping    text,
  summaries   jsonb NOT NULL DEFAULT '{}'::jsonb,
  footer      text,
  is_default  boolean NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX bill_layout_one_default ON billing.bill_layout ((true)) WHERE is_default;
ALTER TABLE party.contact ADD CONSTRAINT contact_bill_layout_fk FOREIGN KEY (bill_layout_id) REFERENCES billing.bill_layout(id);

-- @module billing @retention RC-FIN @class split:P1
CREATE TABLE billing.bill_split (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id       uuid NOT NULL REFERENCES matter.matter(id),
  payer_id        uuid NOT NULL REFERENCES party.contact(id),
  share_ppm       integer NOT NULL CHECK (share_ppm > 0 AND share_ppm <= 1000000),
  applies_to      text NOT NULL DEFAULT 'all' CHECK (applies_to IN ('all','fees','disbursements')),
  effective_from  date NOT NULL,
  UNIQUE (matter_id, payer_id, applies_to, effective_from)
);

-- Bill. Draft is mutable; from status 'approved' on, money columns are frozen (DM-041). Corrections via credit_note.
-- @module billing @retention RC-FIN @class amounts:P1,payer:P2
CREATE TABLE billing.bill (
  id                     uuid PRIMARY KEY DEFAULT core.uuidv7(),
  number                 text UNIQUE,                    -- assigned from core.number_series at approval (gapless)
  matter_id              uuid NOT NULL REFERENCES matter.matter(id),
  payer_id               uuid NOT NULL REFERENCES party.contact(id),
  split_parent_id        uuid REFERENCES billing.bill(id),
  status                 text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','preflight_failed','pending_approval','approved','delivered','partially_paid','paid','void','written_off','credited')),
  issue_date             date,                           -- := approval date in America/Toronto (BIL-C-121, CF-09)
  due_date               date,
  period                 daterange,
  fees_cents             bigint NOT NULL DEFAULT 0 CHECK (fees_cents >= 0),
  taxable_disb_cents     bigint NOT NULL DEFAULT 0 CHECK (taxable_disb_cents >= 0),
  nontaxable_disb_cents  bigint NOT NULL DEFAULT 0 CHECK (nontaxable_disb_cents >= 0),
  discount_cents         bigint NOT NULL DEFAULT 0 CHECK (discount_cents >= 0),
  tax_cents              bigint NOT NULL DEFAULT 0 CHECK (tax_cents >= 0),
  total_cents            bigint NOT NULL DEFAULT 0 CHECK (total_cents >= 0),
  tax_breakdown          jsonb NOT NULL DEFAULT '[]'::jsonb,      -- [{tax_code, component, rate_ppm, base_cents, tax_cents}]
  layout_id              uuid REFERENCES billing.bill_layout(id),
  approval_id            uuid,                                    -- G9 bill approval
  approved_by            uuid REFERENCES core.app_user(id),
  approved_at            timestamptz,
  signature_ref          text,
  signed_by              uuid REFERENCES core.app_user(id),      -- BIL-L-001 signer (CF-09)
  signed_at              timestamptz,
  signature_hash         bytea,
  interest_rate_ppm      integer CHECK (interest_rate_ppm >= 0), -- snapshot at approval; NULL = no interest (RV1-013, Solicitors Act s.33, verify)
  interest_terms_doc_id  uuid REFERENCES docs.document(id),      -- engagement clause the rate comes from
  place_of_supply        jsonb,                                  -- {jurisdiction, rule_applied, confirmed_by, confirmed_at} (BIL-C-201/207)
  place_of_supply_province char(2),
  pos_confirmed_by       uuid REFERENCES core.app_user(id),
  pdf_document_version_id uuid REFERENCES docs.document_version(id),
  delivered_at           timestamptz,
  delivery_channel       text CHECK (delivery_channel IN ('email','portal','mail','hand')),
  currency               char(3) NOT NULL DEFAULT 'CAD' CHECK (currency = 'CAD'),
  created_at             timestamptz NOT NULL DEFAULT now(),
  created_by             uuid REFERENCES core.app_user(id),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  updated_by             uuid REFERENCES core.app_user(id),
  row_version            integer NOT NULL DEFAULT 1,
  CHECK (total_cents = fees_cents + taxable_disb_cents + nontaxable_disb_cents - discount_cents + tax_cents),
  CHECK (discount_cents <= fees_cents + taxable_disb_cents),
  CHECK (status IN ('draft','preflight_failed','pending_approval','void') OR (number IS NOT NULL AND approved_by IS NOT NULL AND issue_date IS NOT NULL
         AND approval_id IS NOT NULL AND place_of_supply_province IS NOT NULL)),
  CHECK (status NOT IN ('delivered','partially_paid','paid','written_off','credited') OR delivered_at IS NOT NULL),
  CHECK (interest_rate_ppm IS NULL OR interest_terms_doc_id IS NOT NULL),
  CHECK (due_date IS NULL OR issue_date IS NULL OR due_date >= issue_date)
);
CREATE INDEX bill_matter_idx ON billing.bill (matter_id);
CREATE INDEX bill_open_idx ON billing.bill (payer_id) WHERE status IN ('delivered','partially_paid');
ALTER TABLE timekeeping.time_entry ADD CONSTRAINT time_entry_bill_fk FOREIGN KEY (bill_id) REFERENCES billing.bill(id);
ALTER TABLE timekeeping.expense    ADD CONSTRAINT expense_bill_fk    FOREIGN KEY (bill_id) REFERENCES billing.bill(id);
ALTER TABLE trust.withdrawal_request ADD CONSTRAINT withdrawal_bill_fk FOREIGN KEY (bill_id) REFERENCES billing.bill(id);

-- @module billing @retention RC-FIN @class narrative:P4,amounts:P1
CREATE TABLE billing.bill_line (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  bill_id           uuid NOT NULL REFERENCES billing.bill(id),
  ordinal           integer NOT NULL,
  source_type       text NOT NULL CHECK (source_type IN ('time','expense','flat','adjustment')),
  time_entry_id     uuid REFERENCES timekeeping.time_entry(id),
  expense_id        uuid REFERENCES timekeeping.expense(id),
  bill_voided       boolean NOT NULL DEFAULT false,      -- denormalised from bill.status='void' by billing.bill_void_lines (CF-08)
  narrative         text NOT NULL,
  quantity_milli    bigint NOT NULL CHECK (quantity_milli >= 0),   -- hours*1000 for time, units*1000 otherwise
  rate_cents        bigint NOT NULL CHECK (rate_cents >= 0),
  amount_cents      bigint NOT NULL,                              -- negative only for 'adjustment'
  tax_code          text NOT NULL,
  write_down_id     uuid REFERENCES timekeeping.write_down(id),
  agent_flags       text[] NOT NULL DEFAULT '{}',
  UNIQUE (bill_id, ordinal),
  CHECK (source_type <> 'time'    OR time_entry_id IS NOT NULL),
  CHECK (source_type <> 'expense' OR expense_id IS NOT NULL),
  CHECK (source_type = 'adjustment' OR amount_cents >= 0)
);
CREATE UNIQUE INDEX bill_line_time_once ON billing.bill_line (time_entry_id) WHERE time_entry_id IS NOT NULL AND NOT bill_voided;
CREATE UNIQUE INDEX bill_line_expense_once ON billing.bill_line (expense_id) WHERE expense_id IS NOT NULL AND NOT bill_voided;

-- Payment received (operating or trust). Trust receipts also produce trust.trust_transaction via allocation.
-- @module billing @retention RC-FIN @class payer:P2,amounts:P1
CREATE TABLE billing.payment (
  id                  uuid PRIMARY KEY DEFAULT core.uuidv7(),
  received_on         date NOT NULL,
  payer_contact_id    uuid REFERENCES party.contact(id),
  payer_name          text NOT NULL,
  amount_cents        bigint NOT NULL CHECK (amount_cents > 0),
  method              text NOT NULL CHECK (method IN ('cheque','eft','wire','cash','card','bank_draft','interac')),
  deposit_to          text NOT NULL CHECK (deposit_to IN ('operating','trust')),
  trust_account_id    uuid REFERENCES trust.trust_account(id),
  provider            text,
  provider_ref        text,
  processor_fee_cents bigint NOT NULL DEFAULT 0 CHECK (processor_fee_cents >= 0),
  status              text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','received','suggested','posted','unallocated','partially_refunded','refunded','failed','reversed')),
  currency            char(3) NOT NULL DEFAULT 'CAD' CHECK (currency = 'CAD'),
  created_at          timestamptz NOT NULL DEFAULT now(),
  created_by          uuid REFERENCES core.app_user(id),
  CHECK ((deposit_to = 'trust') = (trust_account_id IS NOT NULL)),
  CHECK (deposit_to = 'operating' OR processor_fee_cents = 0),       -- processor fees never deducted from trust (verify BL9)
  UNIQUE (provider, provider_ref)
);
ALTER TABLE trust.trust_transaction ADD CONSTRAINT trust_txn_payment_fk FOREIGN KEY (payment_id) REFERENCES billing.payment(id);
ALTER TABLE intake.consultation ADD CONSTRAINT consultation_payment_fk FOREIGN KEY (payment_id) REFERENCES billing.payment(id);

-- Append-only; un-allocation = negative row referencing the original.
-- @module billing @retention RC-FIN @class amounts:P1
CREATE TABLE billing.payment_allocation (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  payment_id       uuid NOT NULL REFERENCES billing.payment(id),
  target_type      text NOT NULL CHECK (target_type IN ('bill','trust_ledger','client_credit')),
  bill_id          uuid REFERENCES billing.bill(id),
  trust_ledger_id  uuid REFERENCES trust.client_ledger(id),
  client_id        uuid REFERENCES party.contact(id),
  amount_cents     bigint NOT NULL CHECK (amount_cents <> 0),
  fee_cents        bigint NOT NULL DEFAULT 0,          -- RV1-010 bucket split, same sign as amount_cents
  dtx_cents        bigint NOT NULL DEFAULT 0,          -- taxable disbursements
  dnt_cents        bigint NOT NULL DEFAULT 0,          -- non-taxable disbursements
  tax_cents        bigint NOT NULL DEFAULT 0,
  interest_cents   bigint NOT NULL DEFAULT 0,
  reverses_id      uuid UNIQUE REFERENCES billing.payment_allocation(id),
  allocated_by     uuid NOT NULL REFERENCES core.app_user(id),
  approval_id      uuid,
  at               timestamptz NOT NULL DEFAULT now(),
  CHECK (num_nonnulls(bill_id, trust_ledger_id, client_id) = 1),
  CHECK ((target_type = 'bill') = (bill_id IS NOT NULL)),
  CHECK ((target_type = 'trust_ledger') = (trust_ledger_id IS NOT NULL)),
  CHECK ((target_type = 'client_credit') = (client_id IS NOT NULL)),
  CHECK ((amount_cents < 0) = (reverses_id IS NOT NULL)),
  CHECK (target_type <> 'bill' OR fee_cents + dtx_cents + dnt_cents + tax_cents + interest_cents = amount_cents),
  CHECK (target_type = 'bill' OR (fee_cents = 0 AND dtx_cents = 0 AND dnt_cents = 0 AND tax_cents = 0 AND interest_cents = 0)),
  CHECK (sign(fee_cents) * sign(amount_cents) >= 0 AND sign(dtx_cents) * sign(amount_cents) >= 0 AND sign(dnt_cents) * sign(amount_cents) >= 0
         AND sign(tax_cents) * sign(amount_cents) >= 0 AND sign(interest_cents) * sign(amount_cents) >= 0)
);

-- Sum of allocations per payment never exceeds the payment (deferred).
CREATE OR REPLACE FUNCTION billing.check_allocation_total() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, billing AS $$
DECLARE alloc bigint; amt bigint;
BEGIN
  SELECT amount_cents INTO amt FROM billing.payment WHERE id = NEW.payment_id FOR UPDATE;
  SELECT coalesce(sum(amount_cents),0) INTO alloc FROM billing.payment_allocation WHERE payment_id = NEW.payment_id;
  IF alloc > amt OR alloc < 0 THEN
    RAISE EXCEPTION 'DM-045: allocations % exceed payment % on %', alloc, amt, NEW.payment_id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER payment_allocation_total AFTER INSERT ON billing.payment_allocation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION billing.check_allocation_total();

-- @module billing @retention RC-FIN @class amounts:P1
CREATE TABLE billing.refund (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  payment_id    uuid NOT NULL REFERENCES billing.payment(id),
  amount_cents  bigint NOT NULL CHECK (amount_cents > 0),
  reason        text NOT NULL,
  approval_id   uuid,
  trust_withdrawal_id uuid REFERENCES trust.withdrawal_request(id),
  status        text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','sent','failed')),
  created_at    timestamptz NOT NULL DEFAULT now()
);

-- Append-only once issued.
-- @module billing @retention RC-FIN @class amounts:P1
CREATE TABLE billing.credit_note (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  number        text UNIQUE,
  bill_id       uuid NOT NULL REFERENCES billing.bill(id),
  kind          text NOT NULL CHECK (kind IN ('fee_reduction','billing_error','bad_debt_write_off','refund')),
  amount_cents  bigint NOT NULL CHECK (amount_cents > 0),     -- pre-tax
  tax_cents     bigint NOT NULL DEFAULT 0 CHECK (tax_cents >= 0),
  reason        text NOT NULL,
  trust_effect  text NOT NULL DEFAULT 'none' CHECK (trust_effect IN ('none','refund_to_trust','refund_to_client')),
  approval_id   uuid,
  status        text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','pending_approval','issued','refund_pending','closed','rejected')),
  issued_at     timestamptz,
  CHECK (status IN ('draft','pending_approval','rejected') OR (number IS NOT NULL AND issued_at IS NOT NULL AND approval_id IS NOT NULL))
);

-- Client credit is derived: sum of payment_allocation where target_type='client_credit' (merged ClientCredit).
-- (client_credit_balance view is defined after billing.client_credit; see review addendum)

-- Bill balance (DM-042) is defined in the review addendum (needs write_off, interest_charge, trust_fee_transfer).

-- @module billing @retention RC-FIN @class link:P1
CREATE TABLE billing.payment_link (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  token_hash       bytea NOT NULL UNIQUE,
  purpose          text NOT NULL CHECK (purpose IN ('bill','retainer','consult_fee')),
  bill_id          uuid REFERENCES billing.bill(id),
  retainer_request_id uuid,
  amount_cents     bigint NOT NULL CHECK (amount_cents > 0),
  deposit_to       text NOT NULL CHECK (deposit_to IN ('operating','trust')),
  expires_at       timestamptz NOT NULL,
  status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active','paid','expired','revoked')),
  CHECK (purpose <> 'retainer' OR deposit_to = 'trust'),
  CHECK (purpose <> 'bill' OR bill_id IS NOT NULL)
);

-- @module billing @retention RC-FIN @class amounts:P1
CREATE TABLE billing.retainer_request (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id         uuid NOT NULL REFERENCES matter.matter(id),
  kind              text NOT NULL CHECK (kind IN ('initial','top_up','evergreen')),
  amount_cents      bigint NOT NULL CHECK (amount_cents > 0),
  reason            text,
  due_on            date,
  status            text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','sent','partially_funded','funded','cancelled')),
  approval_id       uuid
);
ALTER TABLE billing.payment_link ADD CONSTRAINT payment_link_retainer_fk FOREIGN KEY (retainer_request_id) REFERENCES billing.retainer_request(id);
-- funded amount is derived from trust receipts linked via payment_allocation → trust_ledger (no stored funded_amount).

-- @module billing @retention RC-FIN @class plan:P1
CREATE TABLE billing.payment_plan (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  client_id    uuid NOT NULL REFERENCES party.contact(id),
  instalments  jsonb NOT NULL,                     -- [{due_on, amount_cents}]
  bill_ids     uuid[] NOT NULL,
  status       text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','active','completed','defaulted','cancelled')),
  approval_id  uuid
);

-- @module billing @retention RC-FIN @class collections:P2
CREATE TABLE billing.collections_case (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  client_id        uuid NOT NULL REFERENCES party.contact(id),
  bill_ids         uuid[] NOT NULL,
  stage            text NOT NULL CHECK (stage IN ('reminder_1','reminder_2','final_notice','paused','plan','assessment','closed')),
  next_step_at     timestamptz,
  paused_reason    text,
  payment_plan_id  uuid REFERENCES billing.payment_plan(id),
  CHECK (stage <> 'paused' OR paused_reason IS NOT NULL)
);

-- @module billing @retention RC-FIN @class reminder:P2
CREATE TABLE billing.collection_reminder (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  case_id      uuid NOT NULL REFERENCES billing.collections_case(id),
  step         integer NOT NULL,
  body_ref     text NOT NULL,
  status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','sent','cancelled')),
  approval_id  uuid,
  sent_at      timestamptz,
  UNIQUE (case_id, step)
);

-- Frozen money once approved (DM-041).
CREATE OR REPLACE FUNCTION billing.bill_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF OLD.status <> 'draft' THEN RAISE EXCEPTION 'DM-010: only draft bills may be deleted'; END IF;
    RETURN OLD;
  END IF;
  -- RV1-008: delivered_at is a legal clock. NULL -> value (<= now) once; later changes only via an approved delivery_correction.
  IF NEW.delivered_at IS DISTINCT FROM OLD.delivered_at THEN
    IF OLD.delivered_at IS NULL AND NEW.delivered_at IS NOT NULL THEN
      IF NEW.delivered_at > now() THEN RAISE EXCEPTION 'DM-041: delivered_at % is in the future', NEW.delivered_at; END IF;
    ELSIF NOT EXISTS (SELECT 1 FROM billing.delivery_correction dc JOIN gate.approval_request ar ON ar.id = dc.approval_id AND ar.status = 'approved'
                       WHERE dc.bill_id = OLD.id AND dc.old_delivered_at = OLD.delivered_at
                         AND dc.new_delivered_at IS NOT DISTINCT FROM NEW.delivered_at AND dc.applied_at IS NULL) THEN
      RAISE EXCEPTION 'DM-041: delivered_at of bill % cannot be rewritten without an approved delivery_correction', OLD.id;
    END IF;
  END IF;
  IF NEW.status = 'void' AND OLD.status <> 'void' AND OLD.delivered_at IS NOT NULL THEN
    RAISE EXCEPTION 'DM-041: bill % was delivered; issue a credit note instead of voiding', OLD.id;
  END IF;
  IF OLD.status = 'void' AND NEW.status <> 'void' THEN RAISE EXCEPTION 'DM-041: void is terminal'; END IF;
  IF OLD.status NOT IN ('draft','preflight_failed','pending_approval') AND (
       NEW.fees_cents, NEW.taxable_disb_cents, NEW.nontaxable_disb_cents, NEW.discount_cents, NEW.tax_cents,
       NEW.total_cents, NEW.number, NEW.matter_id, NEW.payer_id, NEW.tax_breakdown, NEW.issue_date,
       NEW.due_date, NEW.approved_by, NEW.approved_at, NEW.approval_id, NEW.signature_ref, NEW.signed_by, NEW.signed_at,
       NEW.signature_hash, NEW.interest_rate_ppm, NEW.interest_terms_doc_id, NEW.place_of_supply, NEW.place_of_supply_province)
     IS DISTINCT FROM (
       OLD.fees_cents, OLD.taxable_disb_cents, OLD.nontaxable_disb_cents, OLD.discount_cents, OLD.tax_cents,
       OLD.total_cents, OLD.number, OLD.matter_id, OLD.payer_id, OLD.tax_breakdown, OLD.issue_date,
       OLD.due_date, OLD.approved_by, OLD.approved_at, OLD.approval_id, OLD.signature_ref, OLD.signed_by, OLD.signed_at,
       OLD.signature_hash, OLD.interest_rate_ppm, OLD.interest_terms_doc_id, OLD.place_of_supply, OLD.place_of_supply_province) THEN
    RAISE EXCEPTION 'DM-041: bill % is % and its amounts, dates, signer and terms are frozen; issue a credit note', OLD.id, OLD.status;
  END IF;
  -- RV2-018: approval needs an approved billing.bill_approval gate on this bill (G-B2).
  IF NEW.status = 'approved' AND OLD.status <> 'approved' AND NOT EXISTS (
       SELECT 1 FROM gate.approval_request ar WHERE ar.id = NEW.approval_id AND ar.status = 'approved'
          AND ar.gate_code = 'billing.bill_approval' AND ar.payload_type = 'billing.bill' AND ar.payload_id = NEW.id) THEN
    RAISE EXCEPTION 'DM-067: bill % needs an approved billing.bill_approval gate', NEW.id;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER bill_guard BEFORE UPDATE OR DELETE ON billing.bill FOR EACH ROW EXECUTE FUNCTION billing.bill_guard();

-- =====================================================================================
-- gl: operating/general double-entry ledger (merged GeneralJournalLine, VendorBill)
-- =====================================================================================

-- @module gl @retention RC-FIN @class chart:P0
CREATE TABLE gl.account (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  code        text NOT NULL UNIQUE,
  name        text NOT NULL,
  type        text NOT NULL CHECK (type IN ('asset','liability','equity','revenue','expense')),
  is_bank     boolean NOT NULL DEFAULT false,
  is_trust_control boolean NOT NULL DEFAULT false,  -- mirror of trust funds as liability; never posted to directly
  tax_code    text,
  active      boolean NOT NULL DEFAULT true
);
ALTER TABLE trust.bank_line ADD CONSTRAINT bank_line_gl_fk FOREIGN KEY (gl_account_id) REFERENCES gl.account(id);
ALTER TABLE trust.bank_line ADD CONSTRAINT bank_line_operating_ck CHECK ((account_kind = 'operating') = (gl_account_id IS NOT NULL));

-- @module gl @retention RC-FIN @class period:P0
CREATE TABLE gl.period (
  month        date PRIMARY KEY CHECK (extract(day FROM month) = 1),
  status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
  closed_by    uuid REFERENCES core.app_user(id),
  closed_at    timestamptz,
  approval_id  uuid                                  -- G12g
);

-- @module gl @retention RC-FIN @class vendor:P2
CREATE TABLE gl.vendor_bill (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  vendor        text NOT NULL,
  bill_date     date NOT NULL,
  due_date      date,
  amount_cents  bigint NOT NULL CHECK (amount_cents > 0),
  tax_cents     bigint NOT NULL DEFAULT 0 CHECK (tax_cents >= 0),
  matter_id     uuid REFERENCES matter.matter(id),
  document_id   uuid REFERENCES docs.document(id),
  status        text NOT NULL DEFAULT 'open' CHECK (status IN ('open','paid','void'))
);

-- Append-only journal header.
-- @module gl @retention RC-FIN @class journal:P1
CREATE TABLE gl.journal_entry (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  entry_date   date NOT NULL,
  memo         text NOT NULL,
  source_type  text NOT NULL CHECK (source_type IN ('bill','payment','payment_allocation','credit_note','expense','vendor_bill','write_off','interest_charge','trust_fee_transfer','nsf_replenishment','refund','manual','reversal','import')),
  source_id    uuid,
  reverses_id  uuid UNIQUE REFERENCES gl.journal_entry(id),
  posted_by    uuid NOT NULL REFERENCES core.app_user(id),
  posted_at    timestamptz NOT NULL DEFAULT now(),
  CHECK ((source_type = 'reversal') = (reverses_id IS NOT NULL)),
  CHECK (source_type IN ('manual','import') OR source_id IS NOT NULL)
);
CREATE UNIQUE INDEX journal_once_per_source ON gl.journal_entry (source_type, source_id) WHERE reverses_id IS NULL AND source_id IS NOT NULL;   -- RV2-004

-- @module gl @retention RC-FIN @class journal:P1
CREATE TABLE gl.journal_line (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  entry_id         uuid NOT NULL REFERENCES gl.journal_entry(id),
  account_id       uuid NOT NULL REFERENCES gl.account(id),
  debit_cents      bigint NOT NULL DEFAULT 0 CHECK (debit_cents >= 0),
  credit_cents     bigint NOT NULL DEFAULT 0 CHECK (credit_cents >= 0),
  matter_id        uuid REFERENCES matter.matter(id),
  party            text,
  tax_code         text,
  bank_line_id     uuid REFERENCES trust.bank_line(id),
  CHECK ((debit_cents = 0) <> (credit_cents = 0))
);
CREATE INDEX journal_line_account_idx ON gl.journal_line (account_id);

CREATE OR REPLACE FUNCTION gl.before_entry() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM gl.period p WHERE p.month = date_trunc('month', NEW.entry_date)::date AND p.status = 'closed') THEN
    RAISE EXCEPTION 'DM-062: GL period % is closed', date_trunc('month', NEW.entry_date)::date;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER journal_entry_period BEFORE INSERT ON gl.journal_entry FOR EACH ROW EXECUTE FUNCTION gl.before_entry();

CREATE OR REPLACE FUNCTION gl.check_entry_balanced() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d bigint; c bigint; n int;
BEGIN
  SELECT coalesce(sum(debit_cents),0), coalesce(sum(credit_cents),0), count(*) INTO d, c, n
    FROM gl.journal_line WHERE entry_id = NEW.entry_id;
  IF d <> c OR n < 2 THEN
    RAISE EXCEPTION 'DM-061: journal entry % unbalanced (debits %, credits %, lines %)', NEW.entry_id, d, c, n
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER journal_balanced AFTER INSERT ON gl.journal_line
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION gl.check_entry_balanced();

CREATE TRIGGER journal_entry_immutable BEFORE UPDATE OR DELETE ON gl.journal_entry FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER journal_line_immutable  BEFORE UPDATE OR DELETE ON gl.journal_line  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

-- =====================================================================================
-- comms: communication items (email, SMS, portal, calls), portal access, threads
-- =====================================================================================

-- @module comms @retention RC-CONFIG @class template:P0
CREATE TABLE comms.message_template (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  key          text NOT NULL,
  version      integer NOT NULL CHECK (version > 0),
  skill_id     uuid,                         -- FK added after agent.skill
  channel      text NOT NULL CHECK (channel IN ('email','sms','portal','letter')),
  body_ref     text NOT NULL,
  approved_by  uuid REFERENCES core.app_user(id),
  approved_at  timestamptz,
  UNIQUE (key, version)
);

-- @module comms @retention RC-AUDIT @class sms_consent:P2
CREATE TABLE comms.text_conversation (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  contact_id      uuid NOT NULL REFERENCES party.contact(id),
  firm_number     text NOT NULL,
  consent_state   text NOT NULL DEFAULT 'none' CHECK (consent_state IN ('none','opted_in','opted_out')),
  consent_at      timestamptz,
  consent_method  text,
  UNIQUE (contact_id, firm_number)
);

-- Merged: platform.CommItem + intake.Communication + platform.PortalMessage + documents.EmailFiling.
-- Bodies are refs only (DM-020).
-- @module comms @retention RC-FILE @class participants:P2,body:P4
CREATE TABLE comms.comm_item (
  id                     uuid PRIMARY KEY DEFAULT core.uuidv7(),
  channel                text NOT NULL CHECK (channel IN ('email','sms','portal','call','voicemail','letter','fax')),
  direction              text NOT NULL CHECK (direction IN ('inbound','outbound','internal')),
  lead_id                uuid REFERENCES intake.lead(id),
  connector_id           uuid,                          -- FK added after platform.connector
  external_id            text,
  thread_ref             text,
  participants           jsonb NOT NULL,
  display_subject        text,
  body_ref               text,
  summary_ref            text,
  summary_confirmed_by   uuid REFERENCES core.app_user(id),
  privilege_flag         boolean NOT NULL DEFAULT false,
  template_id            uuid REFERENCES comms.message_template(id),
  drafted_by_agent_run_id uuid,
  approval_id            uuid,                          -- outbound client comms gate
  audience               text CHECK (audience IN ('client','team','external')),
  sent_at                timestamptz,
  delivery_status        text CHECK (delivery_status IN ('queued','sent','delivered','bounced','failed')),
  failure_reason         text,
  excluded_hash          bytea,                         -- personal mail excluded from sync: only a hash kept
  text_conversation_id   uuid REFERENCES comms.text_conversation(id),
  occurred_at            timestamptz NOT NULL,
  created_at             timestamptz NOT NULL DEFAULT now(),
  UNIQUE (connector_id, external_id),
  CHECK (excluded_hash IS NULL OR (body_ref IS NULL AND display_subject IS NULL)),
  CHECK (direction <> 'outbound' OR drafted_by_agent_run_id IS NULL OR approval_id IS NOT NULL)
);

-- Filing of a comm item to one or more matters (merged EmailFiling rule/confidence).
-- @module comms @retention RC-FILE @class filing:P1
CREATE TABLE comms.comm_filing (
  comm_item_id   uuid NOT NULL REFERENCES comms.comm_item(id),
  matter_id      uuid NOT NULL REFERENCES matter.matter(id),
  rule           text,
  confidence_ppm integer CHECK (confidence_ppm BETWEEN 0 AND 1000000),
  status         text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','filed','rejected')),
  filed_by       uuid REFERENCES core.app_user(id),
  filed_at       timestamptz,
  PRIMARY KEY (comm_item_id, matter_id)
);

-- @module comms @retention RC-AUDIT @class portal:P2
CREATE TABLE comms.portal_access (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  contact_id    uuid NOT NULL REFERENCES party.contact(id),
  matter_id     uuid NOT NULL REFERENCES matter.matter(id),
  role          text NOT NULL CHECK (role IN ('client','co_client','guest')),
  invited_at    timestamptz NOT NULL DEFAULT now(),
  invited_by    uuid NOT NULL REFERENCES core.app_user(id),
  last_login_at timestamptz,
  mfa           boolean NOT NULL DEFAULT true,
  revoked_at    timestamptz
);
CREATE UNIQUE INDEX portal_access_active_uq ON comms.portal_access (contact_id, matter_id) WHERE revoked_at IS NULL;

-- @module comms @retention RC-FILE @class thread:P1
CREATE TABLE comms.internal_thread (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id     uuid NOT NULL REFERENCES matter.matter(id),
  participants  uuid[] NOT NULL,
  guests        jsonb NOT NULL DEFAULT '[]'::jsonb,
  guest_expiry  timestamptz
);

-- =====================================================================================
-- agent: skills, agent runs (merged platform.AgentRun + work.AgentRun)
-- =====================================================================================

-- @module agent @retention RC-CONFIG @class config:P0
CREATE TABLE agent.skill (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  key            text NOT NULL,
  version        text NOT NULL,
  area           text NOT NULL,
  enabled        boolean NOT NULL DEFAULT false,
  autonomy       text NOT NULL DEFAULT 'suggest' CHECK (autonomy IN ('off','suggest','draft','act_with_approval')),
  autonomy_cap   text NOT NULL DEFAULT 'act_with_approval' CHECK (autonomy_cap IN ('off','suggest','draft','act_with_approval')),
  approver_rule  jsonb NOT NULL DEFAULT '{}'::jsonb,
  sla_hours      integer CHECK (sla_hours > 0),
  schedule       text,
  eval_score_ppm integer CHECK (eval_score_ppm BETWEEN 0 AND 1000000),
  UNIQUE (key, version)
);
ALTER TABLE comms.message_template ADD CONSTRAINT message_template_skill_fk FOREIGN KEY (skill_id) REFERENCES agent.skill(id);

-- @module agent @retention RC-AUDIT @class plan:P4
CREATE TABLE agent.agent_run (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  skill_id         uuid REFERENCES agent.skill(id),
  on_behalf_of     uuid NOT NULL REFERENCES core.app_user(id),       -- agent acts as this human (DM-030)
  matter_id        uuid REFERENCES matter.matter(id),
  work_item_id     uuid REFERENCES work.work_item(id),
  goal             text NOT NULL,
  plan_steps       jsonb NOT NULL DEFAULT '[]'::jsonb,
  step_trace_ref   text,
  state            text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','waiting_approval','done','failed','stopped')),
  started_at       timestamptz,
  ended_at         timestamptz,
  stopped_by       uuid REFERENCES core.app_user(id),
  output_ref       text,
  log_ref          text,
  sources          jsonb NOT NULL DEFAULT '[]'::jsonb,
  cost_micro_usd   bigint NOT NULL DEFAULT 0 CHECK (cost_micro_usd >= 0),   -- model cost; not client money
  model_provider   text,
  model_region     text CHECK (model_region IS NULL OR model_region LIKE 'ca-%' OR model_region = 'canada'),   -- merged ResidencyLogEntry
  runtime_session_ref text,                                                 -- Paploo session id
  CHECK (state <> 'stopped' OR stopped_by IS NOT NULL),
  CHECK (ended_at IS NULL OR started_at IS NULL OR ended_at >= started_at)
);
CREATE INDEX agent_run_matter_idx ON agent.agent_run (matter_id, started_at);

-- =====================================================================================
-- gate: approval requests and append-only decisions
-- Merged: platform.ApprovalItem + work.Approval + documents.Review decision + trust GateDecision + intake approvals.
-- =====================================================================================

-- @module gate @retention RC-AUDIT @class payload_ref:P1
CREATE TABLE gate.approval_request (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  gate_code         text NOT NULL CHECK (gate_code ~ '^[a-z]+\.[a-z_]+$'),    -- gate kind slug <module>.<slug>, e.g. billing.bill_approval (RV2-001, CF-04)
  spec_gate         text NOT NULL CHECK (spec_gate ~ '^G(-?[A-Z0-9]+)+[a-z]?$'), -- spec id: G9, G11t, G6c, GA1, G-B2, G-IMP, G-TSIG, G-TREL
  version           integer NOT NULL DEFAULT 1 CHECK (version >= 1),           -- payload version; an edit supersedes (RV2-002)
  supersedes_id     uuid UNIQUE REFERENCES gate.approval_request(id),
  matter_id         uuid REFERENCES matter.matter(id),
  payload_type      text NOT NULL,           -- 'billing.bill', 'trust.withdrawal_request', ...
  payload_id        uuid NOT NULL,
  payload_hash      bytea NOT NULL,          -- sha256 of canonical payload at request time; decision binds to it
  requested_by      uuid NOT NULL REFERENCES core.app_user(id),
  agent_run_id      uuid REFERENCES agent.agent_run(id),
  eligible_role     text,
  assigned_to       uuid REFERENCES core.app_user(id),
  required_approvals smallint NOT NULL DEFAULT 1 CHECK (required_approvals BETWEEN 1 AND 3),
  due_at            timestamptz,
  risk              text CHECK (risk IN ('low','medium','high','irreversible')),
  sources           jsonb NOT NULL DEFAULT '[]'::jsonb,
  diff_ref          text,
  status            text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','changes_requested','withdrawn','expired','superseded')),
  created_at        timestamptz NOT NULL DEFAULT now(),
  expires_at        timestamptz NOT NULL,                  -- created_at + gate.gate_kind.ttl_hours (RV2-003)
  closed_at         timestamptz,
  row_version       integer NOT NULL DEFAULT 1,
  CHECK (assigned_to IS DISTINCT FROM requested_by OR assigned_to IS NULL),
  CHECK (expires_at > created_at),
  CHECK (risk IS DISTINCT FROM 'irreversible' OR required_approvals >= 2),
  CHECK ((supersedes_id IS NULL) = (version = 1))
);
CREATE INDEX approval_request_pending_idx ON gate.approval_request (assigned_to, due_at) WHERE status = 'pending';
CREATE UNIQUE INDEX approval_request_one_pending ON gate.approval_request (gate_code, payload_type, payload_id) WHERE status = 'pending';

-- Append-only. Only humans decide (decided_by → app_user; agents have no user row).
-- @module gate @retention RC-AUDIT @class decision:P1
CREATE TABLE gate.gate_decision (
  id                     uuid PRIMARY KEY DEFAULT core.uuidv7(),
  approval_id            uuid NOT NULL REFERENCES gate.approval_request(id),
  decided_by             uuid NOT NULL REFERENCES core.app_user(id),
  decision               text NOT NULL CHECK (decision IN ('approve','reject','changes_requested')),
  reason                 text,
  payload_hash           bytea NOT NULL,
  self_approval_exception boolean NOT NULL DEFAULT false,      -- sole practitioner declared exception
  effective_at           timestamptz NOT NULL DEFAULT now(),   -- later than decided_at when cooling-off applies
  decided_at             timestamptz NOT NULL DEFAULT now(),
  mfa_verified_at        timestamptz NOT NULL,                 -- step-up auth for every decision
  UNIQUE (approval_id, decided_by),
  CHECK (decision = 'approve' OR reason IS NOT NULL),
  CHECK (effective_at >= decided_at)
);

CREATE OR REPLACE FUNCTION gate.check_decision() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, gate, core, agent, audit AS $$
DECLARE r gate.approval_request%ROWTYPE; u core.app_user%ROWTYPE; sole boolean; run_owner uuid; approvals int;
BEGIN
  SELECT * INTO r FROM gate.approval_request WHERE id = NEW.approval_id FOR UPDATE;
  IF r.status <> 'pending' THEN
    RAISE EXCEPTION 'DM-071: approval % is %, not pending', r.id, r.status;
  END IF;
  IF now() >= r.expires_at THEN                                  -- RV2-003 (the gate.expire job persists 'expired')
    RAISE EXCEPTION 'DM-076: approval % expired at %', r.id, r.expires_at USING ERRCODE = 'P0001';
  END IF;
  IF NEW.payload_hash <> r.payload_hash THEN
    RAISE EXCEPTION 'DM-072: decision payload hash does not match the request (payload changed)';
  END IF;
  SELECT * INTO u FROM core.app_user WHERE id = NEW.decided_by;   -- RV2-017
  IF u.principal_kind <> 'human' OR u.is_system_admin OR u.status <> 'active' THEN
    RAISE EXCEPTION 'DM-075: agent_cannot_decide: % (kind %, admin %) may not decide gates', NEW.decided_by, u.principal_kind, u.is_system_admin;
  END IF;
  SELECT on_behalf_of INTO run_owner FROM agent.agent_run WHERE id = r.agent_run_id;
  IF NEW.decided_by = r.requested_by OR NEW.decided_by = run_owner THEN
    SELECT sole_practitioner INTO sole FROM core.firm;
    IF NOT (coalesce(sole, false) AND NEW.self_approval_exception) THEN
      RAISE EXCEPTION 'DM-070: maker cannot approve own item (approval %)', r.id;
    END IF;
    IF r.gate_code IN ('trust.withdrawal','trust.transfer','trust.reconciliation_signoff','trust.regulatory_release')
       AND NEW.effective_at < NEW.decided_at + interval '24 hours' THEN
      RAISE EXCEPTION 'DM-073: self-approval on % requires a 24h cooling-off (verify policy)', r.gate_code;
    END IF;
    INSERT INTO audit.audit_event(stream, actor_kind, actor_user_id, action, object_type, object_id, matter_id, approval_id, summary)
    VALUES ('gate', 'user', NEW.decided_by, 'sod_solo_exception', r.payload_type, r.payload_id, r.matter_id, r.id,
            'sole-practitioner self-approval on ' || r.gate_code);
  ELSIF NEW.self_approval_exception THEN
    RAISE EXCEPTION 'DM-070: self_approval_exception set but decider is not the maker';
  END IF;
  PERFORM set_config('counsel.gate_tx', 'on', true);
  IF NEW.decision <> 'approve' THEN
    UPDATE gate.approval_request SET status = CASE NEW.decision WHEN 'reject' THEN 'rejected' ELSE 'changes_requested' END,
                                     closed_at = now() WHERE id = r.id;
  ELSE
    -- RV2-002: count only approvals bound to this exact payload version.
    SELECT count(*) + 1 INTO approvals FROM gate.gate_decision
     WHERE approval_id = r.id AND decision = 'approve' AND payload_hash = r.payload_hash;
    IF approvals >= r.required_approvals THEN
      UPDATE gate.approval_request SET status = 'approved', closed_at = now() WHERE id = r.id;
    END IF;
  END IF;
  PERFORM set_config('counsel.gate_tx', 'off', true);
  RETURN NEW;
END $$;
CREATE TRIGGER gate_decision_check BEFORE INSERT ON gate.gate_decision FOR EACH ROW EXECUTE FUNCTION gate.check_decision();
CREATE TRIGGER gate_decision_immutable BEFORE UPDATE OR DELETE ON gate.gate_decision FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

-- =====================================================================================
-- audit: single hash-chained event log
-- Merged: conflicts.ComplianceAuditEvent, trust.TrustAuditEvent, documents.DocumentEvent,
--         matters.Activity, platform.AgentEvent, access.AccessEvent, readiness.ResidencyLogEntry.
-- =====================================================================================

-- @module audit @retention RC-AUDIT @class detail_masked:P1
CREATE TABLE audit.audit_event (
  seq             bigint PRIMARY KEY,                     -- assigned under lock in trigger (gapless chain order)
  id              uuid NOT NULL UNIQUE DEFAULT core.uuidv7(),
  stream          text NOT NULL CHECK (stream IN ('access','compliance','trust','billing','document','matter','agent','gate','platform')),
  at              timestamptz NOT NULL DEFAULT clock_timestamp(),
  actor_kind      text NOT NULL CHECK (actor_kind IN ('user','agent','service','system','client','support')),
  actor_user_id   uuid REFERENCES core.app_user(id),
  on_behalf_of    uuid REFERENCES core.app_user(id),
  agent_run_id    uuid REFERENCES agent.agent_run(id),
  action          text NOT NULL,                          -- e.g. 'trust.withdrawal.authorized'
  object_type     text NOT NULL,
  object_id       uuid,
  matter_id       uuid REFERENCES matter.matter(id),
  approval_id     uuid REFERENCES gate.approval_request(id),
  outcome         text NOT NULL DEFAULT 'ok' CHECK (outcome IN ('ok','denied','error')),
  purpose         text,
  summary         text,                                   -- timeline line; no privileged content
  detail_masked   jsonb NOT NULL DEFAULT '{}'::jsonb,     -- before/after with secrets & PII masked
  ip              inet,
  prev_hash       bytea NOT NULL,
  hash            bytea NOT NULL,
  CHECK (actor_kind <> 'agent' OR (agent_run_id IS NOT NULL AND on_behalf_of IS NOT NULL)),
  CHECK (actor_kind <> 'user'  OR actor_user_id IS NOT NULL)
);
CREATE INDEX audit_matter_idx ON audit.audit_event (matter_id, seq) WHERE matter_id IS NOT NULL;
CREATE INDEX audit_object_idx ON audit.audit_event (object_type, object_id);

CREATE OR REPLACE FUNCTION audit.chain() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, audit AS $$
DECLARE last_seq bigint; last_hash bytea;
BEGIN
  PERFORM pg_advisory_xact_lock(7300001);                 -- one writer at a time; held to commit
  SELECT seq, hash INTO last_seq, last_hash FROM audit.audit_event ORDER BY seq DESC LIMIT 1;
  NEW.seq       := coalesce(last_seq, 0) + 1;
  NEW.prev_hash := coalesce(last_hash, '\x00'::bytea);
  NEW.hash      := sha256(NEW.prev_hash || convert_to(jsonb_build_object(
                     'seq', NEW.seq, 'id', NEW.id, 'stream', NEW.stream, 'at', NEW.at, 'actor_kind', NEW.actor_kind,
                     'actor', NEW.actor_user_id, 'obo', NEW.on_behalf_of, 'run', NEW.agent_run_id, 'action', NEW.action,
                     'otype', NEW.object_type, 'oid', NEW.object_id, 'matter', NEW.matter_id, 'approval', NEW.approval_id,
                     'outcome', NEW.outcome, 'detail', NEW.detail_masked)::text, 'UTF8'));
  RETURN NEW;
END $$;
CREATE TRIGGER audit_chain BEFORE INSERT ON audit.audit_event FOR EACH ROW EXECUTE FUNCTION audit.chain();
CREATE TRIGGER audit_immutable BEFORE UPDATE OR DELETE ON audit.audit_event FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER audit_no_truncate BEFORE TRUNCATE ON audit.audit_event FOR EACH STATEMENT EXECUTE FUNCTION core.forbid_mutation();

-- Matter activity timeline is a projection of the audit chain (merged matters.Activity).
CREATE VIEW matter.activity AS
  SELECT seq, at, matter_id, stream, actor_kind, actor_user_id, on_behalf_of, action, summary, object_type, object_id, approval_id
    FROM audit.audit_event WHERE matter_id IS NOT NULL;

-- @module audit @retention RC-AUDIT @class breakglass:P1
CREATE TABLE audit.break_glass (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id         uuid NOT NULL REFERENCES core.app_user(id),
  matter_id       uuid NOT NULL REFERENCES matter.matter(id),
  reason          text NOT NULL,
  explanation     text NOT NULL,
  starts_at       timestamptz NOT NULL DEFAULT now(),
  ends_at         timestamptz NOT NULL,
  reviewer_id     uuid REFERENCES core.app_user(id),
  review_outcome  text CHECK (review_outcome IN ('justified','not_justified')),
  reviewed_at     timestamptz,
  CHECK (ends_at > starts_at AND ends_at <= starts_at + interval '8 hours'),
  CHECK (reviewer_id IS DISTINCT FROM user_id OR reviewer_id IS NULL)
);

-- @module audit @retention RC-AUDIT @class review:P1
CREATE TABLE audit.access_review (
  id         uuid PRIMARY KEY DEFAULT core.uuidv7(),
  period     daterange NOT NULL,
  due_on     date NOT NULL,
  status     text NOT NULL DEFAULT 'open' CHECK (status IN ('open','certified','overdue')),
  items      jsonb NOT NULL DEFAULT '[]'::jsonb,
  closed_by  uuid REFERENCES core.app_user(id),
  closed_at  timestamptz
);

-- @module audit @retention RC-AUDIT @class grant:P1
CREATE TABLE audit.support_grant (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  reason       text NOT NULL,
  scope        text NOT NULL CHECK (scope IN ('metadata','config','matter_readonly')),
  matter_id    uuid REFERENCES matter.matter(id),
  granted_by   uuid NOT NULL REFERENCES core.app_user(id),
  approval_id  uuid REFERENCES gate.approval_request(id),
  starts_at    timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  revoked_at   timestamptz,
  CHECK (expires_at > starts_at AND expires_at <= starts_at + interval '72 hours'),
  CHECK (scope <> 'matter_readonly' OR matter_id IS NOT NULL)
);

-- @module audit @retention RC-AUDIT @class duty:P0
CREATE TABLE audit.duty_rule (
  id                  text PRIMARY KEY,
  text                text NOT NULL,
  locked              boolean NOT NULL DEFAULT true,
  exception           text,
  exception_expires   date,
  CHECK (exception IS NULL OR (NOT locked AND exception_expires IS NOT NULL))
);

-- @module audit @retention RC-PERSONNEL @class offboarding:P1
CREATE TABLE audit.offboarding_plan (
  id         uuid PRIMARY KEY DEFAULT core.uuidv7(),
  user_id    uuid NOT NULL REFERENCES core.app_user(id),
  last_day   date NOT NULL,
  items      jsonb NOT NULL,
  status     text NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','in_progress','cut_off','done')),
  cutoff_by  uuid REFERENCES core.app_user(id),
  cutoff_at  timestamptz,
  CHECK (cutoff_by IS DISTINCT FROM user_id OR cutoff_by IS NULL)
);

-- =====================================================================================
-- platform: connectors, API keys, webhooks, import/export, reports, readiness, subscription
-- =====================================================================================

-- Merged: platform.Connector + readiness.Connector + work.SyncConnection + deadlines.CalendarConnection + EmailSettings.
-- @module platform @retention RC-CONFIG @class credential_ref:P3
CREATE TABLE platform.connector (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  kind             text NOT NULL,                 -- 'mail','calendar','accounting','payments','esign','storage','work_sync','sms','telephony'
  provider         text NOT NULL,
  user_id          uuid REFERENCES core.app_user(id),      -- null = firm-level
  direction        text NOT NULL CHECK (direction IN ('in','out','both')),
  scopes           text[] NOT NULL DEFAULT '{}',
  credential_ref   text NOT NULL,                 -- vault path; never the secret (DM-021)
  data_location    text NOT NULL CHECK (data_location IN ('canada','us','other')),
  settings         jsonb NOT NULL DEFAULT '{}'::jsonb,     -- maps (scope/status/field/tax code), title_privacy, sync_scope, signature
  status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','error','revoked')),
  approval_id      uuid REFERENCES gate.approval_request(id),
  last_sync_at     timestamptz,
  CHECK (status <> 'active' OR approval_id IS NOT NULL OR user_id IS NOT NULL)
);
ALTER TABLE comms.comm_item ADD CONSTRAINT comm_item_connector_fk FOREIGN KEY (connector_id) REFERENCES platform.connector(id);

-- Merged: MerchantAccount.
-- @module platform @retention RC-FIN @class merchant:P1
CREATE TABLE platform.merchant_account (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  purpose          text NOT NULL CHECK (purpose IN ('trust','operating')),
  connector_id     uuid NOT NULL REFERENCES platform.connector(id),
  trust_account_id uuid REFERENCES trust.trust_account(id),
  gl_account_id    uuid REFERENCES gl.account(id),
  status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','disabled')),
  CHECK ((purpose = 'trust') = (trust_account_id IS NOT NULL)),
  CHECK ((purpose = 'operating') = (gl_account_id IS NOT NULL))
);

-- Merged: ApiKey + OAuthApp grants.
-- @module platform @retention RC-AUDIT @class key_hash:P3
CREATE TABLE platform.api_credential (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  kind          text NOT NULL CHECK (kind IN ('api_key','oauth_app')),
  name          text NOT NULL,
  scopes        text[] NOT NULL,
  secret_hash   bytea NOT NULL,
  redirect_uris text[],
  deauth_url    text,
  created_by    uuid NOT NULL REFERENCES core.app_user(id),
  expires_at    timestamptz NOT NULL,
  last_used_at  timestamptz,
  revoked_at    timestamptz
);

-- @module platform @retention RC-CONFIG @class secret_ref:P3
CREATE TABLE platform.webhook (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  url         text NOT NULL CHECK (url LIKE 'https://%'),
  events      text[] NOT NULL,
  model       text,
  fields      text[],
  secret_ref  text NOT NULL,
  status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','expired')),
  expires_at  timestamptz                       -- NULL: firm-owned endpoint never expires (RV2-013)
);

-- @module platform @retention RC-OPS @class delivery:P1
CREATE TABLE platform.webhook_delivery (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  webhook_id   uuid NOT NULL REFERENCES platform.webhook(id),
  event        text NOT NULL,
  attempt      integer NOT NULL DEFAULT 1,
  status_code  integer,
  at           timestamptz NOT NULL DEFAULT now()
);

-- @module platform @retention RC-AUDIT @class import:P1
CREATE TABLE platform.import_job (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  source             text NOT NULL,
  mode               text NOT NULL CHECK (mode IN ('dry_run','full','delta')),
  cutover_at         timestamptz,
  counts             jsonb NOT NULL DEFAULT '{}'::jsonb,
  issues             jsonb NOT NULL DEFAULT '[]'::jsonb,
  trust_evidence     jsonb,                      -- opening balances tie-out per ledger
  status             text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','running','review','committed','failed','rolled_back')),
  approval_id        uuid REFERENCES gate.approval_request(id),
  created_by         uuid NOT NULL REFERENCES core.app_user(id)
);

-- @module platform @retention RC-AUDIT @class export:P1
CREATE TABLE platform.export_job (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  scope            jsonb NOT NULL,
  requested_by     uuid NOT NULL REFERENCES core.app_user(id),
  approval_id      uuid REFERENCES gate.approval_request(id),
  status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','running','ready','expired','failed')),
  manifest_sha256  bytea,
  link_expires_at  timestamptz,
  CHECK (status NOT IN ('running','ready') OR approval_id IS NOT NULL)
);

-- @module platform @retention RC-CONFIG @class report:P0
CREATE TABLE platform.report_definition (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  report_group  text NOT NULL,
  name          text NOT NULL,
  dataset       text NOT NULL,
  definition    jsonb NOT NULL,
  visibility    text NOT NULL DEFAULT 'private' CHECK (visibility IN ('private','role','firm')),
  owner_id      uuid REFERENCES core.app_user(id),
  UNIQUE (report_group, name)
);

-- @module platform @retention RC-OPS @class report:P1
CREATE TABLE platform.report_schedule (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  report_id    uuid NOT NULL REFERENCES platform.report_definition(id),
  filters      jsonb NOT NULL DEFAULT '{}'::jsonb,
  cadence      text NOT NULL CHECK (cadence IN ('daily','weekly','monthly','quarterly')),
  run_time     time NOT NULL,
  recipients   uuid[] NOT NULL,
  starts_on    date NOT NULL,
  ends_on      date,
  format       text NOT NULL CHECK (format IN ('pdf','csv','xlsx'))
);

-- @module platform @retention RC-AUDIT @class report:P1
CREATE TABLE platform.report_run (
  id         uuid PRIMARY KEY DEFAULT core.uuidv7(),
  report_id  uuid NOT NULL REFERENCES platform.report_definition(id),
  run_by     uuid REFERENCES core.app_user(id),
  at         timestamptz NOT NULL DEFAULT now(),
  filters    jsonb NOT NULL,
  row_count  integer NOT NULL,
  format     text NOT NULL,
  file_ref   text,
  sha256     bytea
);

-- @module platform @retention RC-CONFIG @class plan:P1
CREATE TABLE platform.subscription (
  singleton              boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  plan                   text NOT NULL,
  seats                  integer NOT NULL CHECK (seats > 0),
  agent_budget_micro_usd bigint NOT NULL CHECK (agent_budget_micro_usd >= 0),
  status                 text NOT NULL CHECK (status IN ('trial','active','past_due','cancelled'))
);

-- Readiness: certifications, sub-processors, escrow runs, service credits.
-- @module platform @retention RC-PERM @class trust_center:P0
CREATE TABLE platform.readiness_record (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  kind        text NOT NULL CHECK (kind IN ('certification','sub_processor','escrow_run','service_credit')),
  name        text NOT NULL,
  status      text NOT NULL,
  data        jsonb NOT NULL,             -- kind-specific: issued/expires, country/purpose, bytes/hash/restore_tested, month/uptime/credit_cents
  approved_by uuid REFERENCES core.app_user(id),
  at          timestamptz NOT NULL DEFAULT now()
);

-- =====================================================================================
-- Deferred cross-module FKs
-- =====================================================================================
ALTER TABLE core.temp_grant        ADD CONSTRAINT temp_grant_matter_fk   FOREIGN KEY (matter_id) REFERENCES matter.matter(id);
ALTER TABLE core.temp_grant        ADD CONSTRAINT temp_grant_approval_fk FOREIGN KEY (approval_id) REFERENCES gate.approval_request(id);
ALTER TABLE matter.matter          ADD CONSTRAINT matter_lead_fk         FOREIGN KEY (source_lead_id) REFERENCES intake.lead(id);
ALTER TABLE matter.party_role      ADD CONSTRAINT party_role_lead_fk     FOREIGN KEY (lead_id) REFERENCES intake.lead(id);
ALTER TABLE matter.party_role      ADD CONSTRAINT party_role_check_fk    FOREIGN KEY (conflict_check_id) REFERENCES conflict.conflict_check(id);
ALTER TABLE matter.ethical_wall    ADD CONSTRAINT wall_clearance_fk      FOREIGN KEY (origin_clearance_id) REFERENCES conflict.clearance(id);
ALTER TABLE matter.ethical_wall    ADD CONSTRAINT wall_approval_fk       FOREIGN KEY (approval_id) REFERENCES gate.approval_request(id);
ALTER TABLE matter.note            ADD CONSTRAINT note_agent_run_fk      FOREIGN KEY (agent_run_id) REFERENCES agent.agent_run(id);
ALTER TABLE matter.matter_closing  ADD CONSTRAINT closing_letter_fk      FOREIGN KEY (closing_letter_doc_id) REFERENCES docs.document(id);
ALTER TABLE conflict.clearance     ADD CONSTRAINT clearance_approval_fk  FOREIGN KEY (approval_id) REFERENCES gate.approval_request(id);
ALTER TABLE docs.document_review   ADD CONSTRAINT review_approval_fk     FOREIGN KEY (approval_id) REFERENCES gate.approval_request(id);
ALTER TABLE cal.deadline_confirmation ADD CONSTRAINT confirmation_decision_fk FOREIGN KEY (gate_decision_id) REFERENCES gate.gate_decision(id);
ALTER TABLE trust.withdrawal_request ADD CONSTRAINT withdrawal_approval_fk FOREIGN KEY (approval_id) REFERENCES gate.approval_request(id);
ALTER TABLE trust.trust_transaction ADD CONSTRAINT trust_txn_approval_fk FOREIGN KEY (approval_id) REFERENCES gate.approval_request(id);
ALTER TABLE trust.reconciliation   ADD CONSTRAINT reconciliation_approval_fk FOREIGN KEY (approval_id) REFERENCES gate.approval_request(id);
ALTER TABLE trust.reconciliation   ADD CONSTRAINT reconciliation_import_fk FOREIGN KEY (imported_job_id) REFERENCES platform.import_job(id);
ALTER TABLE trust.trust_transfer   ADD CONSTRAINT transfer_approval_fk  FOREIGN KEY (approval_id) REFERENCES gate.approval_request(id);
ALTER TABLE timekeeping.expense           ADD CONSTRAINT expense_trust_txn_fk   FOREIGN KEY (trust_transaction_id) REFERENCES trust.trust_transaction(id);
ALTER TABLE billing.bill           ADD CONSTRAINT bill_approval_fk       FOREIGN KEY (approval_id) REFERENCES gate.approval_request(id);

-- =====================================================================================
-- Immutability wiring for remaining append-only tables (DM-011)
-- =====================================================================================
CREATE TRIGGER consent_event_immutable   BEFORE UPDATE OR DELETE ON party.consent_event          FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER stage_change_immutable    BEFORE UPDATE OR DELETE ON matter.stage_change          FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER check_run_immutable       BEFORE UPDATE OR DELETE ON conflict.check_run           FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER doc_version_immutable     BEFORE UPDATE OR DELETE ON docs.document_version        FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER confirmation_immutable    BEFORE UPDATE OR DELETE ON cal.deadline_confirmation    FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER automation_run_immutable  BEFORE UPDATE OR DELETE ON work.automation_run          FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER write_down_immutable      BEFORE UPDATE OR DELETE ON timekeeping.write_down              FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER allocation_immutable      BEFORE UPDATE OR DELETE ON billing.payment_allocation   FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER bank_match_immutable      BEFORE UPDATE OR DELETE ON trust.bank_match             FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

-- Financial and record tables: no DELETE ever (DM-010). Drafts are voided/archived, not deleted.
CREATE TRIGGER no_delete_matter      BEFORE DELETE ON matter.matter         FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_contact     BEFORE DELETE ON party.contact         FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_document    BEFORE DELETE ON docs.document         FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_time        BEFORE DELETE ON timekeeping.time_entry       FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_expense     BEFORE DELETE ON timekeeping.expense          FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_payment     BEFORE DELETE ON billing.payment       FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_credit_note BEFORE DELETE ON billing.credit_note   FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_withdrawal  BEFORE DELETE ON trust.withdrawal_request FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_trust_acct  BEFORE DELETE ON trust.trust_account   FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_ledger      BEFORE DELETE ON trust.client_ledger   FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_deadline    BEFORE DELETE ON cal.deadline          FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_approval    BEFORE DELETE ON gate.approval_request FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER no_delete_identity    BEFORE DELETE ON party.identity_record FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();

-- touch() on main mutable tables
CREATE TRIGGER touch_matter   BEFORE UPDATE ON matter.matter     FOR EACH ROW EXECUTE FUNCTION core.touch();
CREATE TRIGGER touch_contact  BEFORE UPDATE ON party.contact     FOR EACH ROW EXECUTE FUNCTION core.touch();
CREATE TRIGGER touch_document BEFORE UPDATE ON docs.document     FOR EACH ROW EXECUTE FUNCTION core.touch();
CREATE TRIGGER touch_time     BEFORE UPDATE ON timekeeping.time_entry   FOR EACH ROW EXECUTE FUNCTION core.touch();
CREATE TRIGGER touch_expense  BEFORE UPDATE ON timekeeping.expense      FOR EACH ROW EXECUTE FUNCTION core.touch();
CREATE TRIGGER touch_bill     BEFORE UPDATE ON billing.bill      FOR EACH ROW EXECUTE FUNCTION core.touch();
CREATE TRIGGER touch_deadline BEFORE UPDATE ON cal.deadline      FOR EACH ROW EXECUTE FUNCTION core.touch();
CREATE TRIGGER touch_lead     BEFORE UPDATE ON intake.lead       FOR EACH ROW EXECUTE FUNCTION core.touch();
CREATE TRIGGER touch_user     BEFORE UPDATE ON core.app_user     FOR EACH ROW EXECUTE FUNCTION core.touch();
CREATE TRIGGER touch_work     BEFORE UPDATE ON work.work_item    FOR EACH ROW EXECUTE FUNCTION core.touch();

-- Billed time is frozen (DM-043).
CREATE OR REPLACE FUNCTION timekeeping.time_entry_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IN ('billed','written_off') AND
     (NEW.minutes, NEW.billed_minutes, NEW.rate_cents, NEW.amount_cents, NEW.matter_id, NEW.narrative, NEW.bill_id, NEW.work_date)
     IS DISTINCT FROM
     (OLD.minutes, OLD.billed_minutes, OLD.rate_cents, OLD.amount_cents, OLD.matter_id, OLD.narrative, OLD.bill_id, OLD.work_date) THEN
    RAISE EXCEPTION 'DM-043: time entry % is %; adjust via write_down/credit note', OLD.id, OLD.status;
  END IF;
  IF NEW.work_date < (SELECT lock_before FROM timekeeping.time_settings) AND
     (TG_OP = 'INSERT' OR NEW.work_date IS DISTINCT FROM OLD.work_date OR NEW.minutes IS DISTINCT FROM OLD.minutes) THEN
    RAISE EXCEPTION 'DM-044: work_date % is before time lock date', NEW.work_date;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER time_entry_guard BEFORE INSERT OR UPDATE ON timekeeping.time_entry FOR EACH ROW EXECUTE FUNCTION timekeeping.time_entry_guard();

-- =====================================================================================
-- Roles and row-level security (defence in depth for walls/visibility; app enforces first)
-- =====================================================================================
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'counsel_app')     THEN CREATE ROLE counsel_app NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'counsel_jobs')    THEN CREATE ROLE counsel_jobs NOLOGIN BYPASSRLS; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'counsel_owner')   THEN CREATE ROLE counsel_owner NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'counsel_integrity') THEN CREATE ROLE counsel_integrity NOLOGIN BYPASSRLS; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'counsel_readonly') THEN CREATE ROLE counsel_readonly NOLOGIN; END IF;
END $$;

-- Can the session user see this matter? Walls beat everything (including firm owners).
CREATE OR REPLACE FUNCTION matter.can_see(p_matter uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, matter, core AS $$
  SELECT p_matter IS NULL OR (
    NOT EXISTS (
      SELECT 1 FROM matter.ethical_wall_matter wm
        JOIN matter.ethical_wall w ON w.id = wm.wall_id AND w.status = 'active'
        JOIN matter.ethical_wall_screened_user su ON su.wall_id = w.id
       WHERE wm.matter_id = p_matter AND su.user_id = core.current_user_id())
    AND NOT EXISTS (SELECT 1 FROM core.app_user u WHERE u.id = core.current_user_id() AND u.is_system_admin)
    AND (
      EXISTS (SELECT 1 FROM matter.matter m WHERE m.id = p_matter AND m.visibility = 'firm_open')
      OR EXISTS (SELECT 1 FROM matter.matter_member mm
                  WHERE mm.matter_id = p_matter AND mm.ends_on IS NULL
                    AND (mm.user_id = core.current_user_id()
                         OR mm.team_id IN (SELECT team_id FROM core.team_user WHERE user_id = core.current_user_id())))
      OR EXISTS (SELECT 1 FROM audit.break_glass bg
                  WHERE bg.matter_id = p_matter AND bg.user_id = core.current_user_id()
                    AND now() BETWEEN bg.starts_at AND bg.ends_at)
    ))
$$;

ALTER TABLE matter.matter       ENABLE ROW LEVEL SECURITY;
ALTER TABLE matter.note         ENABLE ROW LEVEL SECURITY;
ALTER TABLE matter.fact         ENABLE ROW LEVEL SECURITY;
ALTER TABLE docs.document       ENABLE ROW LEVEL SECURITY;
ALTER TABLE docs.draft          ENABLE ROW LEVEL SECURITY;
ALTER TABLE docs.research_memo  ENABLE ROW LEVEL SECURITY;
ALTER TABLE comms.comm_filing   ENABLE ROW LEVEL SECURITY;
ALTER TABLE timekeeping.time_entry     ENABLE ROW LEVEL SECURITY;
ALTER TABLE work.work_item      ENABLE ROW LEVEL SECURITY;
ALTER TABLE cal.deadline        ENABLE ROW LEVEL SECURITY;

CREATE POLICY matter_visible   ON matter.matter      FOR ALL TO counsel_app USING (matter.can_see(id));
CREATE POLICY note_visible     ON matter.note        FOR ALL TO counsel_app USING (matter.can_see(matter_id));
CREATE POLICY fact_visible     ON matter.fact        FOR ALL TO counsel_app USING (matter.can_see(matter_id));
CREATE POLICY document_visible ON docs.document      FOR ALL TO counsel_app USING (matter.can_see(matter_id));
CREATE POLICY draft_visible    ON docs.draft         FOR ALL TO counsel_app USING (matter.can_see(matter_id));
CREATE POLICY memo_visible     ON docs.research_memo FOR ALL TO counsel_app USING (matter.can_see(matter_id));
CREATE POLICY filing_visible   ON comms.comm_filing  FOR ALL TO counsel_app USING (matter.can_see(matter_id));
CREATE POLICY time_visible     ON timekeeping.time_entry    FOR ALL TO counsel_app USING (matter.can_see(matter_id) OR user_id = core.current_user_id());
CREATE POLICY work_visible     ON work.work_item     FOR ALL TO counsel_app USING (matter.can_see(matter_id));
CREATE POLICY deadline_visible ON cal.deadline       FOR ALL TO counsel_app USING (matter.can_see(matter_id));
-- counsel_jobs (background workers, reconciliation, retention) is created BYPASSRLS above and audited.
-- Every other matter-scoped table gets RLS + FORCE in the review addendum (RV2-014, CF-17).

-- Privilege hardening for append-only tables (belt and braces with triggers).
REVOKE UPDATE, DELETE, TRUNCATE ON trust.trust_transaction, trust.bank_line, trust.bank_match, gl.journal_entry, gl.journal_line,
  audit.audit_event, gate.gate_decision, billing.payment_allocation, timekeeping.write_down, docs.document_version,
  cal.deadline_confirmation, matter.stage_change, conflict.check_run, party.consent_event, work.automation_run
  FROM counsel_app, counsel_jobs;

-- =====================================================================================
-- Review addendum (r-review-1 RV1-*, r-review-2 RV2-*, o1 conflicts CF-*). Still 0001: pre-release, no expand/contract.
-- =====================================================================================

-- ---------- gate catalogue, versions and once-only effects (CF-03, CF-04, RV2-002/003/004) ----------

-- One row per gate kind and definition version. approval_request.gate_code = kind (RV2-001).
-- @module gate @retention RC-CONFIG @class config:P0
CREATE TABLE gate.gate_kind (
  kind               text NOT NULL CHECK (kind ~ '^[a-z]+\.[a-z_]+$'),
  version            integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  spec_gate          text NOT NULL CHECK (spec_gate ~ '^G(-?[A-Z0-9]+)+[a-z]?$'),
  ttl_hours          integer NOT NULL CHECK (ttl_hours BETWEEN 1 AND 720),
  required_approvals smallint NOT NULL DEFAULT 1 CHECK (required_approvals BETWEEN 1 AND 3),
  risk               text NOT NULL CHECK (risk IN ('low','medium','high','irreversible')),
  active             boolean NOT NULL DEFAULT true,
  created_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (kind, version),
  CHECK (risk <> 'irreversible' OR required_approvals >= 2)
);
CREATE UNIQUE INDEX gate_kind_one_active ON gate.gate_kind (kind) WHERE active;

-- RV2-002: payload identity and status are immutable except through the gate functions.
CREATE OR REPLACE FUNCTION gate.approval_request_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (NEW.payload_hash, NEW.version, NEW.gate_code, NEW.spec_gate, NEW.payload_type, NEW.payload_id, NEW.requested_by,
      NEW.required_approvals, NEW.expires_at, NEW.created_at, NEW.supersedes_id, NEW.risk, NEW.matter_id)
     IS DISTINCT FROM
     (OLD.payload_hash, OLD.version, OLD.gate_code, OLD.spec_gate, OLD.payload_type, OLD.payload_id, OLD.requested_by,
      OLD.required_approvals, OLD.expires_at, OLD.created_at, OLD.supersedes_id, OLD.risk, OLD.matter_id) THEN
    RAISE EXCEPTION 'DM-077: approval % payload binding is immutable; supersede with a new version', OLD.id;
  END IF;
  IF NEW.status IS DISTINCT FROM OLD.status THEN
    IF OLD.status <> 'pending' OR NEW.status NOT IN ('approved','rejected','changes_requested','withdrawn','expired','superseded') THEN
      RAISE EXCEPTION 'DM-077: approval % cannot go % -> %', OLD.id, OLD.status, NEW.status;
    END IF;
    IF coalesce(current_setting('counsel.gate_tx', true), '') <> 'on' THEN
      RAISE EXCEPTION 'DM-077: approval status changes only through gate functions';
    END IF;
  END IF;
  NEW.row_version := OLD.row_version + 1;
  RETURN NEW;
END $$;
CREATE TRIGGER approval_request_guard BEFORE UPDATE ON gate.approval_request FOR EACH ROW EXECUTE FUNCTION gate.approval_request_guard();

-- Payload edit = supersede: close v(n), open v(n+1). Decisions on v(n) never count toward v(n+1).
CREATE OR REPLACE FUNCTION gate.supersede(p_old uuid, p_new_hash bytea) RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, gate AS $$
DECLARE o gate.approval_request%ROWTYPE; nid uuid := core.uuidv7();
BEGIN
  SELECT * INTO o FROM gate.approval_request WHERE id = p_old FOR UPDATE;
  IF o.status <> 'pending' THEN RAISE EXCEPTION 'DM-077: only a pending approval can be superseded'; END IF;
  PERFORM set_config('counsel.gate_tx', 'on', true);
  UPDATE gate.approval_request SET status = 'superseded', closed_at = now() WHERE id = p_old;
  PERFORM set_config('counsel.gate_tx', 'off', true);
  INSERT INTO gate.approval_request (id, gate_code, spec_gate, version, supersedes_id, matter_id, payload_type, payload_id, payload_hash,
         requested_by, agent_run_id, eligible_role, assigned_to, required_approvals, due_at, risk, sources, diff_ref, expires_at)
  VALUES (nid, o.gate_code, o.spec_gate, o.version + 1, o.id, o.matter_id, o.payload_type, o.payload_id, p_new_hash,
         o.requested_by, o.agent_run_id, o.eligible_role, o.assigned_to, o.required_approvals, o.due_at, o.risk, o.sources, o.diff_ref,
         now() + (o.expires_at - o.created_at));
  RETURN nid;
END $$;

-- RV2-003: River job gate.expire calls this every minute.
CREATE OR REPLACE FUNCTION gate.expire_due() RETURNS integer LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, gate AS $$
DECLARE n integer;
BEGIN
  PERFORM set_config('counsel.gate_tx', 'on', true);
  UPDATE gate.approval_request SET status = 'expired', closed_at = now() WHERE status = 'pending' AND expires_at <= now();
  GET DIAGNOSTICS n = ROW_COUNT;
  PERFORM set_config('counsel.gate_tx', 'off', true);
  RETURN n;
END $$;

-- RV2-004: written in the same transaction as the effect; PK makes a second run of the same approval impossible.
-- @module gate @retention RC-AUDIT @class effect:P1
CREATE TABLE gate.effect_execution (
  approval_id  uuid PRIMARY KEY REFERENCES gate.approval_request(id),
  handler      text NOT NULL,
  started_at   timestamptz NOT NULL DEFAULT now(),
  finished_at  timestamptz,
  outcome      text CHECK (outcome IN ('ok','failed')),
  error_code   text,
  CHECK ((outcome IS NULL) = (finished_at IS NULL))
);
CREATE OR REPLACE FUNCTION gate.effect_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r gate.approval_request%ROWTYPE;
BEGIN
  SELECT * INTO r FROM gate.approval_request WHERE id = NEW.approval_id;
  IF r.status <> 'approved' OR r.closed_at >= r.expires_at THEN
    RAISE EXCEPTION 'DM-076: effect for approval % needs status approved before expiry (status %)', r.id, r.status;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER effect_execution_guard BEFORE INSERT ON gate.effect_execution FOR EACH ROW EXECUTE FUNCTION gate.effect_guard();

-- ---------- separation of duties, configurable sole-practitioner exception (RV1-011) ----------
CREATE OR REPLACE FUNCTION core.check_sod(p_maker uuid, p_checker uuid, p_what text, p_object uuid, p_approval uuid)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, core, audit AS $$
BEGIN
  IF p_maker IS NULL OR p_checker IS NULL OR p_maker <> p_checker THEN RETURN; END IF;
  IF NOT coalesce((SELECT sole_practitioner FROM core.firm), false) THEN
    RAISE EXCEPTION 'DM-078: sod_violation: the same person cannot both make and check % %', p_what, p_object;
  END IF;
  INSERT INTO audit.audit_event(stream, actor_kind, actor_user_id, action, object_type, object_id, approval_id, summary)
  VALUES ('compliance', 'user', p_checker, 'sod_solo_exception', p_what, p_object, p_approval, 'sole-practitioner exception');
END $$;

-- ---------- trust: available funds, holds, clearance, withdrawal guard (RV1-002/003, RV2-018/021) ----------

-- Append-only clearance facts for receipts that started uncleared (cheque, bank draft).
-- @module trust @retention RC-FIN @class clearance:P1
CREATE TABLE trust.receipt_clearance (
  trust_transaction_id uuid PRIMARY KEY REFERENCES trust.trust_transaction(id),
  cleared_on    date NOT NULL,
  bank_line_id  uuid REFERENCES trust.bank_line(id),
  recorded_by   uuid NOT NULL REFERENCES core.app_user(id),
  recorded_at   timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER receipt_clearance_immutable BEFORE UPDATE OR DELETE ON trust.receipt_clearance FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

CREATE OR REPLACE FUNCTION trust.balance_cents(p_ledger uuid) RETURNS bigint LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, trust AS $$
  SELECT coalesce(sum(amount_cents), 0)::bigint FROM trust.trust_transaction WHERE ledger_id = p_ledger
$$;

-- available = cleared money in - all money out - protected - minimum - authorized-but-unposted holds (excluding the caller's own).
CREATE OR REPLACE FUNCTION trust.available_cents(p_ledger uuid, p_except_withdrawal uuid DEFAULT NULL, p_except_transfer uuid DEFAULT NULL)
RETURNS bigint LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, trust AS $$
  SELECT (coalesce((SELECT sum(t.amount_cents) FROM trust.trust_transaction t
                     WHERE t.ledger_id = p_ledger
                       AND (t.amount_cents < 0 OR t.cleared_on <= current_date OR t.kind <> 'receipt'
                            OR EXISTS (SELECT 1 FROM trust.receipt_clearance c WHERE c.trust_transaction_id = t.id AND c.cleared_on <= current_date))), 0)
        - l.protected_cents - l.minimum_balance_cents
        - coalesce((SELECT sum(w.amount_cents) FROM trust.withdrawal_request w
                     WHERE w.ledger_id = p_ledger AND w.status = 'authorized' AND w.id IS DISTINCT FROM p_except_withdrawal), 0)
        - coalesce((SELECT sum(x.amount_cents) FROM trust.trust_transfer x
                     WHERE x.from_ledger_id = p_ledger AND x.status = 'authorized' AND x.id IS DISTINCT FROM p_except_transfer), 0))::bigint
    FROM trust.client_ledger l WHERE l.id = p_ledger
$$;

CREATE OR REPLACE FUNCTION trust.withdrawal_guard() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, trust, gate, core AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.status <> 'proposed'
     AND (NEW.amount_cents, NEW.payee, NEW.ledger_id, NEW.method, NEW.bill_id, NEW.purpose_category, NEW.initiated_by)
         IS DISTINCT FROM (OLD.amount_cents, OLD.payee, OLD.ledger_id, OLD.method, OLD.bill_id, OLD.purpose_category, OLD.initiated_by) THEN
    RAISE EXCEPTION 'DM-067: withdrawal % is % and its terms are frozen', OLD.id, OLD.status;
  END IF;
  IF NEW.status IN ('authorized','executed','applied','confirmed') AND (TG_OP = 'INSERT' OR OLD.status IS DISTINCT FROM NEW.status) THEN
    IF NOT EXISTS (SELECT 1 FROM gate.approval_request ar WHERE ar.id = NEW.approval_id AND ar.status = 'approved'
                     AND ar.payload_type = 'trust.withdrawal_request' AND ar.payload_id = NEW.id) THEN
      RAISE EXCEPTION 'DM-067: withdrawal % needs an approved trust.withdrawal gate bound to it', NEW.id;
    END IF;
  END IF;
  IF NEW.status = 'authorized' AND (TG_OP = 'INSERT' OR OLD.status <> 'authorized') THEN
    PERFORM core.check_sod(NEW.initiated_by, NEW.authorized_by, 'trust.withdrawal_request', NEW.id, NEW.approval_id);
    PERFORM core.check_sod(NEW.initiated_by, NEW.second_authorized_by, 'trust.withdrawal_request', NEW.id, NEW.approval_id);
    PERFORM core.check_sod(NEW.authorized_by, NEW.second_authorized_by, 'trust.withdrawal_request', NEW.id, NEW.approval_id);
    PERFORM 1 FROM trust.client_ledger WHERE id = NEW.ledger_id FOR UPDATE;          -- RV1-002 hold under the ledger lock
    IF trust.available_cents(NEW.ledger_id, NEW.id, NULL) < NEW.amount_cents THEN
      RAISE EXCEPTION 'DM-064: insufficient available trust funds to authorize withdrawal % (available %, requested %)',
        NEW.id, trust.available_cents(NEW.ledger_id, NEW.id, NULL), NEW.amount_cents USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.purpose_category = 'fees_to_operating' AND
       NEW.amount_cents > (SELECT balance_cents - reserved_cents FROM billing.bill_balance WHERE bill_id = NEW.bill_id)
                          + coalesce((SELECT amount_cents FROM billing.trust_fee_transfer WHERE withdrawal_id = NEW.id AND status = 'authorized'), 0) THEN
      RAISE EXCEPTION 'DM-047: fee transfer % exceeds the unreserved balance of bill %', NEW.id, NEW.bill_id;
    END IF;
  END IF;
  IF NEW.status = 'executed' AND OLD.status IS DISTINCT FROM 'executed' THEN
    PERFORM core.check_sod(NEW.authorized_by, NEW.executed_by, 'trust.withdrawal_request', NEW.id, NEW.approval_id);
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER withdrawal_guard BEFORE INSERT OR UPDATE ON trust.withdrawal_request FOR EACH ROW EXECUTE FUNCTION trust.withdrawal_guard();

CREATE OR REPLACE FUNCTION trust.reconciliation_sod() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.status = 'signed' AND (TG_OP = 'INSERT' OR OLD.status <> 'signed') THEN
    PERFORM core.check_sod(NEW.prepared_by, NEW.signed_by, 'trust.reconciliation', NEW.id, NEW.approval_id);
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER reconciliation_sod BEFORE INSERT OR UPDATE ON trust.reconciliation FOR EACH ROW EXECUTE FUNCTION trust.reconciliation_sod();

-- RV2-005: imported source-system history. Separate book; NOT in ledger_balance / account_balance / available_cents.
-- Live book starts with exactly one kind='opening_balance' row per ledger; VC-TR-4: sum(historical_line) = opening row.
-- @module trust @retention RC-FIN @class amounts:P1,counterparty:P2
CREATE TABLE trust.historical_line (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  account_id     uuid NOT NULL REFERENCES trust.trust_account(id),
  ledger_id      uuid NOT NULL REFERENCES trust.client_ledger(id),
  txn_date       date NOT NULL,
  kind           text NOT NULL,                  -- source values; 'unknown' allowed
  method         text NOT NULL,
  instrument_no  text,
  counterparty   text,
  amount_cents   bigint NOT NULL,
  purpose        text,
  source_ref     jsonb NOT NULL,                 -- {clio_type, clio_id, etag}
  import_job_id  uuid NOT NULL REFERENCES platform.import_job(id),
  staged_sha256  bytea NOT NULL,
  UNIQUE (import_job_id, source_ref)
);
CREATE INDEX historical_line_ledger_idx ON trust.historical_line (ledger_id, txn_date);
CREATE TRIGGER historical_line_immutable BEFORE UPDATE OR DELETE ON trust.historical_line FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE TRIGGER historical_line_no_truncate BEFORE TRUNCATE ON trust.historical_line FOR EACH STATEMENT EXECUTE FUNCTION core.forbid_mutation();

CREATE VIEW trust.opening_tieout AS          -- VC-TR-4: diff must be 0 for every ledger
  SELECT l.id AS ledger_id,
         coalesce((SELECT sum(h.amount_cents) FROM trust.historical_line h WHERE h.ledger_id = l.id), 0)::bigint AS history_cents,
         coalesce((SELECT t.amount_cents FROM trust.trust_transaction t WHERE t.ledger_id = l.id AND t.kind = 'opening_balance'), 0)::bigint AS opening_cents
    FROM trust.client_ledger l;

-- ---------- billing additions (CF-03, RV1-002, RV1-008, RV1-010, RV1-013, RV1-022, BIL-T-090) ----------

-- RV1-002 step 3: trust-to-operating fee transfer applied to a bill; reserved while authorized/executed.
-- @module billing @retention RC-FIN @class amounts:P1
CREATE TABLE billing.trust_fee_transfer (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  withdrawal_id    uuid NOT NULL UNIQUE REFERENCES trust.withdrawal_request(id),
  bill_id          uuid NOT NULL REFERENCES billing.bill(id),
  amount_cents     bigint NOT NULL CHECK (amount_cents > 0),
  fee_cents        bigint NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
  dtx_cents        bigint NOT NULL DEFAULT 0 CHECK (dtx_cents >= 0),
  dnt_cents        bigint NOT NULL DEFAULT 0 CHECK (dnt_cents >= 0),
  tax_cents        bigint NOT NULL DEFAULT 0 CHECK (tax_cents >= 0),
  interest_cents   bigint NOT NULL DEFAULT 0 CHECK (interest_cents >= 0),
  excess_cents     bigint NOT NULL DEFAULT 0 CHECK (excess_cents >= 0),   -- executed > bill balance: excess to client credit
  status           text NOT NULL DEFAULT 'authorized' CHECK (status IN ('authorized','executed','applied','cancelled')),
  journal_entry_id uuid REFERENCES gl.journal_entry(id),
  created_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (fee_cents + dtx_cents + dnt_cents + tax_cents + interest_cents + excess_cents = amount_cents),
  CHECK (status <> 'applied' OR journal_entry_id IS NOT NULL)
);

-- RV1-013: interest accrual rows (BIL-C-072); rate copied from bill.interest_rate_ppm.
-- @module billing @retention RC-FIN @class amounts:P1
CREATE TABLE billing.interest_charge (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  bill_id          uuid NOT NULL REFERENCES billing.bill(id),
  period           daterange NOT NULL CHECK (NOT isempty(period)),
  principal_cents  bigint NOT NULL CHECK (principal_cents > 0),
  rate_ppm         integer NOT NULL CHECK (rate_ppm > 0),
  days             integer NOT NULL CHECK (days > 0),
  amount_cents     bigint NOT NULL CHECK (amount_cents >= 0),
  posted_at        timestamptz NOT NULL DEFAULT now(),
  journal_entry_id uuid REFERENCES gl.journal_entry(id)
);
CREATE UNIQUE INDEX interest_charge_once ON billing.interest_charge (bill_id, lower(period));   -- one accrual per bill per start date
CREATE OR REPLACE FUNCTION billing.interest_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE b billing.bill%ROWTYPE;
BEGIN
  SELECT * INTO b FROM billing.bill WHERE id = NEW.bill_id;
  IF b.interest_rate_ppm IS NULL OR b.delivered_at IS NULL THEN
    RAISE EXCEPTION 'DM-048: bill % has no interest term or is not delivered', NEW.bill_id;
  END IF;
  IF NEW.rate_ppm <> b.interest_rate_ppm THEN
    RAISE EXCEPTION 'DM-048: interest rate % differs from the bill snapshot %', NEW.rate_ppm, b.interest_rate_ppm;
  END IF;
  -- interest_start(bill) = delivered date + 1 month (Solicitors Act s.33, verify)
  IF lower(NEW.period) < ((b.delivered_at AT TIME ZONE 'America/Toronto')::date + interval '1 month')::date THEN
    RAISE EXCEPTION 'DM-048: interest cannot start before one month after delivery of bill %', NEW.bill_id;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER interest_charge_guard BEFORE INSERT ON billing.interest_charge FOR EACH ROW EXECUTE FUNCTION billing.interest_guard();
CREATE TRIGGER interest_charge_immutable BEFORE UPDATE OR DELETE ON billing.interest_charge FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

-- BIL-C-085 write-off of an unpaid balance (bucketed like allocations).
-- @module billing @retention RC-FIN @class amounts:P1
CREATE TABLE billing.write_off (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  bill_id          uuid NOT NULL REFERENCES billing.bill(id),
  amount_cents     bigint NOT NULL CHECK (amount_cents > 0),
  fee_cents        bigint NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
  dtx_cents        bigint NOT NULL DEFAULT 0 CHECK (dtx_cents >= 0),
  dnt_cents        bigint NOT NULL DEFAULT 0 CHECK (dnt_cents >= 0),
  tax_cents        bigint NOT NULL DEFAULT 0 CHECK (tax_cents >= 0),
  interest_cents   bigint NOT NULL DEFAULT 0 CHECK (interest_cents >= 0),
  reason           text NOT NULL,
  approval_id      uuid NOT NULL REFERENCES gate.approval_request(id),
  journal_entry_id uuid REFERENCES gl.journal_entry(id),
  created_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (fee_cents + dtx_cents + dnt_cents + tax_cents + interest_cents = amount_cents)
);
CREATE TRIGGER write_off_immutable BEFORE UPDATE OR DELETE ON billing.write_off FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

-- BIL-C-063 client credit: signed append-only ledger (payment overflow, credit-note refunds to credit, fee-transfer excess).
-- @module billing @retention RC-FIN @class amounts:P1
CREATE TABLE billing.client_credit (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  client_id     uuid NOT NULL REFERENCES party.contact(id),
  matter_id     uuid REFERENCES matter.matter(id),
  amount_cents  bigint NOT NULL CHECK (amount_cents <> 0),     -- + credit created, - credit used/refunded
  source_type   text NOT NULL CHECK (source_type IN ('payment_allocation','credit_note','trust_fee_transfer','application','refund')),
  source_id     uuid NOT NULL,
  bill_id       uuid REFERENCES billing.bill(id),               -- when credit is applied to a bill
  created_by    uuid NOT NULL REFERENCES core.app_user(id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (source_type, source_id),
  CHECK (source_type <> 'application' OR (bill_id IS NOT NULL AND amount_cents < 0))
);
CREATE TRIGGER client_credit_immutable BEFORE UPDATE OR DELETE ON billing.client_credit FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();
CREATE VIEW billing.client_credit_balance AS
  SELECT client_id, sum(amount_cents)::bigint AS credit_cents FROM billing.client_credit GROUP BY client_id;

-- RV1-008: audited correction of a delivery date (gate billing.delivery_correction, G-B2).
-- @module billing @retention RC-FIN @class correction:P1
CREATE TABLE billing.delivery_correction (
  id                uuid PRIMARY KEY DEFAULT core.uuidv7(),
  bill_id           uuid NOT NULL REFERENCES billing.bill(id),
  old_delivered_at  timestamptz NOT NULL,
  new_delivered_at  timestamptz,
  evidence_doc_id   uuid REFERENCES docs.document(id),
  reason            text NOT NULL,
  approval_id       uuid NOT NULL REFERENCES gate.approval_request(id),
  applied_at        timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now()
);

-- BIL-T-090: voiding/crediting across a filed GST/HST period produces an adjustment in the open period.
-- @module billing @retention RC-FIN @class tax:P1
CREATE TABLE billing.tax_period_adjustment (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  bill_id          uuid NOT NULL REFERENCES billing.bill(id),
  credit_note_id   uuid REFERENCES billing.credit_note(id),
  original_period  daterange NOT NULL,
  report_period    daterange NOT NULL,
  tax_code         text NOT NULL,
  tax_cents        bigint NOT NULL CHECK (tax_cents <> 0),
  journal_entry_id uuid REFERENCES gl.journal_entry(id),
  created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER tax_period_adjustment_immutable BEFORE UPDATE OR DELETE ON billing.tax_period_adjustment FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

-- RV1-022: provider events; handlers re-fetch the provider object and apply by precedence, so order does not matter.
-- @module billing @retention RC-FIN @class event:P1
CREATE TABLE billing.payment_event (
  provider      text NOT NULL,
  event_id      text NOT NULL,
  object_ref    text NOT NULL,
  type          text NOT NULL,
  payment_id    uuid REFERENCES billing.payment(id),
  received_at   timestamptz NOT NULL DEFAULT now(),
  processed_at  timestamptz,
  body_sha256   bytea NOT NULL,
  PRIMARY KEY (provider, event_id)
);
CREATE INDEX payment_event_object_idx ON billing.payment_event (provider, object_ref);

-- Bill balance (DM-042): total - allocations - credits - write-offs - applied fee transfers + posted interest.
-- reserved_cents (RV1-002): fee transfers authorized/executed but not yet applied.
CREATE VIEW billing.bill_balance AS
  SELECT b.id AS bill_id, b.total_cents, x.paid_cents, x.credited_cents, x.written_off_cents, x.fee_transfer_cents, x.interest_cents,
         (b.total_cents - x.paid_cents - x.credited_cents - x.written_off_cents - x.fee_transfer_cents + x.interest_cents)::bigint AS balance_cents,
         x.reserved_cents
    FROM billing.bill b
    CROSS JOIN LATERAL (SELECT
      coalesce((SELECT sum(a.amount_cents) FROM billing.payment_allocation a WHERE a.bill_id = b.id), 0)::bigint AS paid_cents,
      coalesce((SELECT sum(c.amount_cents + c.tax_cents) FROM billing.credit_note c WHERE c.bill_id = b.id AND c.status IN ('issued','refund_pending','closed')), 0)::bigint AS credited_cents,
      coalesce((SELECT sum(w.amount_cents) FROM billing.write_off w WHERE w.bill_id = b.id), 0)::bigint AS written_off_cents,
      coalesce((SELECT sum(f.amount_cents - f.excess_cents) FROM billing.trust_fee_transfer f WHERE f.bill_id = b.id AND f.status = 'applied'), 0)::bigint AS fee_transfer_cents,
      coalesce((SELECT sum(f.amount_cents - f.excess_cents) FROM billing.trust_fee_transfer f WHERE f.bill_id = b.id AND f.status IN ('authorized','executed')), 0)::bigint AS reserved_cents,
      coalesce((SELECT sum(i.amount_cents) FROM billing.interest_charge i WHERE i.bill_id = b.id), 0)::bigint AS interest_cents) x;

-- RV1-010: target/deposit match (DM-046) and no over-allocation incl. reserved fee transfers (DM-047); reversal mirrors buckets.
CREATE OR REPLACE FUNCTION billing.allocation_guard() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, billing AS $$
DECLARE dep text; o billing.payment_allocation%ROWTYPE;
BEGIN
  SELECT deposit_to INTO dep FROM billing.payment WHERE id = NEW.payment_id;
  IF NEW.target_type = 'bill' AND dep <> 'operating' THEN
    RAISE EXCEPTION 'DM-046: a trust receipt cannot be allocated to a bill; use a trust fee transfer' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.target_type = 'trust_ledger' AND dep <> 'trust' THEN
    RAISE EXCEPTION 'DM-046: only trust deposits can be allocated to a trust ledger' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.reverses_id IS NOT NULL THEN
    SELECT * INTO o FROM billing.payment_allocation WHERE id = NEW.reverses_id;
    IF (NEW.payment_id, NEW.target_type, NEW.bill_id, NEW.trust_ledger_id, NEW.client_id,
        NEW.amount_cents, NEW.fee_cents, NEW.dtx_cents, NEW.dnt_cents, NEW.tax_cents, NEW.interest_cents)
       IS DISTINCT FROM (o.payment_id, o.target_type, o.bill_id, o.trust_ledger_id, o.client_id,
        -o.amount_cents, -o.fee_cents, -o.dtx_cents, -o.dnt_cents, -o.tax_cents, -o.interest_cents) THEN
      RAISE EXCEPTION 'DM-046: reversal must equal -1 x the original allocation in every bucket (BIL-I-019)';
    END IF;
  END IF;
  IF NEW.bill_id IS NOT NULL THEN PERFORM 1 FROM billing.bill WHERE id = NEW.bill_id FOR UPDATE; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER payment_allocation_guard BEFORE INSERT ON billing.payment_allocation FOR EACH ROW EXECUTE FUNCTION billing.allocation_guard();

CREATE OR REPLACE FUNCTION billing.check_bill_not_overpaid() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, billing AS $$
DECLARE bal bigint; res bigint;
BEGIN
  IF NEW.bill_id IS NULL THEN RETURN NULL; END IF;
  SELECT balance_cents, reserved_cents INTO bal, res FROM billing.bill_balance WHERE bill_id = NEW.bill_id;
  IF bal - res < 0 THEN
    RAISE EXCEPTION 'DM-047: bill % over-allocated (balance %, reserved %)', NEW.bill_id, bal, res USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER bill_not_overpaid AFTER INSERT ON billing.payment_allocation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION billing.check_bill_not_overpaid();
CREATE CONSTRAINT TRIGGER bill_not_overpaid_wo AFTER INSERT ON billing.write_off
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION billing.check_bill_not_overpaid();

-- CF-08: voiding a bill releases its lines' time/expense locks.
CREATE OR REPLACE FUNCTION billing.bill_void_lines() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.status = 'void' AND OLD.status <> 'void' THEN
    UPDATE billing.bill_line SET bill_voided = true WHERE bill_id = NEW.id;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER bill_void_lines AFTER UPDATE OF status ON billing.bill FOR EACH ROW EXECUTE FUNCTION billing.bill_void_lines();

CREATE OR REPLACE FUNCTION billing.bill_sod() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.status = 'approved' AND OLD.status <> 'approved' THEN
    PERFORM core.check_sod(NEW.created_by, NEW.approved_by, 'billing.bill', NEW.id, NEW.approval_id);
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER bill_sod BEFORE UPDATE ON billing.bill FOR EACH ROW EXECUTE FUNCTION billing.bill_sod();

-- RV1-019: issued credit notes are immutable (only the refund lifecycle moves).
CREATE OR REPLACE FUNCTION billing.credit_note_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IN ('issued','refund_pending','closed') AND (
       (to_jsonb(NEW) - 'status') IS DISTINCT FROM (to_jsonb(OLD) - 'status')
       OR NOT (OLD.status = NEW.status OR (OLD.status, NEW.status) IN (('issued','refund_pending'),('issued','closed'),('refund_pending','closed')))) THEN
    RAISE EXCEPTION 'DM-049: credit note % is % and immutable', OLD.id, OLD.status;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER credit_note_guard BEFORE UPDATE ON billing.credit_note FOR EACH ROW EXECUTE FUNCTION billing.credit_note_guard();

-- BIL-L-039 / RV1-004 (verify By-Law 9 s.4): aggregate cash on a matter, operating + trust. Block at >= 750000 cents
-- unless an exception code with evidence (G6c). Callers take pg_advisory_xact_lock(hashtext(matter_id::text)) first.
CREATE OR REPLACE FUNCTION billing.cash_received_cents(p_matter uuid) RETURNS bigint LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, billing, trust AS $$
  SELECT (coalesce((SELECT sum(p.amount_cents) FROM billing.payment p
                     JOIN billing.payment_allocation a ON a.payment_id = p.id
                     LEFT JOIN billing.bill b ON b.id = a.bill_id
                    WHERE p.method = 'cash' AND p.deposit_to = 'operating' AND b.matter_id = p_matter), 0)
        + coalesce((SELECT sum(t.amount_cents) FROM trust.trust_transaction t JOIN trust.client_ledger l ON l.id = t.ledger_id
                    WHERE t.method = 'cash' AND t.amount_cents > 0 AND l.matter_id = p_matter), 0))::bigint
$$;

-- ---------- gl: once-only already via journal_once_per_source. Locked-month race (RV2-007) handled in trust.before_txn.

-- ---------- platform: messaging, idempotency, inbound events, flags, subscriptions (CF-03, RV2-012, RV2-013) ----------

-- Transactional outbox (ARC-I-008): written in the business transaction; relay publishes at-least-once.
-- @module platform @retention RC-OPS @class event:P1
CREATE TABLE platform.outbox (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  topic         text NOT NULL,
  aggregate     text NOT NULL,
  aggregate_id  uuid NOT NULL,
  payload       jsonb NOT NULL,              -- ids and amounts only; no privileged content
  created_at    timestamptz NOT NULL DEFAULT now(),
  published_at  timestamptz,
  attempts      integer NOT NULL DEFAULT 0
);
CREATE INDEX outbox_unpublished_idx ON platform.outbox (created_at) WHERE published_at IS NULL;

-- Consumer-side dedupe (ARC-I-015): effect applied once per (consumer, message).
-- @module platform @retention RC-OPS @class event:P1
CREATE TABLE platform.inbox (
  consumer      text NOT NULL,
  message_id    uuid NOT NULL,
  processed_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (consumer, message_id)
);

-- API idempotency (ARC-I-023): same key + same request hash replays the stored response.
-- @module platform @retention RC-OPS @class request:P1
CREATE TABLE platform.idempotency_key (
  principal_id   uuid NOT NULL REFERENCES core.app_user(id),
  key            text NOT NULL CHECK (length(key) BETWEEN 8 AND 200),
  request_sha256 bytea NOT NULL,
  status_code    integer,
  response_ref   text,
  created_at     timestamptz NOT NULL DEFAULT now(),
  expires_at     timestamptz NOT NULL,
  PRIMARY KEY (principal_id, key)
);

-- Inbound provider webhooks (RV2-012): signature verified first; duplicate (provider, event_id) -> 204, no effect.
-- @module platform @retention RC-OPS @class event:P1
CREATE TABLE platform.inbound_event (
  provider        text NOT NULL,
  event_id        text NOT NULL,
  received_at     timestamptz NOT NULL DEFAULT now(),
  payload_sha256  bytea NOT NULL,
  processed_at    timestamptz,
  PRIMARY KEY (provider, event_id)
);

-- ARC-U-019 feature flags.
-- @module platform @retention RC-CONFIG @class config:P0
CREATE TABLE platform.feature_flag (
  key          text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_.]*$'),
  enabled      boolean NOT NULL DEFAULT false,
  rollout      jsonb NOT NULL DEFAULT '{}'::jsonb,
  description  text NOT NULL,
  updated_at   timestamptz NOT NULL DEFAULT now(),
  updated_by   uuid REFERENCES core.app_user(id)
);

-- RV2-013 / MIG-042: inbound source-system webhook subscriptions, renewed by River job subscription.renew.
-- @module platform @retention RC-CONFIG @class subscription:P1
CREATE TABLE platform.source_subscription (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  source           text NOT NULL,
  remote_id        text NOT NULL,
  events           text[] NOT NULL,
  expires_at       timestamptz NOT NULL,
  last_renewed_at  timestamptz,
  last_error       text,
  UNIQUE (source, remote_id)
);

REVOKE UPDATE, DELETE, TRUNCATE ON trust.historical_line, trust.receipt_clearance, billing.interest_charge, billing.write_off,
  billing.client_credit, billing.tax_period_adjustment FROM counsel_app, counsel_jobs;

-- ---------- RLS on every matter-scoped table + FORCE (RV2-014, CF-17) ----------
-- Excluded: gate.approval_request and gl.journal_line (financial controls need firm-wide visibility; guarded by Can()).
-- Tables keyed through a parent (trust.client_ledger has matter_id; billing.bill has matter_id) are covered by the loop.
DO $$
DECLARE t record;
BEGIN
  FOR t IN SELECT c.table_schema, c.table_name FROM information_schema.columns c
             JOIN information_schema.tables tb ON tb.table_schema = c.table_schema AND tb.table_name = c.table_name AND tb.table_type = 'BASE TABLE'
            WHERE c.column_name = 'matter_id'
              AND (c.table_schema, c.table_name) NOT IN (('gate','approval_request'), ('gl','journal_line'))
              AND c.table_schema NOT IN ('pg_catalog','information_schema')
  LOOP
    EXECUTE format('ALTER TABLE %I.%I ENABLE ROW LEVEL SECURITY', t.table_schema, t.table_name);
    IF NOT EXISTS (SELECT 1 FROM pg_policies p WHERE p.schemaname = t.table_schema AND p.tablename = t.table_name) THEN
      EXECUTE format('CREATE POLICY %I ON %I.%I FOR ALL TO counsel_app USING (matter.can_see(matter_id))',
                     t.table_name || '_matter_visible', t.table_schema, t.table_name);
    END IF;
  END LOOP;
  FOR t IN SELECT n.nspname AS table_schema, c.relname AS table_name FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
            WHERE c.relkind = 'r' AND c.relrowsecurity LOOP
    EXECUTE format('ALTER TABLE %I.%I FORCE ROW LEVEL SECURITY', t.table_schema, t.table_name);
  END LOOP;
END $$;

-- Privileges: counsel_app gets DML (append-only REVOKEs above still win); integrity functions run as counsel_integrity.
DO $$
DECLARE s text;
BEGIN
  FOREACH s IN ARRAY ARRAY['core','party','intake','conflict','matter','docs','cal','work','timekeeping','trust','billing','gl','comms','gate','agent','audit','platform'] LOOP
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO counsel_app, counsel_jobs, counsel_integrity', s);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA %I TO counsel_app, counsel_jobs', s);
    EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO counsel_integrity', s);
  END LOOP;
END $$;
REVOKE UPDATE, DELETE, TRUNCATE ON trust.trust_transaction, trust.bank_line, trust.bank_match, gl.journal_entry, gl.journal_line,
  audit.audit_event, gate.gate_decision, billing.payment_allocation, timekeeping.write_down, docs.document_version,
  cal.deadline_confirmation, matter.stage_change, conflict.check_run, party.consent_event, work.automation_run,
  trust.historical_line, trust.receipt_clearance, billing.interest_charge, billing.write_off, billing.client_credit,
  billing.tax_period_adjustment, gate.effect_execution
  FROM counsel_app, counsel_jobs;
GRANT INSERT ON audit.audit_event TO counsel_integrity;
-- UPDATE is also what Postgres requires for the row locks (FOR UPDATE / FOR SHARE) these functions take.
GRANT UPDATE ON gate.approval_request, billing.bill_line, trust.trust_account, trust.client_ledger, billing.bill, billing.payment TO counsel_integrity;
GRANT INSERT ON gate.approval_request TO counsel_integrity;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA audit TO counsel_integrity;
ALTER FUNCTION matter.can_see(uuid)                 OWNER TO counsel_integrity;
ALTER FUNCTION audit.chain()                         OWNER TO counsel_integrity;
ALTER FUNCTION trust.before_txn()                    OWNER TO counsel_integrity;
ALTER FUNCTION trust.check_ledger_non_negative()     OWNER TO counsel_integrity;
ALTER FUNCTION trust.balance_cents(uuid)             OWNER TO counsel_integrity;
ALTER FUNCTION trust.available_cents(uuid, uuid, uuid) OWNER TO counsel_integrity;
ALTER FUNCTION trust.withdrawal_guard()              OWNER TO counsel_integrity;
ALTER FUNCTION billing.check_allocation_total()      OWNER TO counsel_integrity;
ALTER FUNCTION billing.allocation_guard()            OWNER TO counsel_integrity;
ALTER FUNCTION billing.check_bill_not_overpaid()     OWNER TO counsel_integrity;
ALTER FUNCTION billing.cash_received_cents(uuid)     OWNER TO counsel_integrity;
ALTER FUNCTION gate.check_decision()                 OWNER TO counsel_integrity;
ALTER FUNCTION gate.supersede(uuid, bytea)           OWNER TO counsel_integrity;
ALTER FUNCTION gate.expire_due()                     OWNER TO counsel_integrity;
ALTER FUNCTION core.check_sod(uuid, uuid, text, uuid, uuid) OWNER TO counsel_integrity;

-- Hold/reservation views for the API (a2): available balance per ledger and reserved amount per bill.
CREATE VIEW trust.ledger_available AS
  SELECT l.id AS ledger_id, trust.balance_cents(l.id) AS balance_cents, trust.available_cents(l.id) AS available_cents
    FROM trust.client_ledger l;
