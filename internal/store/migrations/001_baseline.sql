-- New Yokosuka Server initial schema.

CREATE FUNCTION public.enforce_script_test_fixture_version() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM script_versions version
        WHERE version.id = NEW.source_version_id
          AND version.script_id = NEW.script_id
          AND version.content_format = 'yarn'
          AND version.compile_status = 'valid'
    ) THEN
        RAISE EXCEPTION 'script test fixture source must be a valid Yarn version of the same script';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION public.protect_published_script_version() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.status <> 'draft' AND (
        NEW.script_id IS DISTINCT FROM OLD.script_id
        OR NEW.version IS DISTINCT FROM OLD.version
        OR NEW.document IS DISTINCT FROM OLD.document
        OR NEW.summary IS DISTINCT FROM OLD.summary
        OR NEW.schema_version IS DISTINCT FROM OLD.schema_version
        OR NEW.source_hash IS DISTINCT FROM OLD.source_hash
        OR NEW.revision IS DISTINCT FROM OLD.revision
        OR NEW.content_format IS DISTINCT FROM OLD.content_format
        OR NEW.source_text IS DISTINCT FROM OLD.source_text
        OR NEW.source_text_hash IS DISTINCT FROM OLD.source_text_hash
        OR NEW.compiled_program IS DISTINCT FROM OLD.compiled_program
        OR NEW.compiled_program_hash IS DISTINCT FROM OLD.compiled_program_hash
        OR NEW.compiler_version IS DISTINCT FROM OLD.compiler_version
        OR NEW.command_schema_version IS DISTINCT FROM OLD.command_schema_version
        OR NEW.compile_status IS DISTINCT FROM OLD.compile_status
        OR NEW.compiler_diagnostics IS DISTINCT FROM OLD.compiler_diagnostics
        OR NEW.compiler_lines IS DISTINCT FROM OLD.compiler_lines
        OR NEW.compiler_nodes IS DISTINCT FROM OLD.compiler_nodes
        OR NEW.native_source_locator IS DISTINCT FROM OLD.native_source_locator
        OR NEW.native_source_hash IS DISTINCT FROM OLD.native_source_hash
        OR NEW.based_on_version_id IS DISTINCT FROM OLD.based_on_version_id
        OR NEW.authored_by IS DISTINCT FROM OLD.authored_by
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
    ) THEN
        RAISE EXCEPTION 'non-draft script versions are immutable';
    END IF;

    IF OLD.status <> NEW.status AND NOT (
        (OLD.status = 'draft' AND NEW.status IN ('review', 'reference'))
        OR (OLD.status = 'review' AND NEW.status = 'published')
        OR (OLD.status = 'published' AND NEW.status = 'superseded')
    ) THEN
        RAISE EXCEPTION 'invalid script version status transition from % to %', OLD.status, NEW.status;
    END IF;

    IF NEW.published_at IS DISTINCT FROM OLD.published_at
       AND NOT (
           (OLD.status = 'review' AND NEW.status = 'published')
           OR (OLD.status = 'draft' AND NEW.status = 'reference')
       ) THEN
        RAISE EXCEPTION 'script version publication timestamp is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION public.protect_script_moderation_event() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    RAISE EXCEPTION 'script moderation events are immutable';
END;
$$;

CREATE FUNCTION public.protect_script_review_comment() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    RAISE EXCEPTION 'script review comments are immutable';
END;
$$;

CREATE FUNCTION public.protect_script_review_thread_identity() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.version_id IS DISTINCT FROM OLD.version_id
       OR NEW.line_number IS DISTINCT FROM OLD.line_number
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'script review thread identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION public.protect_script_version_child() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    target_version_id bigint;
    parent_status text;
BEGIN
    target_version_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.version_id ELSE NEW.version_id END;
    SELECT status INTO parent_status FROM script_versions WHERE id = target_version_id;

    -- Cascading removal is allowed after the parent version is no longer visible.
    IF parent_status IS NULL AND TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    IF parent_status IS DISTINCT FROM 'draft' THEN
        RAISE EXCEPTION 'script version indexes are immutable outside draft status';
    END IF;
    RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$;

CREATE FUNCTION public.reject_archived_script_version_write() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM scripts
        WHERE id = NEW.script_id AND archived_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'archived scripts are read-only';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION public.require_immutable_script_review_version() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    version_status text;
    script_origin text;
BEGIN
    SELECT version.status, script.origin INTO version_status, script_origin
    FROM script_versions version
    JOIN scripts script ON script.id=version.script_id
    WHERE version.id=NEW.version_id;
    IF script_origin <> 'community'
       OR version_status NOT IN ('review', 'published', 'superseded') THEN
        RAISE EXCEPTION 'script review threads require an immutable community version';
    END IF;
    RETURN NEW;
END;
$$;

SET default_tablespace = '';

SET default_table_access_method = heap;

CREATE TABLE public.account_sessions (
    token_hash character(64) NOT NULL,
    account_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone
);

CREATE TABLE public.accounts (
    id bigint NOT NULL,
    account_type text NOT NULL,
    email text,
    password_hash text,
    guest_token_hash character(64),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    role text DEFAULT 'member'::text NOT NULL,
    CONSTRAINT accounts_account_type_check CHECK ((account_type = ANY (ARRAY['guest'::text, 'registered'::text]))),
    CONSTRAINT accounts_check CHECK ((((account_type = 'guest'::text) AND (guest_token_hash IS NOT NULL) AND (email IS NULL) AND (password_hash IS NULL)) OR ((account_type = 'registered'::text) AND (guest_token_hash IS NULL) AND (email IS NOT NULL) AND (password_hash IS NOT NULL)))),
    CONSTRAINT accounts_registered_password_bcrypt CHECK (((account_type <> 'registered'::text) OR (password_hash ~ '^\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}$'::text))),
    CONSTRAINT accounts_role_check CHECK ((role = ANY (ARRAY['member'::text, 'moderator'::text, 'admin'::text])))
);

ALTER TABLE public.accounts ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.accounts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.arcade_high_scores (
    machine_id text NOT NULL,
    score double precision NOT NULL,
    account_id bigint,
    achieved_at timestamp with time zone DEFAULT now() NOT NULL,
    character_id bigint,
    CONSTRAINT arcade_high_scores_check CHECK (((score >= (0)::double precision) AND (score <> 'NaN'::double precision) AND (score <> ALL (ARRAY['Infinity'::double precision, '-Infinity'::double precision])) AND (((machine_id = 'qte-0'::text) AND (score <= (1000000000)::double precision)) OR ((machine_id = 'qte-1'::text) AND (score <= (99999)::double precision)) OR ((machine_id = ANY (ARRAY['darts-0'::text, 'darts-1'::text])) AND (score <= (300)::double precision))))),
    CONSTRAINT arcade_high_scores_machine_id_check CHECK ((machine_id = ANY (ARRAY['qte-0'::text, 'qte-1'::text, 'darts-0'::text, 'darts-1'::text]))),
    CONSTRAINT arcade_high_scores_score_check CHECK (((score >= (0)::double precision) AND (score <> 'NaN'::double precision) AND (score <> ALL (ARRAY['Infinity'::double precision, '-Infinity'::double precision])) AND (((machine_id = 'qte-0'::text) AND (score <= (1000000000)::double precision)) OR ((machine_id = 'qte-1'::text) AND (score <= (99999)::double precision)) OR ((machine_id = ANY (ARRAY['darts-0'::text, 'darts-1'::text])) AND (score <= (300)::double precision)))))
);

CREATE TABLE public.character_dialogue_state (
    character_id bigint NOT NULL,
    revision bigint NOT NULL,
    snapshot jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT character_dialogue_state_revision_check CHECK ((revision > 0)),
    CONSTRAINT character_dialogue_state_snapshot_check CHECK ((jsonb_typeof(snapshot) = 'object'::text))
);

CREATE TABLE public.character_inventory (
    character_id bigint NOT NULL,
    item_key text NOT NULL,
    quantity integer NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT character_inventory_quantity_check CHECK ((quantity > 0))
);

CREATE TABLE public.character_script_state (
    character_id bigint NOT NULL,
    revision bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT character_script_state_revision_check CHECK ((revision >= 0))
);

CREATE TABLE public.character_story_flags (
    character_id bigint NOT NULL,
    key text NOT NULL,
    value boolean NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT character_story_flags_key_check CHECK ((key ~ '^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,159}$'::text))
);

CREATE TABLE public.character_story_progress (
    character_id bigint NOT NULL,
    key text NOT NULL,
    value double precision NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT character_story_progress_key_check CHECK ((key ~ '^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,159}$'::text)),
    CONSTRAINT character_story_progress_value_check CHECK (((value = value) AND (value <> ALL (ARRAY['Infinity'::double precision, '-Infinity'::double precision]))))
);

CREATE TABLE public.characters (
    id bigint NOT NULL,
    account_id bigint NOT NULL,
    name text NOT NULL,
    avatar_key text DEFAULT 'ryo'::text NOT NULL,
    world_id text DEFAULT 'exterior'::text NOT NULL,
    x double precision DEFAULT 0 NOT NULL,
    y double precision DEFAULT 0 NOT NULL,
    z double precision DEFAULT 0 NOT NULL,
    yaw double precision DEFAULT 0 NOT NULL,
    experience bigint DEFAULT 0 NOT NULL,
    current_hp integer DEFAULT 100 NOT NULL,
    yen bigint DEFAULT 2000 NOT NULL,
    last_login_at timestamp with time zone,
    time_played_seconds bigint DEFAULT 0 NOT NULL,
    location_updated_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    max_hp integer DEFAULT 100 NOT NULL,
    CONSTRAINT characters_current_hp_check CHECK ((current_hp >= 0)),
    CONSTRAINT characters_current_hp_within_max CHECK ((current_hp <= max_hp)),
    CONSTRAINT characters_experience_check CHECK ((experience >= 0)),
    CONSTRAINT characters_max_hp_positive CHECK ((max_hp > 0)),
    CONSTRAINT characters_time_played_seconds_check CHECK ((time_played_seconds >= 0)),
    CONSTRAINT characters_yen_check CHECK ((yen >= 0))
);

ALTER TABLE public.characters ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.characters_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.chat_messages (
    id bigint NOT NULL,
    account_id bigint,
    character_id bigint,
    player_id text NOT NULL,
    player_name text NOT NULL,
    world_id text NOT NULL,
    message_text text NOT NULL,
    remote_ip text DEFAULT ''::text NOT NULL,
    user_agent text DEFAULT ''::text NOT NULL,
    sent_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chat_messages_message_text_check CHECK (((char_length(message_text) >= 1) AND (char_length(message_text) <= 240)))
);

ALTER TABLE public.chat_messages ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.chat_messages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.economy_events (
    id bigint NOT NULL,
    character_id bigint NOT NULL,
    event_key text,
    kind text NOT NULL,
    item_key text,
    quantity_delta integer DEFAULT 0 NOT NULL,
    yen_delta bigint DEFAULT 0 NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE public.economy_events ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.economy_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.item_definitions (
    key text NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    category text DEFAULT 'misc'::text NOT NULL,
    max_stack integer DEFAULT 99 NOT NULL,
    usable boolean DEFAULT false NOT NULL,
    effect_kind text,
    effect_value integer,
    CONSTRAINT item_definitions_check CHECK ((((usable = false) AND (effect_kind IS NULL) AND (effect_value IS NULL)) OR ((usable = true) AND (effect_kind IS NOT NULL) AND (effect_value IS NOT NULL)))),
    CONSTRAINT item_definitions_max_stack_check CHECK ((max_stack > 0))
);

CREATE TABLE public.npc_runtime_state (
    npc_id text NOT NULL,
    day_number bigint NOT NULL,
    accumulated_delay_seconds double precision DEFAULT 0 NOT NULL,
    revision bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT npc_runtime_state_accumulated_delay_seconds_check CHECK ((accumulated_delay_seconds >= (0)::double precision)),
    CONSTRAINT npc_runtime_state_revision_check CHECK ((revision >= 0))
);

CREATE TABLE public.progression_events (
    id bigint NOT NULL,
    character_id bigint NOT NULL,
    event_key text,
    kind text NOT NULL,
    experience_delta bigint DEFAULT 0 NOT NULL,
    hp_delta integer DEFAULT 0 NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE public.progression_events ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.progression_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.script_collaborators (
    script_id bigint NOT NULL,
    account_id bigint NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT script_collaborators_role_check CHECK ((role = ANY (ARRAY['owner'::text, 'editor'::text, 'reviewer'::text])))
);

CREATE TABLE public.script_event_effects (
    run_id bigint NOT NULL,
    sequence integer NOT NULL,
    command_name text NOT NULL,
    arguments jsonb NOT NULL,
    CONSTRAINT script_event_effects_arguments_check CHECK ((jsonb_typeof(arguments) = 'array'::text)),
    CONSTRAINT script_event_effects_sequence_check CHECK ((sequence > 0))
);

CREATE TABLE public.script_event_runs (
    id bigint NOT NULL,
    character_id bigint NOT NULL,
    version_id bigint NOT NULL,
    entry_node text NOT NULL,
    trigger_kind text NOT NULL,
    lease_token character(64) NOT NULL,
    lease_expires_at timestamp with time zone NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    completion_requested boolean DEFAULT false NOT NULL,
    state_revision bigint NOT NULL,
    failure_code text,
    failure_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    CONSTRAINT script_event_runs_entry_node_check CHECK (((char_length(entry_node) >= 1) AND (char_length(entry_node) <= 160))),
    CONSTRAINT script_event_runs_lease_token_check CHECK ((lease_token ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT script_event_runs_lifecycle_check CHECK ((((status = 'active'::text) AND (finished_at IS NULL) AND (failure_code IS NULL) AND (failure_message IS NULL)) OR ((status = ANY (ARRAY['completed'::text, 'passed'::text, 'cancelled'::text, 'expired'::text])) AND (finished_at IS NOT NULL) AND (failure_code IS NULL) AND (failure_message IS NULL)) OR ((status = 'failed'::text) AND (finished_at IS NOT NULL) AND (failure_code IS NOT NULL) AND (failure_message IS NOT NULL)))),
    CONSTRAINT script_event_runs_state_revision_check CHECK ((state_revision >= 0)),
    CONSTRAINT script_event_runs_status_check CHECK ((status = ANY (ARRAY['active'::text, 'completed'::text, 'passed'::text, 'cancelled'::text, 'failed'::text, 'expired'::text]))),
    CONSTRAINT script_event_runs_trigger_kind_check CHECK ((trigger_kind = ANY (ARRAY['talk'::text, 'use'::text, 'inspect'::text, 'enter'::text, 'automatic'::text, 'activity'::text])))
);

ALTER TABLE public.script_event_runs ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.script_event_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.script_event_trace (
    run_id bigint NOT NULL,
    ordinal integer NOT NULL,
    runtime_sequence integer NOT NULL,
    direction text NOT NULL,
    kind text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT script_event_trace_direction_check CHECK ((direction = ANY (ARRAY['runtime'::text, 'controller'::text]))),
    CONSTRAINT script_event_trace_kind_check CHECK ((kind ~ '^[A-Za-z][A-Za-z0-9_-]{0,63}$'::text)),
    CONSTRAINT script_event_trace_ordinal_check CHECK ((ordinal > 0)),
    CONSTRAINT script_event_trace_payload_check CHECK ((jsonb_typeof(payload) = 'object'::text)),
    CONSTRAINT script_event_trace_runtime_sequence_check CHECK ((runtime_sequence >= 0))
);

CREATE TABLE public.script_moderation_events (
    id bigint NOT NULL,
    script_id bigint NOT NULL,
    version_id bigint,
    actor_id bigint NOT NULL,
    action text NOT NULL,
    details jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT script_moderation_events_action_check CHECK ((action = ANY (ARRAY['script.archived'::text, 'script.restored'::text, 'version.submitted'::text, 'version.published'::text, 'version.rollback-published'::text, 'collaborator.added'::text, 'collaborator.role-changed'::text, 'collaborator.removed'::text, 'review-thread.resolved'::text, 'review-thread.reopened'::text]))),
    CONSTRAINT script_moderation_events_details_check CHECK ((jsonb_typeof(details) = 'object'::text))
);

ALTER TABLE public.script_moderation_events ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.script_moderation_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.script_review_comments (
    id bigint NOT NULL,
    thread_id bigint NOT NULL,
    author_id bigint NOT NULL,
    body text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT script_review_comments_body_check CHECK ((((char_length(body) >= 1) AND (char_length(body) <= 4000)) AND (body = btrim(body))))
);

ALTER TABLE public.script_review_comments ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.script_review_comments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.script_review_threads (
    id bigint NOT NULL,
    version_id bigint NOT NULL,
    line_number integer,
    created_by bigint NOT NULL,
    status text DEFAULT 'open'::text NOT NULL,
    resolved_by bigint,
    resolved_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT script_review_threads_check CHECK ((((status = 'open'::text) AND (resolved_by IS NULL) AND (resolved_at IS NULL)) OR ((status = 'resolved'::text) AND (resolved_by IS NOT NULL) AND (resolved_at IS NOT NULL)))),
    CONSTRAINT script_review_threads_line_number_check CHECK ((line_number > 0)),
    CONSTRAINT script_review_threads_status_check CHECK ((status = ANY (ARRAY['open'::text, 'resolved'::text])))
);

ALTER TABLE public.script_review_threads ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.script_review_threads_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.script_test_fixtures (
    id bigint NOT NULL,
    script_id bigint NOT NULL,
    source_version_id bigint NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    start_node text NOT NULL,
    fixture jsonb NOT NULL,
    created_by bigint,
    revision bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    archived_by bigint,
    origin text DEFAULT 'community'::text NOT NULL,
    CONSTRAINT script_test_fixtures_check CHECK ((((archived_at IS NULL) AND (archived_by IS NULL)) OR (archived_at IS NOT NULL))),
    CONSTRAINT script_test_fixtures_description_check CHECK ((char_length(description) <= 1000)),
    CONSTRAINT script_test_fixtures_fixture_check CHECK ((jsonb_typeof(fixture) = 'object'::text)),
    CONSTRAINT script_test_fixtures_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 120))),
    CONSTRAINT script_test_fixtures_origin_check CHECK ((origin = ANY (ARRAY['community'::text, 'official'::text]))),
    CONSTRAINT script_test_fixtures_revision_check CHECK ((revision > 0)),
    CONSTRAINT script_test_fixtures_start_node_check CHECK (((char_length(start_node) >= 1) AND (char_length(start_node) <= 160)))
);

ALTER TABLE public.script_test_fixtures ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.script_test_fixtures_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.script_version_dependencies (
    version_id bigint NOT NULL,
    access text NOT NULL,
    kind text NOT NULL,
    identifier text NOT NULL,
    CONSTRAINT script_version_dependencies_access_check CHECK ((access = ANY (ARRAY['read'::text, 'write'::text]))),
    CONSTRAINT script_version_dependencies_identifier_check CHECK (((char_length(identifier) >= 1) AND (char_length(identifier) <= 160))),
    CONSTRAINT script_version_dependencies_kind_check CHECK ((kind = ANY (ARRAY['flag'::text, 'progress'::text, 'item'::text, 'actor'::text, 'object'::text, 'script'::text, 'scene'::text, 'activity'::text])))
);

CREATE TABLE public.script_version_identifiers (
    version_id bigint NOT NULL,
    kind text NOT NULL,
    identifier text NOT NULL,
    CONSTRAINT script_version_identifiers_identifier_check CHECK (((char_length(identifier) >= 1) AND (char_length(identifier) <= 160))),
    CONSTRAINT script_version_identifiers_kind_check CHECK ((kind ~ '^[a-z][a-z0-9-]{0,63}$'::text))
);

CREATE TABLE public.script_version_native_dialogue_regions (
    version_id bigint NOT NULL,
    ordinal integer NOT NULL,
    disc smallint NOT NULL,
    area text NOT NULL,
    executable_target_index integer CONSTRAINT script_version_native_dialogue_executable_target_index_not_null NOT NULL,
    region_start_file_offset bigint CONSTRAINT script_version_native_dialogu_region_start_file_offset_not_null NOT NULL,
    ownership text NOT NULL,
    activity_id text,
    evidence_locator text CONSTRAINT script_version_native_dialogue_region_evidence_locator_not_null NOT NULL,
    CONSTRAINT script_version_native_dialogue_r_region_start_file_offset_check CHECK ((region_start_file_offset >= 0)),
    CONSTRAINT script_version_native_dialogue_re_executable_target_index_check CHECK ((executable_target_index >= 0)),
    CONSTRAINT script_version_native_dialogue_regions_area_check CHECK ((area ~ '^[A-Z0-9]{4}$'::text)),
    CONSTRAINT script_version_native_dialogue_regions_check CHECK ((((ownership = 'translated'::text) AND (activity_id IS NULL)) OR ((ownership = 'specialized-activity-owned'::text) AND (activity_id IS NOT NULL) AND (activity_id <> ''::text)))),
    CONSTRAINT script_version_native_dialogue_regions_disc_check CHECK (((disc >= 1) AND (disc <= 3))),
    CONSTRAINT script_version_native_dialogue_regions_evidence_locator_check CHECK ((evidence_locator <> ''::text)),
    CONSTRAINT script_version_native_dialogue_regions_ordinal_check CHECK ((ordinal >= 0)),
    CONSTRAINT script_version_native_dialogue_regions_ownership_check CHECK ((ownership = ANY (ARRAY['translated'::text, 'specialized-activity-owned'::text])))
);

CREATE TABLE public.script_version_native_sources (
    version_id bigint NOT NULL,
    ordinal integer NOT NULL,
    role text NOT NULL,
    source_locator text NOT NULL,
    source_hash character(64) NOT NULL,
    CONSTRAINT script_version_native_sources_ordinal_check CHECK ((ordinal >= 0)),
    CONSTRAINT script_version_native_sources_role_check CHECK ((role ~ '^[a-z][a-z0-9_-]{0,63}$'::text)),
    CONSTRAINT script_version_native_sources_source_hash_check CHECK ((source_hash ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT script_version_native_sources_source_locator_check CHECK ((source_locator <> ''::text))
);

CREATE TABLE public.script_version_triggers (
    version_id bigint NOT NULL,
    node_id text NOT NULL,
    kind text NOT NULL,
    area text,
    actor text,
    configuration jsonb NOT NULL,
    object_key text,
    activity_key text,
    priority smallint DEFAULT 0 NOT NULL,
    CONSTRAINT script_version_triggers_configuration_check CHECK ((jsonb_typeof(configuration) = 'object'::text)),
    CONSTRAINT script_version_triggers_priority_check CHECK (((priority >= '-1000'::integer) AND (priority <= 1000)))
);

CREATE TABLE public.script_versions (
    id bigint NOT NULL,
    script_id bigint NOT NULL,
    version integer NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    schema_version text NOT NULL,
    document jsonb,
    summary text DEFAULT ''::text NOT NULL,
    source_hash character(64),
    based_on_version_id bigint,
    authored_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    content_format text NOT NULL,
    source_text text,
    source_text_hash character(64),
    compiled_program bytea,
    compiled_program_hash character(64),
    compiler_version text,
    command_schema_version text,
    compile_status text DEFAULT 'not-applicable'::text NOT NULL,
    compiler_diagnostics jsonb DEFAULT '[]'::jsonb NOT NULL,
    compiler_lines jsonb,
    compiler_nodes jsonb,
    native_source_locator text,
    native_source_hash character(64),
    CONSTRAINT script_versions_check CHECK ((((status = ANY (ARRAY['published'::text, 'reference'::text])) AND (published_at IS NOT NULL)) OR (status <> ALL (ARRAY['published'::text, 'reference'::text])))),
    CONSTRAINT script_versions_compile_status_check CHECK ((compile_status = ANY (ARRAY['not-applicable'::text, 'uncompiled'::text, 'invalid'::text, 'valid'::text]))),
    CONSTRAINT script_versions_compiler_diagnostics_check CHECK ((jsonb_typeof(compiler_diagnostics) = 'array'::text)),
    CONSTRAINT script_versions_content_format_check CHECK ((content_format = ANY (ARRAY['native-reference'::text, 'yarn'::text]))),
    CONSTRAINT script_versions_content_shape_check CHECK ((((content_format = 'native-reference'::text) AND (document IS NOT NULL) AND (jsonb_typeof(document) = 'object'::text) AND (source_text IS NULL) AND (source_text_hash IS NULL) AND (compiled_program IS NULL) AND (compiled_program_hash IS NULL) AND (compiler_version IS NULL) AND (command_schema_version IS NULL) AND (compiler_lines IS NULL) AND (compiler_nodes IS NULL) AND (compile_status = 'not-applicable'::text)) OR ((content_format = 'yarn'::text) AND (document IS NULL) AND (source_text IS NOT NULL) AND (source_text_hash IS NOT NULL) AND (source_text_hash ~ '^[0-9a-f]{64}$'::text) AND (compiler_version IS NOT NULL) AND (command_schema_version IS NOT NULL) AND (compiler_lines IS NOT NULL) AND (jsonb_typeof(compiler_lines) = 'array'::text) AND (compiler_nodes IS NOT NULL) AND (jsonb_typeof(compiler_nodes) = 'array'::text) AND (compile_status = ANY (ARRAY['uncompiled'::text, 'invalid'::text, 'valid'::text])) AND (((compile_status = 'valid'::text) AND (compiled_program IS NOT NULL) AND (compiled_program_hash IS NOT NULL) AND (compiled_program_hash ~ '^[0-9a-f]{64}$'::text)) OR ((compile_status <> 'valid'::text) AND (compiled_program IS NULL) AND (compiled_program_hash IS NULL)))))),
    CONSTRAINT script_versions_document_check CHECK ((jsonb_typeof(document) = 'object'::text)),
    CONSTRAINT script_versions_native_provenance_pair_check CHECK ((((native_source_locator IS NULL) AND (native_source_hash IS NULL)) OR ((native_source_locator IS NOT NULL) AND (native_source_locator <> ''::text) AND (native_source_hash ~ '^[0-9a-f]{64}$'::text)))),
    CONSTRAINT script_versions_revision_check CHECK ((revision > 0)),
    CONSTRAINT script_versions_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'review'::text, 'published'::text, 'reference'::text, 'superseded'::text]))),
    CONSTRAINT script_versions_summary_check CHECK ((char_length(summary) <= 4000)),
    CONSTRAINT script_versions_version_check CHECK ((version > 0))
);

ALTER TABLE public.script_versions ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.script_versions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.scripts (
    id bigint NOT NULL,
    slug text NOT NULL,
    title text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    origin text NOT NULL,
    source_locator text,
    source_hash character(64),
    created_by bigint,
    current_published_version_id bigint,
    current_reference_version_id bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    archived_by bigint,
    CONSTRAINT scripts_archive_audit_complete CHECK ((((archived_at IS NULL) AND (archived_by IS NULL)) OR ((archived_at IS NOT NULL) AND (archived_by IS NOT NULL)))),
    CONSTRAINT scripts_check CHECK ((((origin = 'recovered'::text) AND (source_locator IS NOT NULL) AND (source_hash IS NOT NULL)) OR (origin <> 'recovered'::text))),
    CONSTRAINT scripts_description_check CHECK ((char_length(description) <= 2000)),
    CONSTRAINT scripts_origin_check CHECK ((origin = ANY (ARRAY['recovered'::text, 'community'::text, 'official'::text]))),
    CONSTRAINT scripts_slug_check CHECK ((slug ~ '^[a-z0-9][a-z0-9-]{2,79}$'::text)),
    CONSTRAINT scripts_title_check CHECK (((char_length(title) >= 1) AND (char_length(title) <= 120)))
);

ALTER TABLE public.scripts ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.scripts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.vending_purchases (
    id bigint NOT NULL,
    character_id bigint NOT NULL,
    request_id text NOT NULL,
    machine_id text NOT NULL,
    drink_key text NOT NULL,
    price bigint NOT NULL,
    winning_can boolean NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vending_purchases_price_check CHECK ((price > 0))
);

ALTER TABLE public.vending_purchases ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.vending_purchases_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

ALTER TABLE ONLY public.account_sessions
    ADD CONSTRAINT account_sessions_pkey PRIMARY KEY (token_hash);

ALTER TABLE ONLY public.accounts
    ADD CONSTRAINT accounts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.arcade_high_scores
    ADD CONSTRAINT arcade_high_scores_pkey PRIMARY KEY (machine_id);

ALTER TABLE ONLY public.character_dialogue_state
    ADD CONSTRAINT character_dialogue_state_pkey PRIMARY KEY (character_id);

ALTER TABLE ONLY public.character_inventory
    ADD CONSTRAINT character_inventory_pkey PRIMARY KEY (character_id, item_key);

ALTER TABLE ONLY public.character_script_state
    ADD CONSTRAINT character_script_state_pkey PRIMARY KEY (character_id);

ALTER TABLE ONLY public.character_story_flags
    ADD CONSTRAINT character_story_flags_pkey PRIMARY KEY (character_id, key);

ALTER TABLE ONLY public.character_story_progress
    ADD CONSTRAINT character_story_progress_pkey PRIMARY KEY (character_id, key);

ALTER TABLE ONLY public.characters
    ADD CONSTRAINT characters_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.chat_messages
    ADD CONSTRAINT chat_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.economy_events
    ADD CONSTRAINT economy_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.item_definitions
    ADD CONSTRAINT item_definitions_pkey PRIMARY KEY (key);

ALTER TABLE ONLY public.npc_runtime_state
    ADD CONSTRAINT npc_runtime_state_pkey PRIMARY KEY (npc_id);

ALTER TABLE ONLY public.progression_events
    ADD CONSTRAINT progression_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.script_collaborators
    ADD CONSTRAINT script_collaborators_pkey PRIMARY KEY (script_id, account_id);

ALTER TABLE ONLY public.script_event_effects
    ADD CONSTRAINT script_event_effects_pkey PRIMARY KEY (run_id, sequence);

ALTER TABLE ONLY public.script_event_runs
    ADD CONSTRAINT script_event_runs_lease_token_key UNIQUE (lease_token);

ALTER TABLE ONLY public.script_event_runs
    ADD CONSTRAINT script_event_runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.script_event_trace
    ADD CONSTRAINT script_event_trace_pkey PRIMARY KEY (run_id, ordinal);

ALTER TABLE ONLY public.script_moderation_events
    ADD CONSTRAINT script_moderation_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.script_review_comments
    ADD CONSTRAINT script_review_comments_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.script_review_threads
    ADD CONSTRAINT script_review_threads_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.script_test_fixtures
    ADD CONSTRAINT script_test_fixtures_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.script_version_dependencies
    ADD CONSTRAINT script_version_dependencies_pkey PRIMARY KEY (version_id, access, kind, identifier);

ALTER TABLE ONLY public.script_version_identifiers
    ADD CONSTRAINT script_version_identifiers_pkey PRIMARY KEY (version_id, kind, identifier);

ALTER TABLE ONLY public.script_version_native_dialogue_regions
    ADD CONSTRAINT script_version_native_dialogu_version_id_disc_area_executab_key UNIQUE (version_id, disc, area, executable_target_index, region_start_file_offset);

ALTER TABLE ONLY public.script_version_native_dialogue_regions
    ADD CONSTRAINT script_version_native_dialogue_regions_pkey PRIMARY KEY (version_id, ordinal);

ALTER TABLE ONLY public.script_version_native_sources
    ADD CONSTRAINT script_version_native_sources_pkey PRIMARY KEY (version_id, ordinal);

ALTER TABLE ONLY public.script_version_triggers
    ADD CONSTRAINT script_version_triggers_pkey PRIMARY KEY (version_id, node_id);

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_script_id_version_key UNIQUE (script_id, version);

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_slug_key UNIQUE (slug);

ALTER TABLE ONLY public.vending_purchases
    ADD CONSTRAINT vending_purchases_character_id_request_id_key UNIQUE (character_id, request_id);

ALTER TABLE ONLY public.vending_purchases
    ADD CONSTRAINT vending_purchases_pkey PRIMARY KEY (id);

CREATE INDEX account_sessions_account_id_idx ON public.account_sessions USING btree (account_id);

CREATE INDEX account_sessions_expiry_idx ON public.account_sessions USING btree (expires_at);

CREATE UNIQUE INDEX accounts_email_unique ON public.accounts USING btree (lower(email)) WHERE (email IS NOT NULL);

CREATE UNIQUE INDEX accounts_guest_token_hash_unique ON public.accounts USING btree (guest_token_hash) WHERE (guest_token_hash IS NOT NULL);

CREATE INDEX characters_account_id_idx ON public.characters USING btree (account_id) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX characters_active_name_unique ON public.characters USING btree (lower(name)) WHERE (deleted_at IS NULL);

CREATE INDEX chat_messages_character_sent_at_idx ON public.chat_messages USING btree (character_id, sent_at DESC) WHERE (character_id IS NOT NULL);

CREATE INDEX chat_messages_sent_at_idx ON public.chat_messages USING btree (sent_at DESC);

CREATE INDEX chat_messages_world_sent_at_idx ON public.chat_messages USING btree (world_id, sent_at DESC);

CREATE UNIQUE INDEX economy_events_idempotency_unique ON public.economy_events USING btree (character_id, event_key) WHERE (event_key IS NOT NULL);

CREATE UNIQUE INDEX progression_events_idempotency_unique ON public.progression_events USING btree (character_id, event_key) WHERE (event_key IS NOT NULL);

CREATE INDEX script_event_runs_expiry_idx ON public.script_event_runs USING btree (lease_expires_at) WHERE (status = 'active'::text);

CREATE UNIQUE INDEX script_event_runs_one_active_per_character ON public.script_event_runs USING btree (character_id) WHERE (status = 'active'::text);

CREATE INDEX script_event_runs_version_idx ON public.script_event_runs USING btree (version_id, created_at DESC);

CREATE INDEX script_event_trace_runtime_sequence_idx ON public.script_event_trace USING btree (run_id, runtime_sequence, ordinal);

CREATE INDEX script_moderation_events_script_idx ON public.script_moderation_events USING btree (script_id, created_at DESC, id DESC);

CREATE INDEX script_review_comments_thread_idx ON public.script_review_comments USING btree (thread_id, created_at, id);

CREATE INDEX script_review_threads_version_idx ON public.script_review_threads USING btree (version_id, status, created_at, id);

CREATE UNIQUE INDEX script_test_fixtures_active_name_unique ON public.script_test_fixtures USING btree (script_id, lower(name)) WHERE (archived_at IS NULL);

CREATE INDEX script_test_fixtures_official_idx ON public.script_test_fixtures USING btree (script_id, origin) WHERE (origin = 'official'::text);

CREATE INDEX script_test_fixtures_script_updated_idx ON public.script_test_fixtures USING btree (script_id, updated_at DESC, id DESC);

CREATE INDEX script_version_dependencies_lookup_idx ON public.script_version_dependencies USING btree (kind, identifier, access);

CREATE INDEX script_version_identifiers_lookup_idx ON public.script_version_identifiers USING btree (kind, identifier);

CREATE INDEX script_version_native_dialogue_regions_identity_idx ON public.script_version_native_dialogue_regions USING btree (disc, area, executable_target_index, region_start_file_offset);

CREATE INDEX script_version_native_sources_locator_idx ON public.script_version_native_sources USING btree (source_locator);

CREATE INDEX script_version_triggers_activity_idx ON public.script_version_triggers USING btree (activity_key, kind) WHERE (activity_key IS NOT NULL);

CREATE INDEX script_version_triggers_area_idx ON public.script_version_triggers USING btree (area, kind) WHERE (area IS NOT NULL);

CREATE INDEX script_version_triggers_object_idx ON public.script_version_triggers USING btree (object_key, kind) WHERE (object_key IS NOT NULL);

CREATE INDEX scripts_active_updated_idx ON public.scripts USING btree (updated_at DESC, id DESC) WHERE (archived_at IS NULL);

CREATE UNIQUE INDEX scripts_recovered_source_unique ON public.scripts USING btree (source_locator) WHERE (origin = 'recovered'::text);

CREATE INDEX vending_purchases_character_created_idx ON public.vending_purchases USING btree (character_id, created_at DESC);

CREATE TRIGGER script_dependencies_draft_only BEFORE INSERT OR DELETE OR UPDATE ON public.script_version_dependencies FOR EACH ROW EXECUTE FUNCTION public.protect_script_version_child();

CREATE TRIGGER script_identifiers_draft_only BEFORE INSERT OR DELETE OR UPDATE ON public.script_version_identifiers FOR EACH ROW EXECUTE FUNCTION public.protect_script_version_child();

CREATE TRIGGER script_moderation_events_immutable BEFORE UPDATE ON public.script_moderation_events FOR EACH ROW EXECUTE FUNCTION public.protect_script_moderation_event();

CREATE TRIGGER script_native_dialogue_regions_draft_only BEFORE INSERT OR DELETE OR UPDATE ON public.script_version_native_dialogue_regions FOR EACH ROW EXECUTE FUNCTION public.protect_script_version_child();

CREATE TRIGGER script_native_sources_draft_only BEFORE INSERT OR DELETE OR UPDATE ON public.script_version_native_sources FOR EACH ROW EXECUTE FUNCTION public.protect_script_version_child();

CREATE TRIGGER script_review_comments_immutable BEFORE UPDATE ON public.script_review_comments FOR EACH ROW EXECUTE FUNCTION public.protect_script_review_comment();

CREATE TRIGGER script_review_threads_identity_immutable BEFORE UPDATE ON public.script_review_threads FOR EACH ROW EXECUTE FUNCTION public.protect_script_review_thread_identity();

CREATE TRIGGER script_review_threads_immutable_version BEFORE INSERT ON public.script_review_threads FOR EACH ROW EXECUTE FUNCTION public.require_immutable_script_review_version();

CREATE TRIGGER script_test_fixture_version_guard BEFORE INSERT OR UPDATE OF script_id, source_version_id ON public.script_test_fixtures FOR EACH ROW EXECUTE FUNCTION public.enforce_script_test_fixture_version();

CREATE TRIGGER script_triggers_draft_only BEFORE INSERT OR DELETE OR UPDATE ON public.script_version_triggers FOR EACH ROW EXECUTE FUNCTION public.protect_script_version_child();

CREATE TRIGGER script_versions_immutable_after_publish BEFORE UPDATE ON public.script_versions FOR EACH ROW EXECUTE FUNCTION public.protect_published_script_version();

CREATE TRIGGER script_versions_read_only_while_archived BEFORE INSERT OR UPDATE ON public.script_versions FOR EACH ROW EXECUTE FUNCTION public.reject_archived_script_version_write();

ALTER TABLE ONLY public.account_sessions
    ADD CONSTRAINT account_sessions_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.arcade_high_scores
    ADD CONSTRAINT arcade_high_scores_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.arcade_high_scores
    ADD CONSTRAINT arcade_high_scores_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.character_dialogue_state
    ADD CONSTRAINT character_dialogue_state_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.character_inventory
    ADD CONSTRAINT character_inventory_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.character_inventory
    ADD CONSTRAINT character_inventory_item_key_fkey FOREIGN KEY (item_key) REFERENCES public.item_definitions(key);

ALTER TABLE ONLY public.character_script_state
    ADD CONSTRAINT character_script_state_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.character_story_flags
    ADD CONSTRAINT character_story_flags_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.character_story_progress
    ADD CONSTRAINT character_story_progress_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.characters
    ADD CONSTRAINT characters_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.chat_messages
    ADD CONSTRAINT chat_messages_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.chat_messages
    ADD CONSTRAINT chat_messages_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.economy_events
    ADD CONSTRAINT economy_events_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.economy_events
    ADD CONSTRAINT economy_events_item_key_fkey FOREIGN KEY (item_key) REFERENCES public.item_definitions(key);

ALTER TABLE ONLY public.progression_events
    ADD CONSTRAINT progression_events_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_collaborators
    ADD CONSTRAINT script_collaborators_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_collaborators
    ADD CONSTRAINT script_collaborators_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_event_effects
    ADD CONSTRAINT script_event_effects_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.script_event_runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_event_runs
    ADD CONSTRAINT script_event_runs_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_event_runs
    ADD CONSTRAINT script_event_runs_version_id_fkey FOREIGN KEY (version_id) REFERENCES public.script_versions(id);

ALTER TABLE ONLY public.script_event_trace
    ADD CONSTRAINT script_event_trace_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.script_event_runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_moderation_events
    ADD CONSTRAINT script_moderation_events_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.accounts(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.script_moderation_events
    ADD CONSTRAINT script_moderation_events_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_moderation_events
    ADD CONSTRAINT script_moderation_events_version_id_fkey FOREIGN KEY (version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_review_comments
    ADD CONSTRAINT script_review_comments_author_id_fkey FOREIGN KEY (author_id) REFERENCES public.accounts(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.script_review_comments
    ADD CONSTRAINT script_review_comments_thread_id_fkey FOREIGN KEY (thread_id) REFERENCES public.script_review_threads(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_review_threads
    ADD CONSTRAINT script_review_threads_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.accounts(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.script_review_threads
    ADD CONSTRAINT script_review_threads_resolved_by_fkey FOREIGN KEY (resolved_by) REFERENCES public.accounts(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.script_review_threads
    ADD CONSTRAINT script_review_threads_version_id_fkey FOREIGN KEY (version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_test_fixtures
    ADD CONSTRAINT script_test_fixtures_archived_by_fkey FOREIGN KEY (archived_by) REFERENCES public.accounts(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.script_test_fixtures
    ADD CONSTRAINT script_test_fixtures_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.accounts(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.script_test_fixtures
    ADD CONSTRAINT script_test_fixtures_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_test_fixtures
    ADD CONSTRAINT script_test_fixtures_source_version_id_fkey FOREIGN KEY (source_version_id) REFERENCES public.script_versions(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.script_version_dependencies
    ADD CONSTRAINT script_version_dependencies_version_id_fkey FOREIGN KEY (version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_version_identifiers
    ADD CONSTRAINT script_version_identifiers_version_id_fkey FOREIGN KEY (version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_version_native_dialogue_regions
    ADD CONSTRAINT script_version_native_dialogue_regions_version_id_fkey FOREIGN KEY (version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_version_native_sources
    ADD CONSTRAINT script_version_native_sources_version_id_fkey FOREIGN KEY (version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_version_triggers
    ADD CONSTRAINT script_version_triggers_version_id_fkey FOREIGN KEY (version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_authored_by_fkey FOREIGN KEY (authored_by) REFERENCES public.accounts(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_based_on_version_id_fkey FOREIGN KEY (based_on_version_id) REFERENCES public.script_versions(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_archived_by_fkey FOREIGN KEY (archived_by) REFERENCES public.accounts(id);

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.accounts(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_current_published_version_fk FOREIGN KEY (current_published_version_id) REFERENCES public.script_versions(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_current_reference_version_fk FOREIGN KEY (current_reference_version_id) REFERENCES public.script_versions(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.vending_purchases
    ADD CONSTRAINT vending_purchases_character_id_fkey FOREIGN KEY (character_id) REFERENCES public.characters(id) ON DELETE CASCADE;

-- Required reference data for a new, otherwise empty database.
INSERT INTO public.item_definitions
    (key, name, description, category, max_stack, usable, effect_kind, effect_value)
VALUES
    ('toy_capsule', 'Toy Capsule', 'A capsule containing a collectible toy.', 'collectible', 99, false, NULL, NULL),
    ('winning_can', 'Winning Can', 'A rare gold can that can be exchanged for a raffle entry.', 'collectible', 99, false, NULL, NULL);

INSERT INTO public.arcade_high_scores (machine_id, score)
VALUES
    ('qte-0', 0),
    ('qte-1', 0),
    ('darts-0', 0),
    ('darts-1', 0);
