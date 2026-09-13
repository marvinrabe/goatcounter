with x as (
	select
		count(*) as total,
		substr(datetime(hits.created_at, :offset2), 0, 14)                      as hour
	from hits
	join paths using (path_id)
	where
		datetime(hits.created_at) >= datetime(:start) and datetime(hits.created_at) <= datetime(:end) and
		paths.event = 0 and
		:filter
	group by hour
	order by hour asc
)
select coalesce(
	json_group_object(hour, total)
, '{}') as stats2
from x
