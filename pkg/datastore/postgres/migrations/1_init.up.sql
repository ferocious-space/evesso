begin;
create table if not exists profiles
(
    id           varchar(255) not null,
    profile_name varchar(255) not null,
    created_at   timestamp    not null,
    updated_at   timestamp    not null,
    constraint profiles_pkey
        primary key (id)
);

create unique index if not exists profiles_profile_name_idx
    on profiles (profile_name);

create table if not exists pkces
(
    id                    varchar(255)                                   not null,
    profile_ref           varchar(255)                                   not null,
    state                 varchar(255)                                   not null,
    code_verifier         varchar(255)                                   not null,
    code_challange        varchar(255)                                   not null,
    code_challange_method varchar(255) default 'S256'::character varying not null,
    created_at            timestamp                                      not null,
    constraint pkces_pkey
        primary key (id),
    constraint pkce_profile_fk
        foreign key (profile_ref) references profiles
            on update cascade on delete cascade
);

create unique index if not exists pkces_state_idx
    on pkces (state);

create table if not exists characters
(
    id             varchar(255) not null,
    profile_ref    varchar(255) not null,
    character_id   integer      not null,
    character_name varchar(255) not null,
    owner          varchar(255) not null,
    refresh_token  varchar(255) not null,
    scopes         jsonb        not null,
    active         boolean      not null,
    created_at     timestamp    not null,
    updated_at     timestamp    not null,
    constraint characters_pkey
        primary key (id),
    constraint character_profile_fk
        foreign key (profile_ref) references profiles
            on update cascade on delete cascade
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
