-- Validates the checks 00002 added NOT VALID, in their own transaction (SHARE UPDATE EXCLUSIVE lock only).

-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '60s';

ALTER TABLE hits VALIDATE CONSTRAINT hits_format_check;
ALTER TABLE hits VALIDATE CONSTRAINT hits_resource_check;
