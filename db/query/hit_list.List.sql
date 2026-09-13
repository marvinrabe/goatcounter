with x as (
	select
		sum(total) as total,
		path_id,
		json_group_object(substr(datetime(hour, :offset2), 0, 14), total) as stats2
	from hit_counts
	where
		{{if .exclude}}path_id not in (:exclude) and{{end}}
		:filter and
		datetime(hour) >= datetime(:start) and datetime(hour) <= datetime(:end)
	group by path_id
	order by total desc, path_id desc
	limit :limit
)
select
	path_id,
	paths.path,
	paths.event,
	total as count,
	coalesce(stats2, '{}') as stats2
from x
join paths using (path_id)
order by total desc, path_id desc
