set search_path = evesso, public;
begin;
alter table if exists pkces
    ADD COLUMN reference_data jsonb;
alter table if exists characters
    add column reference_data jsonb;
commit;