set search_path = evesso, public;
begin;
alter table if exists pkces
    DROP COLUMN reference_data;
alter table if exists characters
    drop column reference_data;
commit;