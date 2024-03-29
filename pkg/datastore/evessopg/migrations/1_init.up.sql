begin;
create schema if not exists evesso;
create table if not exists evesso.profiles
(
    id           uuid        not null DEFAULT uuid_generate_v4(),
    profile_name text        not null,
    data         jsonb,
    created_at   timestamptz not null,
    updated_at   timestamptz not null,
    constraint profiles_pkey
        primary key (id)
);

create unique index if not exists profiles_profile_name_idx
    on evesso.profiles (profile_name);

create table if not exists evesso.pkces
(
    id                    uuid        not null DEFAULT uuid_generate_v4(),
    profile_ref           uuid        not null,
    state                 uuid        not null DEFAULT uuid_generate_v4(),
    code_verifier         text        not null,
    code_challange        text        not null,
    code_challange_method text                 default 'S256'::character varying not null,
    scopes                text[],
    reference_data        jsonb,
    created_at            timestamptz not null,
    constraint pkces_pkey
        primary key (id),
    constraint pkce_profile_fk
        foreign key (profile_ref) references evesso.profiles
            on delete cascade
);

create unique index if not exists pkces_state_idx
    on evesso.pkces (state);

create table if not exists evesso.characters
(
    id             uuid        not null DEFAULT uuid_generate_v4(),
    profile_ref    uuid        not null,
    character_id   integer     not null,
    character_name text        not null,
    owner          text        not null,
    refresh_token  text        not null,
    scopes         text[],
    active         boolean     not null,
    access_token   text,
    reference_data jsonb,
    created_at     timestamptz not null,
    updated_at     timestamptz not null,
    constraint characters_pkey
        primary key (id),
    constraint characters_identity
        unique (profile_ref, character_id, character_name, owner, scopes),
    constraint character_profile_fk
        foreign key (profile_ref) references evesso.profiles
            on delete cascade
);

create index if not exists characters_character_id_idx
    on evesso.characters (character_id);

create index if not exists characters_character_name_idx
    on evesso.characters (character_name);

create index if not exists characters_owner_idx
    on evesso.characters (owner);

create index if not exists characters_scopes_idx
    on evesso.characters (scopes);
commit;
