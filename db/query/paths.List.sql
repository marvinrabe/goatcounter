select * from paths
where
	site = :site
	{{:after and path_id > :after}}
order by path, path_id
{{:limit limit :limit}}
