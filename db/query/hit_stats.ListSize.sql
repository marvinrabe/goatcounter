select
	'↔ ' || width || 'px' as name,
	sum(count)     as count
from size_stats
where
	day >= :start and day <= :end and :filter
	{{:max_size and width != 0 and width > :min_size and width <= :max_size}}
	{{:empty    and width = 0}}
group by width
order by count desc, name asc
limit :limit offset :offset
