-- Settings are no longer per-user: the timezone comes from the TZ environment
-- variable and everything else moved into site.settings.
alter table users drop column settings;
alter table site  drop column user_defaults;
