with x as (
	select
		count(*) as total,
		{{:sqlite  substr(datetime(hits.created_at, :offset2), 0, 14)                      as hour}}
		{{:sqlite! substr((hits.created_at + :offset * interval '1 minute')::text, 0, 14) as hour}}
	from hits
	join paths using (path_id)
	where
		hits.created_at >= :start and hits.created_at <= :end and
		paths.event = 0 and
		:filter
	group by hour
	order by hour asc
)
select coalesce(
	{{:sqlite  json_group_object(hour, total)}}
	{{:sqlite! jsonb_object_agg(hour, total)}}
, '{}') as stats2
from x
