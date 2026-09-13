with x as (
	select
		sum(total) as total,
		substr(datetime(hour, :offset2), 0, 14)                     as hour
	from hit_counts
	{{if .no_events}}join paths using (path_id){{end}}
	where
		datetime(hour) >= datetime(:start) and datetime(hour) <= datetime(:end) and
		{{if .no_events}}paths.event = 0 and{{end}}
		:filter
	group by hour
	order by hour asc
)
select coalesce(
	json_group_object(hour, total)
, '{}') as stats2
from x
