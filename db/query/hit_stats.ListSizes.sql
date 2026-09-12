select
	width             as name,
	sum(count) as count
from size_stats
where day >= :start and day <= :end and :filter
group by width
order by count desc, name asc
