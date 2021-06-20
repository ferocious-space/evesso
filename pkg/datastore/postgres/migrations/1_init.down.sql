set search_path = evesso, public;
begin;
drop table characters;
drop table pkces;
drop table profiles;
drop extension if exists "uuid-ossp";
commit;
