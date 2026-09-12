-- Page titles are no longer collected or displayed.
drop index if exists "paths#site#title";
drop index if exists "paths#site#path";

-- Rebuild instead of using DROP COLUMN so this also works when initializing a
-- new database from the current schema, where title is already absent.
create table paths_without_titles (
	path_id integer primary key autoincrement,
	site    varchar not null,
	path    varchar not null,
	event   integer default 0
);
insert into paths_without_titles (path_id, site, path, event)
	select path_id, site, path, event from paths;
drop table paths;
alter table paths_without_titles rename to paths;
create unique index "paths#site#path" on paths(site, lower(path));
