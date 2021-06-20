set search_path = evesso, public;
begin;
alter table if exists characters
    drop column access_token;
commit;
