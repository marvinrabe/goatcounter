select
	trim(name || ' ' || version) as name,
	sum(count)            as count
from system_stats
join systems using (system_id)
where day >= date(:start) and day <= date(:end) and :filter and lower(name) = lower(:system)
group by name, version
order by count desc, name asc
limit :limit offset :offset
