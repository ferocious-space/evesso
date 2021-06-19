create extension if not exists "uuid-ossp" with schema public;
create schema if not exists evesso;
set search_path = evesso, public;
begin;
create table if not exists profiles
(
    id           uuid        not null DEFAULT uuid_generate_v4(),
    profile_name text        not null,
    created_at   timestamptz not null,
    updated_at   timestamptz not null,
    constraint profiles_pkey
        primary key (id)
);

create unique index if not exists profiles_profile_name_idx
    on profiles (profile_name);

create table if not exists pkces
(
    id                    uuid        not null DEFAULT uuid_generate_v4(),
    profile_ref           uuid        not null,
    state                 text        not null DEFAULT uuid_generate_v4(),
    code_verifier         text        not null,
    code_challange        text        not null,
    code_challange_method text                 default 'S256'::character varying not null,
    scopes                text[],
    created_at            timestamptz not null,
    constraint pkces_pkey
        primary key (id),
    constraint pkce_profile_fk
        foreign key (profile_ref) references profiles
            on delete cascade
);

create unique index if not exists pkces_state_idx
    on pkces (state);

create table if not exists characters
(
    id             uuid        not null DEFAULT uuid_generate_v4(),
    profile_ref    uuid        not null,
    character_id   integer     not null,
    character_name text        not null,
    owner          text        not null,
    refresh_token  text        not null,
    scopes         text[],
    active         boolean     not null,
    created_at     timestamptz not null,
    updated_at     timestamptz not null,
    constraint characters_pkey
        primary key (id),
    constraint character_profile_fk
        foreign key (profile_ref) references profiles
            on delete cascade
);

create unique index if not exists characters_profile_ref_character_id_character_name_owner_scopes
    on characters (profile_ref, character_id, character_name, owner, scopes);

create index if not exists characters_character_id_idx
    on characters (character_id);

create index if not exists characters_character_name_idx
    on characters (character_name);

create index if not exists characters_owner_idx
    on characters (owner);

create index if not exists characters_scopes_idx
    on characters (scopes);
commit;
