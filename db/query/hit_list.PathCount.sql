with x as (
	select path_id, path from paths
	where lower(path) = lower(:path) and site = :site
)
select
	x.path,
	coalesce(sum(total), 0) as count
from hit_counts
join x using (path_id)
where
	path_id = x.path_id and hit_counts.site = :site
	{{:start and datetime(hour) >= datetime(:start)}}
	{{:end   and datetime(hour) <= datetime(:end)}}
group by x.path, hit_counts.path_id
