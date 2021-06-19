set search_path = evesso, public;
begin;
drop table skynet.evesso.characters;
drop table skynet.evesso.pkces;
drop table skynet.evesso.profiles;
drop extension if exists "uuid-ossp";
commit;
drop schema evesso cascade;
