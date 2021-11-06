set search_path = evesso, public;
begin;
alter table if exists characters
    ADD COLUMN access_token text;
commit;
