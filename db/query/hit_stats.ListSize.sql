select
	'↔ ' || width || 'px' as name,
	sum(count)     as count
from size_stats
where
	day >= date(:start) and day <= date(:end) and :filter
	{{if .max_size}}and width != 0 and width > :min_size and width <= :max_size{{end}}
	{{if .empty}}and width = 0{{end}}
group by width
order by count desc, name asc
limit :limit offset :offset
