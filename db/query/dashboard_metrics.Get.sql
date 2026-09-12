with session_stats as (
	select
		hits.session,
		count(*) as pageviews,
		{{:sqlite  cast(strftime('%s', max(hits.created_at)) as integer) - cast(strftime('%s', min(hits.created_at)) as integer) as duration}}
		{{:sqlite! extract(epoch from (max(hits.created_at) - min(hits.created_at))) as duration}}
	from hits
	join paths using (path_id)
	where
		hits.created_at >= :start and hits.created_at <= :end and
		paths.event = 0 and
		:filter
	group by hits.session
)
select
	count(*) as visits,
	coalesce(sum(pageviews), 0) as pageviews,
	coalesce(100.0 * sum(case when pageviews = 1 then 1 else 0 end) / nullif(count(*), 0), 0) as bounce_rate,
	coalesce(avg(duration), 0) as visit_duration_seconds
from session_stats
