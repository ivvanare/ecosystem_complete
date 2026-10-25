-- Schemas and shared enums for melia.
-- Spec: database/diagram_base.txt — "public → solo los Enum compartidos, porque son
-- los únicos objetos que usan ambos esquemas".
\connect melia

-- El ledger de migraciones nace en seg (seg.tb_schema_migration), así que el esquema
-- seg ya existe cuando este archivo corre. IF NOT EXISTS evita que el applier choque
-- consigo mismo con "schema seg already exists" (SQLSTATE 42P06).
CREATE SCHEMA IF NOT EXISTS seg;
CREATE SCHEMA IF NOT EXISTS core;

-- Enums live in public because seg and core share them (PostgreSQL types live in one
-- schema and public is in the default search_path, so they are used unqualified).
CREATE TYPE public.activatable_status AS ENUM ('ACTIVE', 'INACTIVE');
CREATE TYPE public.suspendable_status AS ENUM ('ACTIVE', 'INACTIVE', 'SUSPENDED');
CREATE TYPE public.review_status AS ENUM ('ACTIVE', 'INACTIVE', 'MODERATED');
CREATE TYPE public.photo_kind AS ENUM ('COVER', 'GALLERY');
CREATE TYPE public.identity_provider AS ENUM ('GOOGLE', 'APPLE');
CREATE TYPE public.user_token_purpose AS ENUM ('PASSWORD_RESET', 'EMAIL_CHANGE');
