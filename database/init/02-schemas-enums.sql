-- Schemas and shared enums for melzone.
-- Spec: database/diagram_base.txt — "public → solo los Enum compartidos, porque son
-- los únicos objetos que usan ambos esquemas".
\connect melzone

CREATE SCHEMA seg;
CREATE SCHEMA core;

-- Enums live in public because seg and core share them (PostgreSQL types live in one
-- schema and public is in the default search_path, so they are used unqualified).
CREATE TYPE public.activatable_status AS ENUM ('ACTIVE', 'INACTIVE');
CREATE TYPE public.suspendable_status AS ENUM ('ACTIVE', 'INACTIVE', 'SUSPENDED');
CREATE TYPE public.review_status AS ENUM ('ACTIVE', 'INACTIVE', 'MODERATED');
CREATE TYPE public.photo_kind AS ENUM ('COVER', 'GALLERY');
CREATE TYPE public.identity_provider AS ENUM ('GOOGLE', 'APPLE');
CREATE TYPE public.user_token_purpose AS ENUM ('PASSWORD_RESET', 'EMAIL_CHANGE');
