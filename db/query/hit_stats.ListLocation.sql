select
	coalesce(region_name, '(unknown)') as name,
	sum(count)                  as count
from location_stats
join locations on location = iso_3166_2
where day >= date(:start) and day <= date(:end) and :filter and country = :country
group by iso_3166_2, name
order by count desc, name asc
limit :limit offset :offset
