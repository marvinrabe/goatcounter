with session_stats as (
	select
		hits.session,
		min(hits.created_at) as started_at,
		count(*) as pageviews,
		{{:sqlite  cast(strftime('%s', max(hits.created_at)) as integer) - cast(strftime('%s', min(hits.created_at)) as integer) as duration}}
		{{:sqlite! extract(epoch from (max(hits.created_at) - min(hits.created_at))) as duration}}
	from hits
	join paths using (path_id)
	where
		datetime(hits.created_at) >= datetime(:start) and datetime(hits.created_at) <= datetime(:end) and
		paths.event = 0 and
		:filter
	group by hits.session
)
select
	{{:sqlite  substr(datetime(started_at, :offset2), 0, 14)                      as hour,}}
	{{:sqlite! substr((started_at + :offset * interval '1 minute')::text, 0, 14) as hour,}}
	count(*) as visits,
	sum(pageviews) as pageviews,
	sum(case when pageviews = 1 then 1 else 0 end) as bounces,
	sum(duration) as duration
from session_stats
group by hour
order by hour
