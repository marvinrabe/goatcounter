select
	trim(name || ' ' || version) as name,
	sum(count)            as count
from browser_stats
join browsers using (browser_id)
where day >= date(:start) and day <= date(:end) and :filter and lower(name) = lower(:browser)
group by name, version
order by count desc, name asc
limit :limit offset :offset
