select
	width             as name,
	sum(count) as count
from size_stats
where day >= date(:start) and day <= date(:end) and :filter
group by width
order by count desc, name asc
