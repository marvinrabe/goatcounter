with prev as (
	select
		path_id,
		sum(total) as total
	from hit_counts
	where
		site = :site and path_id in (:paths) and
		datetime(hour) >= datetime(:prevstart) and datetime(hour) <= datetime(:prevend)
	group by path_id
),
cur as (
	select
		path_id,
		sum(c.total) as total
	from hit_counts c
	where
		site = :site and path_id in (:paths) and
		datetime(hour) >= datetime(:start) and datetime(hour) <= datetime(:end)
	group by path_id
)
select
	case
		when coalesce(prev.total, 0) = 0 then 1e999
		else (cast(coalesce(cur.total, 0) - prev.total as real) / prev.total) * 100.0
	end as diff
from cur
left join prev using (path_id)
order by cur.total desc, path_id desc
