-- Dashboard authentication is configured entirely through environment
-- variables; user records are no longer stored in the analytics database.
drop table if exists users;
