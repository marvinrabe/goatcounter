select * from paths
where
	1=1
	{{:after and path_id > :after}}
order by path, path_id
{{:limit limit :limit}}
