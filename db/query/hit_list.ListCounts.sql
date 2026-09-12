with x as (
	select sum(total) as total, path_id
	from hit_counts
	where
		{{:exclude path_id not in (:exclude) and}}
		:filter and
		datetime(hour) >= datetime(:start) and datetime(hour) <= datetime(:end)
	group by path_id
	order by total desc, path_id desc
	limit :limit
)
select path_id, paths.path, paths.event, total as count
from x
join paths using (path_id)
order by total desc, path_id desc
